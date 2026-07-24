# Spikes — lazy-autosync / incremental re-sync (2026-07-24)

Two measurements: an inline perf spike (this repo) + a full stress-test of the
two-phase design. **The perf spike invalidates the ADR's core thesis.** Details:

## 1. Perf — today's warm ≈ cold (confirmed), and parsing is NOT the cost

Inline spike (this repo, ~500 files), and stress-test cross-check (opencode ~31 modules):

| | cold | 0-change warm | 1-file |
|---|---|---|---|
| this repo | 0.91s | **0.73s** | 0.72s |
| stress-test cross-check | 0.85s | 0.71s | 0.71s |

Warm 0-change is ~cold even though it "added 0 nodes" — today's sync is fully
non-incremental. **Confirmed.**

**Sub-step breakdown (stress-test, ~500 files) — the thesis-killer:**
- code extract **405 ms dominates**, but inside it:
  - wazero `CompileModule` of the 32 MB wasm = **225 ms (fixed, per process,
    per module in a monorepo)**
  - symbol load ~80 ms
  - **parsing all 500 files = only ~78 ms**
  - Phase-2 resolve = **< 10 ms**
- A 1-file extract still costs ~304 ms (the fixed engine-init floor); the whole
  repo only ~381 ms.

⇒ **Skipping tree-sitter on unchanged files (the ADR's Part A centerpiece)
saves only ~78 ms of a ~730 ms warm sync.** The 225 ms wasm-compile floor ALONE
exceeds CodeGraph's 0.13–0.20 s warm. The AST-per-file cache is the complex,
risky part and buys the least.

## 2. Determinism — the two-phase design is SOUND

- Phase 2 (call/route resolution, `internal/extract/code/code.go:325-396`) runs
  after all files merge and is a **pure function of the assembled intermediates**,
  order-independent (15 parallel gathers → identical graph hash; the four S1
  adversarial edits all correctly drop the unchanged file's INFERRED edge).
- **MUST-HANDLE correction to the ADR:** Phase 2 resolves over unexported
  `fileResult{nodes, edges, calls[], decls[], routes[]}`, NOT the graph node set
  — `declRef.label` is unqualified vs the node's qualified `Label`, and dropped
  calls have zero graph representation. So `cache/ast` MUST persist the whole
  `fileResult`, not just graph rows. The ADR's Phase-1 wording is wrong here.
- S4 (atomic writes): store already does temp-write + `os.Rename`
  (`internal/store/store.go:551`) — safe. S6 (detached child): no spawn exists
  yet; needs per-GOOS build-tagged `SysProcAttr` (Unix `Setsid`, Windows
  `CREATE_NEW_PROCESS_GROUP|DETACHED_PROCESS`).

## 3. Verdict — reshape the design

Go/no-go on the ADR's claims:
- `block` 1-file ≤ 0.13 s (beat CodeGraph) → **NO** as specified (~300–350 ms
  floor from wasm compile, not parsing).
- `lazy` adds 0 ms to query → **YES**, unconditionally — the honest headline.
- 0-change ≈ manifest-scan → **YES, but only if detect short-circuits BEFORE
  engine init** (never touch wazero when nothing changed).

**The real levers (missing from the ADR):**
1. **0-change short-circuit** — detect (manifest stat/hash scan) says nothing
   changed → return before engine init. Biggest, cheapest win; needs NO AST
   cache. Collapses warm 0-change from 730 ms to ~manifest-scan.
2. **Persisted wazero compilation cache** (`wazero.NewCompilationCache` to disk)
   + ONE shared compiled module across monorepo modules — removes the 225 ms
   (×N-modules) floor. This is what actually approaches CodeGraph, not the AST
   cache.
3. **lazy autosync** — 0 ms added query latency. Ship this framing.
4. Per-file AST cache — DEFER/DROP: saves ~78 ms, highest complexity, the
   fileResult-persistence gap above.

Recommended framing: "lazy = 0 ms added latency + provable determinism + zero
infra + a tenth the disk," NOT "beats CodeGraph on block."

## 4. IMPLEMENTED + measured (2026-07-24)

Built levers 1+2 and the safe lane-skips (AST cache deferred per owner; lazy
autosync on query still pending). Measured on this repo (~500 files):

| path | before | after | how |
|---|---|---|---|
| cold gather | 0.91s | **0.57s** | lever 2: disk-backed wazero compilation cache (225ms compile → cache hit; machine-local `os.UserCacheDir`, never in a store) |
| 0-change re-sync | 0.73s | **0.02s** | lever 1: tree stat-signature short-circuit before engine init |
| 1-file edit (graph unchanged) | 0.54s | **0.37s** | skip git-history when HEAD unchanged + skip wiki regen when graph unchanged (both byte-identical guards) |

Determinism: short-circuit / skips are byte-identical to a `--force` rebuild
(`after-cold == after-skip == after-force`). Six scenario tests pin it:
file edit / add / delete, folder delete, ignored-folder delete (short-circuits,
never in graph), module delete (dropped from navigator+federation, orphan
reported not auto-deleted). `task ci` + `task golden` green.

Correctness bugs the tests caught pre-merge: (a) tree-sig skipped
`.ctxoptimize/` → pack/adapter/config changes wrongly seen as no-change (fixed:
skip only `.git`/build dirs); (b) wasm cache under the store root polluted
synced stores (fixed: machine-local cache dir).

Still pending: **lazy autosync on query** (detached child + `autosync` config +
per-GOOS SysProcAttr) — the 0ms-latency "auto" layer on top of this fast sync.
