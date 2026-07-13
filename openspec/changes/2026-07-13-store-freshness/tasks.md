# Tasks — store freshness

- [ ] 1. `internal/freshness`: `Source`, `Report`, pure `Evaluate` + stdlib-only test
- [ ] 2. `internal/store`: `RecordSource(path, head, headUnix, addedUnix)` + `Sources()` reading/writing `source.json` (atomic, sorted)
- [ ] 3. CLI git shim: best-effort `gitHead(dir)` (rev-parse + committer time), never fatal
- [ ] 4. Wire `cmdAdd`: after a successful add of a git working tree, RecordSource
- [ ] 5. `cmdStatus`: add `fresh:` line + `freshness` json block
- [ ] 6. New `cmdFresh` verb + exit codes (0/1/2) + `--json`; register in dispatch + help
- [ ] 7. New app-level test proving the CEO stale case (real temp git repo, add → commit → stale)
- [ ] 8. `task ci` green; dogfood on brain repo; reproduce benchmark
