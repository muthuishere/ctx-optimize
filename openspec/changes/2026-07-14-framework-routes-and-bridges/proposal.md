# ADR — framework-aware routes + cross-language bridges (CodeGraph parity, our architecture)

Status: DRAFT v1 — owner review pending 2026-07-14. Separate change from
`2026-07-14-competitive-wedge/` on owner instruction; nothing implemented.

## Context

CodeGraph (54K⭐, the SQLite+MCP competitor) ships two extraction features we
lack, both squarely inside our deterministic contract (pattern recognition
over parsed ASTs — no model, no network):

1. **Framework-aware routes.** Routing declarations become route nodes linked
   by references edges to their handler functions/classes, so "who calls
   this view?" surfaces the URL pattern that binds it. Their coverage:
   Django/Flask/FastAPI, Express/NestJS, Laravel, Drupal, Rails, Spring,
   Play, Gin/chi/gorilla, Axum/actix/Rocket, ASP.NET, Vapor, React
   Router/SvelteKit, Vue/Nuxt, Astro.
2. **Cross-language bridges.** Static extraction stops at language
   boundaries; they synthesize edges across them: Swift↔ObjC (@objc
   auto-bridging rules), React Native legacy bridge (RCT_EXPORT_METHOD /
   @ReactMethod), TurboModules (Codegen spec as ground truth), native→JS
   event channels (keyed by literal event name), Expo Modules DSL,
   Fabric/Paper view components. Each synthesized edge is tagged
   `provenance:'heuristic'` + `metadata.synthesizedBy:<channel>`, validated
   on a small/medium/large real codebase per bridge.

Why it matters for us: a route or a bridge is exactly the edge an agent
cannot get from grep, and exactly what makes `affected`/`path` answers right
in web and mobile codebases — the two biggest agent audiences. Today our
call edges are name-matched within a module (INFERRED) and stop at every
framework indirection and every language boundary.

## Fit with our architecture (what transfers, what doesn't)

- **Provenance maps cleanly.** Our schema already carries edge `confidence`
  (EXTRACTED | INFERRED) and node/edge `metadata`. Adopt CodeGraph's honesty
  verbatim: synthesized edges are `INFERRED` + `metadata.synthesized_by:
  <stable channel name>` (e.g. `flask-route`, `swift-objc-bridge`,
  `rn-event-channel`). The agent skill already explains confidence.
- **Recognition is post-parse, per-language.** We hold full tree-sitter ASTs
  in `internal/extract/code`. Routes are decorator/call-expression patterns
  with string-literal arguments — table-driven matching over nodes we
  already visit. No new parse pass.
- **Route node shape.** `kind: "route"`, label = `GET /users/:id`, source =
  file, location = line; edge route →(handles, INFERRED)→ handler decl.
  Config-file routes (Drupal yml, Play conf/routes) ride the existing
  markdown/config producer.
- **Language gaps are real.** Embedded langs cover
  py/js/ts/go/java/c#/rust — most of the route table. Ruby (Rails), PHP
  (Laravel), Swift (Vapor) are grammar packs; **ObjC has no grammar at all
  yet** — the Swift↔ObjC bridge needs an objc pack first. Kotlin pack exists
  (RN @ReactMethod side).

## Proposal — three phases, each shippable and measurable

### Phase 1 — routes for embedded languages (the 80%)

Framework recognizers, table-driven, in priority order of agent audience:

| lane | frameworks |
|---|---|
| python | FastAPI (`@app.get/@router.post`), Flask (`@app.route`, blueprints), Django (`path/re_path/include`, `.as_view()`) |
| js/ts | Express (`app.get/router.post` + middleware chains), NestJS (`@Controller`+`@Get/...`, `@Resolver`), React Router / file-based (Next/Nuxt/SvelteKit/Astro `pages/`) |
| go | net/http `HandleFunc`, Gin/chi/gorilla `r.GET(...)` |
| java | Spring `@GetMapping/@PostMapping/@RequestMapping` |
| c# | ASP.NET `[HttpGet("/x")]` |
| rust | Axum/actix/Rocket `.route("/x", get(handler))` |

Design constraint ⚖️: recognizers as a **data table** (pattern spec →
emission) rather than per-framework Go code, so grammar packs can ship
recognizer tables the same way they ship node-type mappings. Decide in
design.md after prototyping two frameworks — a table that can't express
Express middleware chains is worse than plain code.

### Phase 2 — file-based route conventions + config routes

`pages/`-style conventions (Next/Nuxt/SvelteKit/Astro incl. `[param]` /
`[...rest]`) and config-file routes (Drupal `*.routing.yml`, Play
`conf/routes`, Rails `routes.rb` once the ruby pack lands). These are walk +
filename/content rules, not AST work.

### Phase 3 ⚖️ — cross-language bridges (mobile)

Adopt CodeGraph's channel list in dependency order: RN legacy bridge +
TurboModules + event channels + Expo DSL (kotlin pack + embedded js/ts —
buildable now), then Swift↔ObjC (**blocked on an objc grammar pack**;
`languages add` can build it — tree-sitter-objc exists), then Fabric/Paper
view components. Each bridge validated CodeGraph-style on a small + medium +
large real repo before it's claimed in README (their validation matrix is
the right discipline — steal it).

## Success checks

- On a FastAPI + a Spring + an Express fixture: `query "GET /users"` returns
  the route node; `card <handler>` lists the route in callers; `affected
  <handler>` includes the route. All new edges INFERRED + `synthesized_by`.
- Zero recognizer hits on repos without the framework (no false-positive
  noise on this repo / etcd) — measured, not assumed.
- Gather time on beam within +10% of today (recognizers ride the existing
  AST visit).
- Phase 3 bridges each validated on their small/medium/large matrix before
  README mention.

## Non-goals

- No runtime tracing, no LSP, no framework version resolution — pattern
  recognition on source only, same as everything else in the binary.
- No new producer: routes/bridges ride the `code` producer's batch (same
  Replace/prune lifecycle).
