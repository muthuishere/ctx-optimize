# Tasks — framework routes + bridges (Phase 1 partially shipped 2026-07-14)

- [ ] ⚖️ Maintainer: confirm Phase 1 framework priority order
- [x] Prototype 2 recognizers (FastAPI, Express) both ways — data-table vs Go code — decide the mechanism in design.md → DECIDED: plain Go code (see design.md)
- [ ] Phase 1 recognizers + fixtures per framework; false-positive check on etcd/this repo
  - [x] FastAPI + Flask (python), Express + NestJS (ts/tsx) — `internal/extract/code/routes.go`; fixtures + near-miss guard + zero-hits-on-this-repo test in `routes_test.go`
  - [x] Frontend routers: Angular (forRoot/forChild/provideRouter), React Router (JSX + data routers), Vue Router — `internal/extract/code/frontend_routes.go`; ROUTE method token; fixtures + near-miss guard in `frontend_routes_test.go` (see design.md)
  - [x] ROUTE PACKS (maintainer doctrine: core embedded + drop-in packs) — declarative call-shaped rules in `.ctxoptimize/routes/` + `<store-root>/routes/`, repo wins; channel `route-pack:<name>`; loud failure on malformed packs — `internal/extract/code/routepacks.go` + `routepacks_test.go` (see design.md)
  - [x] `routes` CLI verb family mirroring `languages`: list / add <name|github-url|json-url> [--global] / remove — `internal/app/routes.go` + `routes_cli_test.go` (hermetic httptest fetch)
  - [ ] Django, go (net/http), java (Spring), c# (ASP.NET), rust (Axum/actix/Rocket) core recognizers (Gin/chi/gorilla-style calls already expressible as route packs); false-positive check on etcd
- [ ] Phase 2 file-based + config routes
  - [x] Config routes (yaml, no yaml lib — indent walker): OpenAPI/Swagger, Drupal `*.routing.yml`, k8s Ingress (best-effort) — `internal/extract/markdown/yamlroutes.go` + `yamlroutes_test.go`; docker-compose/Taskfile/goreleaser false-positive guards + repo-wide zero-routes sweep
  - [ ] `pages/`-style file-based conventions (Next/Nuxt/SvelteKit/Astro); Play `conf/routes`; Rails `routes.rb` (needs ruby pack)
- [ ] objc grammar pack (prereq for Swift↔ObjC bridge)
- [ ] Phase 3 bridges with small/medium/large validation matrix per channel
