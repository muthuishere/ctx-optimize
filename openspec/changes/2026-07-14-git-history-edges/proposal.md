# ADR — git-history edges: co-change coupling from `git log`

Status: DRAFT — 2026-07-14. GitNexus-lane adoption from the competitive
discussion (openspec/changes/2026-07-14-competitive-wedge/). Decisions marked
⚖️ are still open; everything else is decided.

## Context

Every edge in the graph today is derived from file CONTENT (tree-sitter,
markdown structure, adapters). But a repo's history knows couplings content
cannot express: impl↔test, code↔doc surface, schema↔migration — "when you
touch A you almost always touch B". GitNexus ships this as its headline
feature; deriving it needs nothing but `git log` (no AST, no LLM, fully
deterministic — inside our binary's constitution).

The premise had to be measured before building (house rule: every claim
traces to a spike).

## Measured evidence (STEP 0)

Spike: `git log --name-only --no-merges -n 500 --pretty=format:%H` on this
repo itself (102 non-merge commits; etcd clone unavailable, beam clone is
shallow). Pairs = files sharing a commit; commits touching >20 files skipped
(5 skipped — bulk scaffolds); min support 3. Result: 86 qualifying pairs.
Top of the list, with per-file confidence ratios (n_together / n_fileX):

| support | pair | reading |
|---|---|---|
| 12 | `internal/app/app.go` ↔ `internal/skills/bundled/ctx-optimize/SKILL.md` | CLI surface ↔ its agent-skill doc — **cross-boundary coupling no AST can see**; ratio 0.63: touching SKILL.md means app.go moved 63% of the time |
| 10 | `internal/app/app.go` ↔ `internal/app/app_test.go` | impl ↔ test, ratio 1.00 on the test side |
| 7 | `internal/project/project.go` ↔ `internal/project/project_test.go` | impl ↔ test, ratio 1.00 |
| 8 | `AGENTS.md` ↔ `CLAUDE.md` | synced doc copies, ratio 1.00 both ways |

Noise check: the remaining top-15 are README/CLAUDE/SKILL doc-surface
couplings and app.go↔store.go — all real. Zero junk pairs in the top 15.

**Verdict: the signal is real — build it.** The highest-value pairs are
exactly the ones content extraction is blind to (code↔doc), which is the
point of adding the lane.

## Decision

New tier-1 producer `internal/extract/githistory`, producer tag
`git-history`, **edges only** — it emits no nodes. Edges reference the file
node ids the code/markdown producers already emit (dir-relative slash
paths); cross-batch edges are the schema's design intent.

- Relation `co_changed_with`, confidence `INFERRED`, one edge per pair
  (endpoints ordered lexically, source < target).
- Edge metadata (strings, per schema): `synthesized_by=git-cochange`,
  `support=<commits together>`, `confidence_ratio=<support / n_source>`.
- Best-effort like `internal/extract/ignore`: no git binary, not a repo, or
  empty history → empty batch, never an error that blocks `add`.
- Wired into `gatherInto` (internal/app/multimodule.go) so every add — single,
  module, fan-out — refreshes it; producer-scoped `store.Replace` prunes
  stale pairs automatically (the shrink guard is node-scoped, so an
  edges-only producer never trips it).
- Module scoping: `git -C <moduleDir> log --relative --name-only -- .` —
  `--relative` both filters to the subtree and re-relativizes paths, so the
  emitted ids match the module store's node ids (verified against git's
  actual behavior on this repo). Fan-out `excludes` (child module dirs) are
  filtered out before pair counting.

### Thresholds (consts in githistory.go, pointing back here)

| const | value | why |
|---|---|---|
| `windowCommits` | 500 | recency window: old couplings decay out; bounded runtime |
| `maxCommitFiles` | 20 | commits touching more are bulk renames/scaffolds — they poison the signal (5/102 skipped in the spike, all scaffolds). Applied to the files VISIBLE after pathspec + exclude filtering |
| `minSupport` | 3 | below 3 shared commits, coincidence dominates (measured: support-2 pairs were mostly unrelated drive-bys) |
| `maxPairs` | 200 | cap per store; keeps the batch and query surface bounded on huge repos |

### Filters

- Both endpoints must still exist on disk at gather time — dead files leave
  the graph; edges to pruned nodes are noise.
- Secret-ish basenames are skipped (`secret`, `credential`, `password`,
  `.env*`) — same posture as the other producers, even though edges only
  carry paths.

## Deferred

- **Ownership (⚖️ privacy)**: per-file author/maintainer attribution from the
  same window was considered and deliberately NOT built in v1. It would
  require person nodes (or person-valued metadata) — that embeds names/emails
  from git history into a store that gets pushed to shared remotes, and the
  node-ownership semantics (who "owns" a file across producers) are
  undesigned. Revisit only with an explicit opt-in flag and an anonymization
  answer.
- Weighting query rank by co-change support (edges carry the data already).
- A second-pass bulk-commit heuristic that sees the UNFILTERED commit size
  in module scopes (today the cap applies to the subtree-visible file list).
