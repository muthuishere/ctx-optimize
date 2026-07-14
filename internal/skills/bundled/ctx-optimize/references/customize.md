# Customizing extraction — you are the customization helper

ctx-optimize extracts more than plain code: framework **routes**, build-tool
**dependencies**, **Kubernetes** topology, extra **languages**, and anything
else via adapters. Most of it works with zero setup. When a user's stack
isn't covered out of the box, your job is to add it — declaratively, with a
drop-in pack, never by editing the binary.

**The doctrine (say it to the user): core is embedded, everything else is a
drop-in file.** Four extension axes, all discovered on every `add`, all
committable so the whole team inherits them:

| axis | core (works now, no setup) | extend with | lives in |
|---|---|---|---|
| routes | fastapi, flask, express, nestjs, angular, react-router, vue-router, openapi, drupal, ingress | **route pack** (`routes add`) | `.ctxoptimize/routes/*.json` |
| manifests | package.json, pom.xml, csproj/sln, go.mod, gradle, **k8s** | **manifest pack** (`manifests add`) | `.ctxoptimize/manifests/*.json` |
| languages | 12 embedded (go/py/js/ts/tsx/java/c/c++/c#/rust/zig/sql) | **grammar pack** (`languages add`) | `.ctxoptimize/grammars/` |
| everything else | code + markdown + config | **adapter** (see `adapters.md`) | `.ctxoptimize/adapters/` |

`routes list` / `manifests list` / `languages list` show core + installed
packs. Repo packs beat machine (`~/ctxoptimize/…`) packs on name collision. A
malformed pack fails the `add` loudly — never silently skipped.

## Routes — is the framework core, or does it need a pack?

1. **First check `routes list`.** If the user's framework is already core,
   just `ctx-optimize add .` — routes appear as `kind:"route"` nodes
   (`GET /users/{id}`) with `handles` edges to the handler. Confirm with
   `ctx-optimize query "GET /users"` or `card <handler>` (the route shows in
   its callers).
2. **Custom / in-house framework** (`registerRoute("/x", handler)`,
   `router.on(...)`, any call-shaped convention): scaffold a **route pack**.
   ```
   ctx-optimize routes add myframework      # writes .ctxoptimize/routes/myframework.json
   ```
   Then EDIT the scaffolded rule to match the real call. Rule fields:
   ```json
   {"call": "registerRoute", "path_arg": 0, "handler_arg": 1, "method_arg": -1, "method": "GET"}
   ```
   - `call` — the function name matched (last identifier: `api.registerRoute`
     matches on `registerRoute`).
   - `path_arg` — 0-based index of the string-literal path argument.
   - `handler_arg` — index of the handler; an identifier earns a `handles`
     edge (resolved by name), anything else = route node only. Omit / `-1`
     for none.
   - `method` — fixed HTTP method; OR `method_arg` ≥ 0 to read it from a
     literal argument (method_arg wins when literal, else falls back to
     `method`, else `ROUTE`).
   Works across every language whose grammar exposes calls (all 12 embedded +
   grammar packs). Remove the `_review` marker once you've checked it, then
   `ctx-optimize add .`.
3. **Recognizer too complex for a rule** (middleware chains, stateful
   controller+method composition like Express/NestJS): that's why those are
   *core*, not packs — a flat call-rule can't express them. If a user needs a
   genuinely new complex recognizer, it's a core contribution, not a pack;
   say so and fall back to an adapter for the interim.

Install a shared pack from a teammate or a public repo:
`ctx-optimize routes add https://github.com/org/route-packs` (or a direct
`.json` URL).

## Kubernetes and build tools — the manifest lane

K8s and the five build tools are **core** — `ctx-optimize add .` already
emits them. Confirm and explain, don't rebuild:

- **Dependencies** are version-free nodes (`dep:npm/express`,
  `dep:maven/org.apache.kafka:kafka-clients`) with the version on the
  `declares` edge — so the SAME lib across Maven + Gradle + modules is ONE
  node. `ctx-optimize affected dep:npm/express` → every module that uses it.
- **K8s** → resource nodes (`k8s://ns/kind/name`), `selects` (Service→pod by
  label), `routes_to` (Ingress→Service), `mounts` (→ConfigMap/Secret),
  `uses_image`. `card k8s://default/service/api` shows the topology. Secret
  resources: node only, data never read. Helm templates (`{{ }}`) are
  skipped — tell the user, and use an adapter if they need rendered values.
- **Custom structured manifest** (an in-house `*.deps.json`, a bespoke
  build descriptor): scaffold a **manifest pack**:
  ```
  ctx-optimize manifests add internal-deps   # .ctxoptimize/manifests/internal-deps.json
  ```
  Rule = a tiny path selector over a structured file:
  ```json
  {"file": "*.deps.json", "format": "json", "path": "libraries.*",
   "emit": "dependency", "namespace": "internal"}
  ```
  `format` ∈ json|xml|yaml; `path` is a dot path with `*` wildcard for
  json/yaml (trailing `*` over a map → name=key/version=value) or an element
  path with `/@attr` for xml; `emit` ∈ dependency|task. If the selector can't
  express it (predicates, joins, sibling lookups), the answer is an ADAPTER,
  not a bigger language — say so.

## A new language

`ctx-optimize languages add kotlin` (known name) or
`languages add <tree-sitter-github-url>` builds a grammar pack in pure Go (no
toolchain to install — zig is auto-fetched, sha256-verified). Review the
auto-suggested `.json` node-type mapping (marked `_review`), then `add .`.
See the routing table's language row.

## The workflow you drive for the user

1. `routes list` / `manifests list` / `languages list` — is it already core?
   If yes, just `add .` and confirm with a query. Done.
2. Not core but declarative (call-shaped route / structured manifest / known
   language): scaffold the pack, edit the rule to their real shape, drop
   `_review`, `add .`, verify with a query/card.
3. Not expressible declaratively: an adapter (`adapters.md`) — you introspect
   and print a batch.
4. Commit `.ctxoptimize/` — the pack travels with the repo, the whole team's
   agents inherit it.
5. Everything is also visible and editable in the dashboard
   (`ctx-optimize serve` → Settings): every pack across all four axes, with
   its file path. Changes there are audited (`ctx-optimize log`).

Never edit the Go binary. Never hand-write graph nodes. The pack (or adapter)
IS the customization, and it's a plain committable file.
