# Design, config layout, and lineage

## Evidence-first

Every product decision traces to a measured spike
(`openspec/changes/2026-07-11-graphify-gaps/spikes.md`) — including honest
benchmarks against a real agent baseline (not corpus-stuffing strawmen), the
terrain law (graph value is inverse to a codebase's greppability), and the
symbol-card finding (agents' reads are pointer-chases a complete answer
eliminates). Claims that failed measurement are retired in public: the
universal token-savings claim is dead (S16 — Claude Code −0.2%, Codex +3.0%),
and what survived is impact-answer correctness, onboarding traces, and wall
time. The standing counter-argument lives in [CRITIQUE.md](CRITIQUE.md); the
long-term position in [VISION.md](VISION.md).

Extensibility is a verified differentiator, not a slogan: a source audit of
graphify (2026-07-11) found its languages, data-source lanes and exporters are
all fork-required static registries (only its remote hooks are user-pluggable).
Here languages are drop-in packs, adapters are dropped scripts, and the batch
door takes any producer.

## `.ctxoptimize/` — config that travels with the repo

The store itself lives outside your repo, at `~/ctxoptimize/<repo-name>/`, as
plain sorted ndjson/json/md. The only thing committed into the repo is this
directory:

```
.ctxoptimize/
  config.json          name + remote commands + sources[] (+ modules[] in a monorepo)
  instructions.md      the committed usage card agents read — managed block,
                       version-stamped, refreshed by `up` (upgrade-only; your
                       edits outside the markers are never touched)
  adapters/            drop scripts here — every .js/.py/.sh runs on `add`
  push.js / pull.js    your transport scripts (init writes an inert *.sample pair)
  remote.example.md    transport recipes: git lane, s3 lane, custom
  grammars/            optional language packs that travel with the repo
  (no secrets here)    source URLs with secrets live in the environment,
                       your root .env, or ~/.config/ctx-optimize/.env
                       (machine-global, outside the repo) — never in config
```

```json
{
  "name": "my-module",
  "remote": {
    "push": "node .ctxoptimize/push.js",
    "pull": "node .ctxoptimize/pull.js"
  }
}
```

Commit the directory — it is safe by construction:

- `name` picks the store folder under `~/ctxoptimize/` (default: repo basename).
- `remote` declares the push/pull commands — plain shell lines the binary runs
  as-is (cwd = repo root). See [remote-github.md](remote-github.md).
- Secrets stay env-var **names** in scripts and config alike; the shell expands
  them at run time. A literal password in a source entry is a hard error.
- **Adapters are files**: dropping `kafka.js` into `.ctxoptimize/adapters/` is
  the whole registration. See [adapters.md](adapters.md).

## Fast lane / slow lane

Adapters can be arbitrarily slow (DB dumps, doc converters), so they get their
own lanes: `sync` re-gathers the repo you're in and **skips** them (safe —
replace is producer-scoped, so adapter nodes stay put), `adapters run [name]`
re-runs all or one on demand, and `add --no-adapters` is the fast lane spelled
long. One `add` refreshes the whole world; a fresh clone needs zero setup —
`ctx-optimize up`.

## Lineage

With all due respect to graphify — a project we learned a great deal from —
there is a direct line between it and this tool: graphify's central graph store
and its pluggable remote push/pull hooks (the one part of graphify an end user
can extend without forking) were contributed upstream by this project's author
(graphify #1751 / #1752; git-verifiable). ctx-optimize is that same idea carried
through the whole product: the store, the languages, the adapters, and the sync
are all open seams by design — nothing here requires a fork to extend.
