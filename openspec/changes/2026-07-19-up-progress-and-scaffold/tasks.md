# Tasks
- [x] project.EnsureSamples(repo, name) — writes only MISSING templates, returns created paths; Scaffold delegates to it
- [x] cmdUp calls EnsureSamples when a config exists; reports what was created
- [x] runMultiAdd emits `gathering N modules (jobs=J)…` + `[i/N] label` ticks to progressOut (stderr)
- [x] progressOut package var so tests capture ticks; stdout untouched
- [x] test: config.json-only repo → all templates present, config + edited sample byte-identical, custom store name honored, second up re-scaffolds nothing
- [x] test: fan-out emits header + ticks on progressOut, nothing leaks to stdout
- [x] task ci + task golden green; scenario matrix 33/33; volentis dogfood shows live ticks
- [x] docs: cli.md, monorepos.md, cookbook.md
