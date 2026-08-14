# ADR 8 — the only lever left is parsing less

Status: DRAFT — owner review pending 2026-08-14. No product code until agreed.
Scope: `internal/extract/code` file admission (what we parse), plus whatever
the graph loses by skipping it — which is why this is a PRODUCT decision, not a
perf tweak.
Written from the 2026-08-14 pprof pass, which answers the owner's question
*"there are atomic and so much better ways in golang i am not sure you have
tried all"* with: **there were two, they are taken, and the rest is not a Go
problem.**

## Where the time actually goes (kubernetes, 17.9k files, CPU profile)

| share | site |
|---|---|
| **58.1%** | `runtime._ExternalCode` — tree-sitter parsing, inside wasm |
| **15.6%** | `memclrNoHeapPointers` — zeroing; 23% of it `wazero MemoryInstance.Grow` |
| 7.2% | `syscall.rawsyscalln` — reading files |
| 5.4% | `memmove` — wasm memory copies |

Allocation was the surprise: **8.3 GB for a 2.5s gather**, 71% of it wazero
linear memory (`Grow` 4.6 GB, `NewMemoryInstance` 1.28 GB). Guest instances
start at **64 MB**, and a single pathological 1.8 MB generated `.pb.go` drives
one worker's output buffer to **282 MB**.

## What Go-side tuning bought (taken, measured)

| change | effect |
|---|---|
| symbol-table cache, process-wide, keyed by engine key | 4-module gather **12.39 → 11.00 GB (−11.2%)**, allocs **−8.9%** |
| pre-sized collector slices/maps | java-spring **−1.9%** bytes |

`loadSyms` was spinning up a dedicated 64 MB instance per engine key and
throwing it away, and `ExtractPaths` runs **once per module** — so a 6-module
monorepo paid ~478 MB six times for a byte-identical answer.

## What Go-side tuning cannot buy — the honest answer to "atomics"

**The mutex and block profiles show no contention.** The worker pool already
accumulates per-worker and merges serially, which is the shape atomics and
lock-free structures exist to rescue you from. There is nothing for
`sync/atomic`, `sync.Pool` sharding, or lock-free collection to bite on.

Rejected, each with the number that killed it:

- **Guest heap prewarm** — designed to collapse incremental `memory.grow`.
  A no-op: instances already start at 64 MB, so a 7 MB prewarm never triggers a
  grow. Grow allocations went 4.63 → 4.83 GB. Reverted.
- **`WithMemoryCapacityFromMax(true)`** — wazero's own docs confirm the default
  re-allocates, but eager allocation needs `WithMemoryLimitPages`, and measured
  peak is 282 MB × 17 workers ≈ **4.8 GB RSS**. Not worth it.
- **Larger `out_buf` growth steps** — already geometric
  (`cap = (out_len+need)*2 + 4096`), so growth is log-step. Not a bug.

⇒ **58% of the time is inside the wasm parser and ~15% is zeroing driven by
output volume.** Neither is reachable from Go. The remaining lever is not
"parse faster", it is **parse less**.

## D1 — a generated-file heuristic, beside the existing minified check

`maxFileBytes` (2 MB) and `isMinified` (a >50k-byte line) already admit-or-skip
files. They do not catch **generated source that looks hand-written**:
`*.pb.go`, `*_generated.go`, `zz_generated.*`, `*.g.dart`, `*_pb2.py`,
`*.designer.cs`, files carrying a `// Code generated … DO NOT EDIT.` header
(a Go convention with an exact regexp in the toolchain), and OpenAPI/gRPC
client dumps.

The kubernetes evidence is stark: **one 1.8 MB generated protobuf yields ~8.7 M
AST records and a 282 MB output buffer** — for symbols nobody asks about by
name, in a file nobody edits.

## D2 — this is a product decision, and must be argued as one

Skipping generated files **changes the graph**, so it cannot be justified by
speed alone:

- **For**: they are not authored, not edited, and not what "where is X
  implemented" means. They inflate node counts (making our own benchmark
  numbers flattering for the wrong reason) and cost the most per byte.
- **Against**: `affected` and `change-plan` traverse *callers*, and generated
  code genuinely calls hand-written code. Dropping the file drops those edges,
  so a blast radius could silently shrink. That is the exact failure this
  project refuses elsewhere.

Therefore: **skip = declare, never delete silently.** Options to measure —
(a) skip entirely and report the count, (b) index declarations but not call
bodies, (c) keep them but mark `metadata.generated:true` so consumers can
filter while traversal stays complete. **(c) is the shape most consistent with
this codebase's doctrine** (label uncertainty, never hide it) and costs the
least recall — but it saves less time, so it must be measured, not assumed.

## Gates

- Judged scoreboard may not move DOWN (16.5 / 13.0). If skipping generated
  files costs a mark, the option is wrong.
- `affected`/`change-plan` on a hand-written symbol called from generated code:
  before/after edge counts, explicitly compared.
- Node-count floors move DOWN by design under (a)/(b) — a reviewed diff with a
  note, per the linux-block precedent.
- Perf: report the actual saving per option. If (c) saves little, say so and
  let the owner choose recall over speed with the numbers in hand.

## Note for whoever measures this

Wall-clock A/B on a loaded machine is unusable — the same kubernetes comparison
swung +16.8% / +4.9% on 2026-08-14. Use allocation counts (deterministic) or a
quiet machine. See the error-bars note in ADR 6.
