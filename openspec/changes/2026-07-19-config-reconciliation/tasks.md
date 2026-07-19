# Tasks — config reconciliation (narrowed: add + up only)

- [x] add/guard: residual root gather skips the count-shrink refusal when the config declares modules[]; module + single-module stores keep the guard unchanged
- [x] up: existence/refresh decision walks the DECLARED module set (config in hand); root store node count gates nothing; missing module stores gather individually, populated+fresh ones untouched
- [x] hermetic test (a): module-list edit → residual re-gather does NOT trip the guard
- [x] hermetic test (b): module store still refuses a real >50% shrink
- [x] hermetic test (d): broken/empty root + populated module stores → up refreshes the residual only, never full fan-out
- [x] hermetic test (e): module added to config (no commit) → next up gathers exactly it
- [x] task ci + task golden green; single-module byte-identical
- [x] dogfood: volentis converges with one up, zero --force, zero full rebuilds
- [ ] DEFERRED: failed-gather surfacing in up summary; orphan-store logging
