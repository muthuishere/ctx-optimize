# ADR — lazy auto-sync on query + cheap incremental re-sync

Status: DRAFT — 2026-07-24. For DISCUSSION before any code (repo change-flow).
Motivated by a MEASURED loss, not a hunch.

## Why now — the measured gap

The 2026-07-24 cross-tool benchmark (audited, `~/ctx-bench-arena/AUDIT.md`) found
ctx-optimize's single most consistent loss:

- **CodeGraph's warm/sync path beats ctx-optimize 3–8× on every corpus**
  (0.13–0.20s vs 0.55–1.23s). CodeGraph re-syncs incrementally; ctx-optimize
  has **no incremental fast-path** — `add` on an unchanged tree costs the same
  as cold (audit log 004: "added 0 nodes" yet full-length pass).
- ctx-optimize's `warm_s` is even *slower* than cold on the big corpus
  (1.23s vs 1.17s) — full re-extraction plus wiki regen, every time.

Owner directive: match CodeGraph — auto-sync, config-gated, lazily on query —
AND fix the re-sync itself (make it genuinely incremental). Answer quality is a
separate follow-up, tracked but out of scope here.

## What already exists (don't rebuild)

- **Staleness detection**: `internal/freshness` + `cmdFresh` (exit 0 fresh / 1
  stale vs git HEAD); `cmdStatus` reports per-module freshness.
- **Content-hash manifest**: `Store.UpdateManifest` / `hashFile` already stamp
  `{mtime, size, hash}` per file — the *detect* half of incremental is here.
- **A `sync` verb that is a MISNOMER**: `sync` (app.go:73) is literally
  "`add .` minus adapter scripts" — a **full re-extract**, not incremental. And
  `up` branches "stale vs HEAD → re-gather / fresh → no-op" (app.go:257) — the
  fresh path is a cheap no-op, but the stale path re-gathers EVERYTHING. So
  today: 0-change is fast (no-op), but a 1-file edit costs a full rebuild.
  **This change makes `sync`/`up`'s stale path genuinely incremental** — the verb
  finally does what its name says.
- **A full incremental DESIGN, unimplemented**:
  `openspec/changes/2026-07-11-graphify-gaps/design.md` specs content-hash
  invalidation, a versioned `cache/ast/{filehash}.json`, evict-and-reconcile,
  and community-pinned wiki re-distill. **This ADR proposes finally building the
  code-graph half of it** (the wiki/distill half stays deferred — we have no
  LLM in the binary anyway).

## The two parts

### Part A — cheap incremental re-sync (fix `add`/`up` sync)
Implement the *rebuild* half of the 2026-07-11 design, code-graph only:
1. **detect** — stat fast-path (mtime+size) → hash slow-path → {changed, added,
   deleted} file set, from the existing manifest.
2. **reuse** — unchanged files: pull their nodes/edges from a versioned
   `cache/ast/v{extractorVer}/{filehash}.json` instead of re-parsing.
3. **re-extract** only changed/added files (tree-sitter as today).
4. **reconcile** — evict nodes/edges whose source file changed or was deleted,
   keep the rest, merge the freshly-extracted set. Producer-scoped `Replace`
   already exists; this narrows it to the touched files.
5. Determinism preserved — same sorted output, same content-hash identity; the
   cache is a speed optimization, never a source of truth.
   *Expected result: warm re-sync drops from ~full-cold to O(changed files) —
   the CodeGraph-class fast path.*

### Part B — lazy auto-sync on query (config-gated)
Today `query` answers from whatever is in the store, stale or not. Add an
opt-in: when configured, `query` (and the other read verbs) checks freshness
first and, if stale, runs the Part-A incremental sync **before** answering.

- **Config key** (in `.ctxoptimize/config.json`, committable, team-wide):
  `"autosync": "off" | "lazy" | "block"` (default **off** — no surprise work,
  deterministic-by-default doctrine).
  - `lazy` — first stale query fires the incremental sync on a **separate async
    channel and answers immediately** from the current (slightly stale) store;
    the *next* query sees the refreshed store. Owner's "whenever a second query
    happens" semantics — sync kicks off on query 1, query 2 gets it.
  - `block` — stale query syncs first, then answers (always fresh; only the
    changed-file delta, cheap after Part A).
- Env override `CTX_OPTIMIZE_AUTOSYNC=off|lazy|block` for CI/hook control.
- Never creates store dirs on a read path (preserves "read never writes a
  store") — auto-sync only fires when a store already exists and is declared.

**LOCKED — mechanism (owner):** NO cron, NO daemon, NO watcher. The query
process itself spawns a **detached child `ctx-optimize` sync process** — a
"separate channel" for that one sync — and returns. A **lockfile/PID guard** in
the store makes concurrent queries no-op the spawn (one sync in flight, never a
stampede). This keeps the no-server doctrine: the binary starts nothing that
outlives an explicit command except this self-launched, self-terminating child.

**LOCKED — scope (owner):** auto-sync re-syncs **code only**. Adapters and
native sources (DB schemas, buckets, queues — anything that DIALS an external
system) are **NEVER auto-synced on a query** — they refresh only on an explicit
`add` / `up` / `capture`. Rationale: a read/query must never trigger a network
dial or credential use; code is local, cheap, and the whole benchmark loss.

## Open questions for the owner (discuss before code)

1. **Default**: `off` (my lean — determinism/no-surprise-work), or ship `lazy`
   on by default so the CodeGraph-parity behavior is the out-of-box story?
2. **lazy vs block as the headline**: "answer now, refresh for next time"
   (lazy) is the CodeGraph feel and never adds latency; `block` is always-fresh
   but adds the delta-sync time to the query. Ship both, default `lazy`?
3. ~~Background mechanism~~ **LOCKED**: detached child process ("separate
   channel"), PID/lockfile-guarded. No cron/daemon/watcher.
4. ~~Scope of Part A~~ **LOCKED**: code producer only; adapters/sources stay
   manual, never auto-synced on a read.
5. **Wiki**: incremental re-distill is deferred (no LLM in-binary). On sync,
   regenerate the deterministic wiki only for touched communities, or skip wiki
   on auto-sync entirely and only refresh it on explicit `add`? (Lean: skip on
   auto-sync; wiki is not on the query hot path.)

## Stress test (adversarial — where a naive build breaks)

**S1 — the INFERRED cross-file edge landmine (CRITICAL, must design for).**
ctx-optimize resolves calls "module-wide by unique name" (INFERRED edges). If
incremental sync just evicts-and-re-extracts the *changed* file, the graph can
come out DIFFERENT from a full re-gather: adding a second `Foo()` in file A
makes a call named `Foo` in *unchanged* file B ambiguous (should now drop);
renaming in A should re-point or drop B's INFERRED edge. Per-file incremental
alone silently corrupts cross-file edges and **kills determinism** — the one
thing we sell.
→ **Design fix (locked into Part A): two-phase extraction.**
  - Phase 1 (per-file, cacheable): AST → the file's own decl/file nodes +
    EXTRACTED edges (contains, imports). These ARE file-local — cache them per
    content-hash in `cache/ast/v{ver}/{filehash}.json`.
  - Phase 2 (global, always re-run): resolve INFERRED call edges over the FULL
    assembled node set. It's just name-matching over an in-memory map — cheap
    even on 12k files. On every sync, reuse Phase-1 for unchanged files,
    re-parse only changed ones, then **re-run Phase 2 wholesale.**
  → Guarantees `incremental graph == full-gather graph`, byte-for-byte. The
  speed win is skipping tree-sitter (the expensive part); correctness is never
  traded for it. **Golden gate must assert this equality.**

**S2 — mtime is a liar.** git clone/checkout can bump mtime on unchanged files
(→ over-hash, safe but slower) or preserve it; `touch` bumps mtime with no
content change (→ hash matches, no re-extract, correct). Content-hash is
identity; mtime is only a skip-the-read gate. Residual risk: a change within the
same second at the same size and no mtime bump — vanishingly rare; `block`/
explicit `add` may force a full hash for the correctness-critical path.

**S3 — deletes / renames orphan edges.** A deleted file must evict its nodes AND
any INFERRED edges pointing into them from elsewhere. Phase 2's wholesale
re-resolve handles inbound INFERRED automatically (targets vanish → edges drop);
EXTRACTED edges are file-local so evicting the file covers them. Rename = delete
+ add. Covered by S1's design, not a separate mechanism.

**S4 — torn reads during sync.** The detached child rewrites store ndjson while
a concurrent query reads it. Reuse the store's existing **atomic write-temp +
rename** discipline: a reader sees the whole old store or the whole new one,
never a torn file. The lockfile is an atomic `O_CREATE|O_EXCL` with PID+start
time; a dead-PID lock is reclaimed.

**S5 — lazy answers can be briefly wrong.** In `lazy`, query 1 answers from a
stale store — a real trust risk (the whole benchmark was about trustworthy
answers). Mitigation: the query annotates freshness ("store 1 commit behind —
syncing; re-run for updated") using the EXISTING `freshness` gate, so the agent
knows; `block` is the always-fresh option. We ship the honest tradeoff, not a
hidden one.

**S6 — detached child on Windows.** No fork; needs `DETACHED_PROCESS` /
`CREATE_NEW_PROCESS_GROUP` via per-GOOS `SysProcAttr`. Solvable but must be
tested on all three release platforms, not just darwin.

**S7 — monorepo.** Freshness is already per-module; the child syncs only the
module store(s) whose files changed. Root federation reads each independently.

## Beating CodeGraph, not just matching it

CodeGraph's 0.13–0.20s warm sync **blocks** the operation. Our design aims past
parity on three axes:

1. **Zero added query latency (lazy).** The async "separate channel" means the
   query NEVER waits for sync — answer latency is unaffected by staleness,
   period. A blocking sync (CodeGraph) can't offer that. Freshness catches up by
   the next query.
2. **A delta sync that should undercut 0.13s (block).** After Part A a 0-change
   re-sync is a stat-scan + no-op (~manifest read, plausibly <30ms); a 1-file
   change is parse-one-file + a cheap global re-resolve — the work is O(changed),
   not O(repo). CodeGraph's floor is its SQLite write path; ours is a rename.
3. **Provable determinism + zero infra.** S1's two-phase design lets us assert
   `incremental == full` in the golden net — a correctness claim CodeGraph
   doesn't publish — while keeping the single static binary, no SQLite, and the
   already-measured 3–10× smaller footprint. "Faster to re-sync, provably
   identical, and a tenth the disk, with nothing running" is a better story than
   "as fast as CodeGraph."

Thesis to prove with numbers before any of this ships on the site: on a 1-file
edit, `block` sync ≤ CodeGraph's warm; on 0 changes, ≤ CodeGraph; and `lazy`
adds 0ms to query. If the spike doesn't clear that bar, we ship the honest
"matches, doesn't beat" framing instead.

## Success check (when built)

- Warm re-sync on an unchanged tree is **O(0 changed) ≈ manifest-scan only**,
  and a 1-file change re-syncs in well under the cold time — benchmarked into
  the golden perf floor next to cold-gather, "may only move up".
- With `autosync: lazy`, editing a file then querying twice returns the fresh
  answer on the second query, with no manual `add`.
- `off` default path is byte-identical to today (no behavior change unless
  opted in). Determinism and "read never writes a store" both preserved.
- The comparison page's "no incremental fast-path" loss row can be honestly
  updated once the numbers land — not before.
