# `up` UX: live progress for fan-out + scaffold the missing templates

Date: 2026-07-19 · Status: ADOPTED (owner 2026-07-19: "so just a progress
bar and then config.json is alone there, just add all others stuff / just
put templates")

## Problem — two reports, both reproduced

**1. No progress.** `runMultiAdd` buffers every worker's output into a
private `bytes.Buffer` and prints nothing until `wg.Wait()` returns. On a
monorepo (volentis: 19 modules) the terminal is silent for the whole run,
then dumps everything at once. Buffering exists so concurrent workers never
interleave — correct goal, but it costs all visibility.

**2. A hand-written config.json yields a half-populated `.ctxoptimize/`.**
Reproduced: copy only `config.json`, run `up` →

```
config.json          ← yours
instructions.md      ← created
(no adapters/example.js.sample, push.js.sample, pull.js.sample, remote.example.md)
```

Mechanism: `upCore`'s bootstrap lane (which calls `cmdInit` → `Scaffold`)
fires ONLY when `sc.cfg == nil`. With a config present it is skipped, while
`cmdUp` calls `EnsureInstructions` unconditionally — hence exactly one file.
The user is left with no template to learn adapters or transports from.

## Decision

### 1. Progress lines on STDERR, stdout unchanged

As each module's gather completes, emit one line to stderr:

```
gathering 19 modules (jobs=8)…
[1/19] apps/libreadmin
[2/19] apps/librechat
...
```

- **stderr, not stdout**: piping stdout to a file stays clean, and every
  existing stdout assertion (including the byte-for-byte determinism test
  across `--jobs`) is untouched. Conventional for progress (curl, npm).
- **Plain lines, no `\r` rewriting**: CI logs stay readable; no TTY
  detection needed.
- **Detailed per-producer output keeps printing to stdout in task order**
  after the wait — deterministic as today, no duplication.
- Written through a package var (`progressOut`) so tests can capture it.
- Single-module `add .` already streams producer lines live; unchanged.

### 2. `up` fills in missing template files

`up` calls the sample-scaffolding step whenever a config exists (not just
on bootstrap). Safe by construction: it only ever writes files that are
ABSENT — a brought-in `config.json`, edited samples, and user text are
never touched. Reported when anything was created:
`scaffolded 4 missing template files in .ctxoptimize/ — commit them`.

Refactor: the seed-writing loop inside `project.Scaffold` becomes
`project.EnsureSamples(repo, name) ([]string, error)` returning the paths
created; `Scaffold` calls it (behavior identical for `init`).

Accepted trade-off: a team that deliberately deleted a sample gets it back
on the next `up`. Judged the lesser cost — the samples are inert, and
discoverability of the adapter/transport lanes matters more.

## Gates

- Hermetic: config.json-only repo + `up` ⇒ all samples present, config
  byte-identical, store key honored.
- Hermetic: progress lines captured for a multi-module fan-out; stdout
  byte-identical across `--jobs 1` vs `--jobs 8` (existing test).
- `task ci` + `task golden` green; scenario matrix 33/33.
