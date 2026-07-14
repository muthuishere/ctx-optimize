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

## Deferred (not in this change)

- Django (`path`/`re_path`), Go/Java/C#/Rust lanes, React-Router — same
  mechanism, more recognizers.
- Phase 2 file-based/config routes; Phase 3 bridges.
- Recognizer tables for grammar packs (re-open if a pack needs routes).
