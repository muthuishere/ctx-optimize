# Design — recognizer mechanism (Phase 1 decision)

Status: DECIDED 2026-07-14 after prototyping FastAPI + Express both ways.

## ⚖️ Decision: plain Go code per framework, not a data table

Recognizers live in `internal/extract/code/routes.go` as ordinary functions
(`pyDecoratorRoutes`, `nestControllerBase` + `nestMethodRoutes`,
`expressRoute`) hooked into the ONE existing preorder visit in
`extractFile` — no second parse, no extra full-tree walk.

## Why the table lost

Both mechanisms were prototyped against real ASTs (spike: preorder dumps of
FastAPI/Flask decorators, NestJS controllers, Express chains via the
embedded grammars).

A declarative table expresses the FastAPI case fine — it is one shape:

```json
{"lang": "python", "node": "decorator", "callee_attr": ["get","post",…],
 "method_from": "callee", "path_arg": 0, "emit": "fastapi-route"}
```

It cannot express the other two frameworks without growing an interpreter:

1. **Express middleware chains** need procedure, not pattern: gate on
   receiver identifier (`app`/`router`/`*Router`), require ≥2 args (that is
   the whole `Map.get('/users')` false-positive defense), pick the LAST
   argument, then branch — identifier ⇒ deferred module-wide resolution
   reusing the existing call-`pick` (in-file unique, else module unique,
   ambiguous dropped); inline function ⇒ route node with no edge (no decl
   node exists to point at). "The last arg, resolved later, through the
   call resolver" is a program, and a table field like
   `"handler": "last_arg_or_resolve"` is just code hiding in a string.
2. **NestJS is stateful across nodes**: `@Controller('users')` on the class
   composes with `@Get(':id')` on each method, so recognition carries scope
   state on the decl stack (`openDecl.ctrlBase`) between visits. Tables
   describe single nodes; composition across an enclosing scope needs the
   visitor's state.
3. **Flask's `methods=[…]` kwarg** needs list-of-literals validation with a
   poison rule (one non-literal member kills the decorator) — again
   procedural.

The spec's own tripwire ("a table that can't express Express middleware
chains is worse than plain code") fired. Grammar packs shipping recognizer
tables was the argument FOR the table; that stays open for Phase-1
*decorator-only* frameworks later (Spring/ASP.NET attributes fit the
one-shape mold), but the mechanism of record is Go code.

## Shape of the implementation

- **One visit, bounded local scans.** Decorator discovery is a
  preceding-sibling walk (python `decorated_definition`, TS
  `export_statement`/`class_body`) plus leading-children (non-exported TS
  class), bounded by the decorator subtrees themselves.
- **Emission.** Node `kind:"route"`, id `<file>::route:<METHOD> <path>`,
  label `GET /users/{id}`, location decorator-start → decl-end (Express:
  the call's range). Edge `route -handles-> handler`, `INFERRED`,
  `metadata.synthesized_by` ∈ {`fastapi-route`, `flask-route`,
  `express-route`, `nestjs-route`}. Duplicate re-registrations of the same
  METHOD+path in one file dedupe the node (first declaration wins,
  deterministically) but keep every handles edge.
- **Literal-or-silent.** Method and path must be string literals
  (f-strings, template strings, identifiers, computed members ⇒ skip).
  `@Controller(nonLiteral)` disables the whole class; a non-literal
  `methods=` list disables that decorator.
- **Documented match boundaries** (no import/type tracking — deterministic
  pattern honesty, same as call edges): any python `@X.verb("lit")` is
  tagged `fastapi-route` (Flask 2.x verb shortcuts land here too); any
  `@X.route("lit")` is `flask-route` (blueprints included); Express matches
  only receivers named `app`, `router`, or `*Router`; NestJS verb
  decorators count only inside a literal `@Controller` class.
- **Validation.** Table-driven fixture tests per framework assert exact
  ids/labels/edges; a near-miss guard fixture (Map.get lookalikes,
  identifier-callee decorators, non-verb attributes, decorator-less class)
  and an extraction of this repo's own `internal/` tree both assert zero
  route nodes — measured, not assumed.

## ⚖️ Decision: frontend routers are CORE recognizers (2026-07-14, W4)

Angular / React Router / Vue Router live in
`internal/extract/code/frontend_routes.go`, riding the same visit. Frontend
routes have no HTTP method — the method token is **ROUTE** (label
`ROUTE /admin/users`), keeping the id shape `<file>::route:<METHOD> <path>`
uniform. These are shape-recognizers a declarative rule cannot express
(nested-children path composition, identifier→top-level-array resolution,
JSX ancestor composition) — the same finding that decided Phase 1.

What is matched / deliberately skipped:

- **angular-route** — object literals with a literal `path:` inside
  `RouterModule.forRoot([...])` / `.forChild([...])` / `provideRouter([...])`;
  the array may be a bare identifier resolved to a UNIQUE top-level
  `const <name> = [...]` in the same file (two declarations = ambiguous =
  silent). `component:` identifier → handles edge (same pick as call edges);
  `loadChildren` → lazy route node, no edge; nested `children:` compose with
  the parent path. A non-literal `path:` skips the object AND its children —
  a composed path can never be guessed. A path-only grouping object emits
  nothing (it only composes).
- **react-router-route** — JSX `<Route path="…" element={<Foo/>}/>` (also
  `Component={Foo}`), .tsx and .jsx alike (the js grammar parses JSX);
  nested `<Route>` children compose via ancestor scan, a non-literal path on
  any Route ancestor poisons the branch, a path-less ancestor (layout route)
  contributes nothing; plus `createBrowserRouter`/`createHashRouter`/
  `createMemoryRouter` object literals (same walker as angular). A `<Route>`
  without element/Component is grouping-only. Member tags (`<Foo.Bar/>`) and
  non-Route tags never match.
- **vue-router-route** — `createRouter({routes: [...]})`, incl. `{routes}`
  shorthand / identifier resolved like the angular case. Lazy
  `component: () => import(...)` emits the node without an edge (component
  key present, not an identifier). `.vue` SFCs are not parsed — only the
  `.js/.ts` router module, which is where routes live in practice.

## ⚖️ Decision: call-shaped custom routes are ROUTE PACKS, not config (2026-07-14, owner)

Doctrine: **core embedded + drop-in packs, exactly like grammar packs.**
Core = the big frameworks (recognizer code above) + yaml shapes (below);
packs = everything call-shaped that a declarative table CAN express — the
simple one-shape case Phase 1's table analysis carved out.

- A pack is one `<name>.json` in `.ctxoptimize/routes/` (repo, committable)
  or `<store-root>/routes/` (machine, default `~/ctxoptimize/routes`;
  `CTX_OPTIMIZE_STORE` relocates it). Repo wins on name collision — same
  precedence as grammar packs. Discovered at add time; malformed packs fail
  the add LOUDLY (grammar-pack precedent: never silently skip).
- Shape: `{"name": "myfw", "rules": [{"call": "registerRoute",
  "path_arg": 0, "handler_arg": 1, "method_arg": -1, "method": "GET"}]}`.
  `call` matches the callee's LAST identifier (`registerRoute` and
  `api.registerRoute` both match) in every language whose grammar maps call
  nodes — all embedded languages and any grammar pack declaring `calls`.
  Argument positions count the argument list's named children (positional;
  python kwargs occupy positions). Literal-or-silent: `path_arg` must be a
  plain quoted string (f-strings/template/raw strings never match);
  `method_arg` wins when it holds a literal, else fixed `method`, neither →
  the method-less ROUTE token (a set `method_arg` with no literal and no
  fixed fallback skips the site); `handler_arg` must be a bare identifier to
  earn a handles edge. Channel: `route-pack:<name>`.
- CLI mirror of `languages`: `routes list` (core + packs with source),
  `routes add <name>` (scaffold with a live example rule + `_review` marker;
  `--global` targets the machine dir), `routes add <github-url|pack.json
  url>` (fetch → validate → install; repo tarball via codeload like
  `languages add`, looking for `routes/*.json` else top-level pack-shaped
  `.json`), `routes remove <name>` (repo first, then global, says which).

## ⚖️ Decision: yaml route shapes are CORE, in the config lane (2026-07-14, W4)

OpenAPI/Swagger, Drupal `*.routing.yml`, and Kubernetes Ingress ride the
markdown producer's config lane (`internal/extract/markdown/yamlroutes.go`) —
the existing secret-name refusals and `maxConfigBytes` cap gate them. **No
yaml library** (stdlib-only rule): a small indentation-based line walker,
deterministic, scoped to exactly these three shapes. Multi-document files
split on `---` and match per document.

- **openapi-route** — `.yaml/.yml` with top-level `openapi:`/`swagger:`;
  under `paths:`, second-level `/…` keys × their get/post/put/delete/patch/
  head/options children → one node per method+path (`GET /users/{id}`),
  location = the method line, contains edge from the config doc (EXTRACTED —
  it IS in the file). `operationId:` literal → handles edge to that bare
  name (INFERRED, resolved cross-batch by the store, dangling is fine). No
  operationId → node only (a spec has no in-repo handler).
- **drupal-route** — `*.routing.yml` top-level entries with a `path:` child
  starting with `/` (`ROUTE /path`; `methods: [GET, POST]` inline or block
  list → one node per method); `_controller: 'Class::method'` → handles to
  the LAST `::` segment. No `::` in the controller → no edge (conservative).
- **ingress-route** — documents with top-level `kind: Ingress`: `path:`
  literals under the top-level `spec:` block (`ROUTE /path`);
  `backend.service.name` → handles target by name (dangling OK). BEST-EFFORT
  by design — anything the walker can't pin down is skipped silently.
- Guards measured, not assumed: docker-compose/Taskfile/goreleaser-shaped
  yaml yields zero routes; a repo-wide sweep of this repo asserts zero.
- Future extension (recorded, NOT built): yaml-shape packs — declarative
  key-path rules for other config-route dialects (Play `conf/routes` is not
  yaml and stays a core candidate).

## Deferred (not in this change)

- Django (`path`/`re_path`), Go/Java/C#/Rust lanes — same mechanism, more
  recognizers (route packs already cover the call-shaped subset: Gin/chi/
  gorilla-style `r.GET("/x", h)` is expressible as a pack today).
- Next/Nuxt/SvelteKit/Astro `pages/` file-based conventions (walk rules).
- Phase 3 bridges.
- yaml-shape route packs (see above).
