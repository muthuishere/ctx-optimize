# Community detection — cluster the graph into architecture neighborhoods

Status: **IMPLEMENTED** — wiki Subsystems + navigator `about` shipped as specified.
Amended 2026-07-25: clustering now filters AMBIGUOUS edges (see below).

## Context

Adoption shortlist from the competitive-wedge discussion (2026-07-14,
`openspec/changes/2026-07-14-competitive-wedge/`): graphify's wiki ships one
thing ours lacks — community detection ("this repo is 6 subsystems"). We
already have hubs (`internal/analyze.Hubs`); communities are the missing
architecture-level view. Constraints are the house rules: pure stdlib, no
model calls, deterministic output (the store must stay git-diffable), and the
chromium field store is real — 1.49M nodes / 3.7M edges must complete in
seconds, so anything super-linear (Girvan–Newman betweenness O(VE²),
spectral methods) is out.

## Decision — deterministic greedy modularity (Louvain-style), not plain label propagation

Candidates considered, with measured verdicts on our own store
(1,238 nodes / 1,689 edges):

- **Union-find over strong edges** — killed on paper: `contains`/`imports`
  connect the whole code graph, so it degenerates to connected components →
  1 giant community. Answers nothing.
- **Plain label propagation (LPA), deterministic variant** — implemented and
  measured: one 568-node mega-community ("everything that is Go code") plus
  73 disconnected doc islands. Hub files (app.go imports every package) let
  one label spread epidemically; tie-break tweaks (supporting-degree) fix
  small bridges but not the hub epidemic. Fails "useful granularity".
- **Greedy modularity local moving + coarsening (Louvain-style)** — CHOSEN.
  Same sorted-order visit loop as LPA, but a node joins the neighbor
  community with the best modularity gain (w_i→C − Σtot(C)·k_i / 2m); the
  Σtot penalty is exactly what stops hub-driven mega-merges. Measured on our
  store: the code graph splits into the real subsystems — internal/store,
  internal/app, internal/extract/code, internal/dashboard, internal/remote,
  internal/analyze, internal/wiki, internal/export… (45 communities total,
  30 shown on the index).

Determinism (both phases): node ids sorted ascending once; visits happen in
that fixed order with in-place updates; every reduce is order-independent
(max gain, tie → smallest community id); coarsening renumbers communities by
smallest member index; no randomness anywhere. Identical input — including
permuted input slices — yields byte-identical output (tested).

Complexity: O(rounds × E) per level (rounds capped at 20), levels shrink
geometrically (capped at 10). Measured: 50k nodes / 100k edges in ~27 ms →
1.5M/3.7M extrapolates to low single-digit seconds, inside the `add` budget.

Granularity: dust communities (< 5 members) merge into their strongest
neighbor; disconnected dust (an .md file with two headings, no links out) is
dropped, not reported as a fake subsystem. No forced merging down to a count
— measured, cap-driven merging is what recreates the mega-blob (connected
code communities cascade into one while unmergeable doc islands hold the
count up). The wiki caps the *display* at 30 rows instead ("… N more
subsystems"). Naming is derived, never invented: highest-degree non-module
member's label + dominant source directory ("store.go (internal/store)") —
non-module because stdlib import nodes ("os", "strings") out-degree
everything but describe nothing.

## Surfacing

- `internal/wiki`: new **Subsystems** section on `index.md` (one row per
  community: label, size, top-3 hub labels, dominant dirs; largest first,
  top 30 shown), regenerated on every `add` like the rest of the wiki;
  sorted and byte-stable.
- `internal/navigator`: `Build` already loads every module's nodes+edges for
  hub labels, so the top community label fills a module's `about` line when
  the module has no README summary — a README-derived summary still wins
  (human-authored beats derived). No extra graph loads.
- No new CLI verb in this change — the wiki and navigator are the consumers;
  a `communities` verb can follow if agents ask for it raw.

## Verification

- Unit: synthetic two-cluster-plus-bridge graph — stable membership and
  identical results across 3 runs and across reversed input order.
- Wiki: Subsystems section renders; two generations byte-identical.
- Performance guard: 50k nodes / 100k edges must complete well under a
  second (measured ~27 ms, logged by the test).


---

## Amendment 2026-07-25 — clustering must see facts only

`Communities` consumed every edge it was given. That was harmless until ADR
`2026-07-25-abstain-out-loud` started emitting AMBIGUOUS shortlist edges, at
which point clustering silently began building subsystems out of maybes.

Measured on this repo the moment that landed (8,495 edges, 1,092 of them
AMBIGUOUS):

| | with AMBIGUOUS | facts only |
|---|---|---|
| communities detected | 136 | 136 |
| **communities whose hub list repeats one label** | **7** | **0** |
| top-6 subsystems | different | different |

The 7 were the `Run, Run, Run` artifact — a "subsystem" that is really one
over-used name, which is precisely the god-node-by-name-collision failure
`docs/VISION.md:284` measured, arriving through clustering instead of through
`hubs`. The top six subsystems were reshuffled outright, so this was not
cosmetic: the wiki's architecture summary was being decided by guesses.

`Communities` now applies `WithoutAmbiguous` internally, like every other
traversal function. Pinned by `TestCommunitiesIgnoreAmbiguous` (two dense
clusters plus one AMBIGUOUS bridge edge: membership must not change).

Caught before release — the shortlisting change had not been pushed.

### Still not done

The dashboard does not colour its graph by community, which is graphify's most
visible feature (their hero image). The data exists; only the UI is missing.
Tracked separately rather than folded in here.
