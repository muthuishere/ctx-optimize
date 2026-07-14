# Tasks — framework routes + bridges (Phase 1 partially shipped 2026-07-14)

- [ ] ⚖️ Owner: confirm Phase 1 framework priority order
- [x] Prototype 2 recognizers (FastAPI, Express) both ways — data-table vs Go code — decide the mechanism in design.md → DECIDED: plain Go code (see design.md)
- [ ] Phase 1 recognizers + fixtures per framework; false-positive check on etcd/this repo
  - [x] FastAPI + Flask (python), Express + NestJS (ts/tsx) — `internal/extract/code/routes.go`; fixtures + near-miss guard + zero-hits-on-this-repo test in `routes_test.go`
  - [ ] Django, go (net/http/Gin/chi/gorilla), java (Spring), c# (ASP.NET), rust (Axum/actix/Rocket), React Router; false-positive check on etcd
- [ ] Phase 2 file-based + config routes
- [ ] objc grammar pack (prereq for Swift↔ObjC bridge)
- [ ] Phase 3 bridges with small/medium/large validation matrix per channel
