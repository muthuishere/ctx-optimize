# ADR — wiki at scale: keep the per-file wiki, make RESYNC skip it

Status: **ACCEPTED** — 2026-07-24 (owner-directed this session). Supersedes the
option menu in `proposal.md`; `spikes.md` has the measurements. Deterministic,
**no LLM** (per binary doctrine).

## The decision, up front

1. **Do NOT restructure the wiki.** Keep today's **one page per file/decl** layout.
   People rely on it (browsable per-entity pages; `serve` dashboard; obsidian).
   The "community-level default, per-file opt-in" idea (graphify's model) is
   **rejected** — it changes a thing people like, to fix a cost that isn't on the
   hot path.
2. **The only change: RESYNC skips the wiki.** The lazy/block background resync
   (lever 3) refreshes the **graph only**; the wiki is regenerated on an explicit
   `add` / `sync --wiki`, not on a background/auto resync. An optional `--no-wiki`
   flag lets an explicit run skip it too (for huge repos).
3. Everything else stays exactly as it is.

## Why this is the right scope — the load-bearing fact

**The wiki is NOT on the query path.** Verified in code:
- `cmdQuery` → `query.Run(nodes, edges, …)` (`internal/app/app.go`), reading
  `graph/nodes.ndjson` + `graph/edges.ndjson`. `internal/query/` has **zero**
  references to `wiki`.
- `card` / `change-plan` / `affected` / `path` all run over the graph via
  `internal/analyze`. None open a `wiki/*.md` page.

Consequences:
- **Query speed and answers do not depend on the wiki at all.** Query is fast
  because of the graph + lexical index (IDF/prefix/trigram), not because pages
  are per-file. Rendering the wiki differently (or not at all) cannot change what
  a query returns or how fast it returns.
- A background resync only needs the **graph** fresh for queries to be current.
  Regenerating 60,388 wiki pages on every edit (Linux: ~1,462 s) is pure waste on
  that path. Skipping it is free correctness-wise.

## Does this hurt "answer everything about project / jira / openapi / db / schema"?

**No.** Each of those is ingested as **nodes + edges** in the graph — a Jira
ticket, a DB table, an OpenAPI endpoint, a schema column are each a node (via
adapters / native sources). Query answers about them by searching the **graph**,
independent of the wiki. Whether the wiki renders one page per table or none at
all does not change what ctx-optimize can *answer* — only what a human can
*browse*. So the full-coverage goal is a **graph/ingestion** property, untouched
by this decision.

## Measured (spikes.md)

| repo | wiki pages | full gather | wiki-only regen | wiki share |
|---|---|---|---|---|
| ctx-optimize (~3.5k nodes) | 487 | 0.81 s | 0.09 s | ~11% |
| Linux (2.85M nodes, dense) | 60,388 | 1,481 s | ~1,462 s | ~98% |

Wiki cost scales with node count × degree: negligible on a normal repo,
dominant on Linux. It is a **regeneration** cost, entirely off the query path.
⇒ Making it explicit/skippable on resync fixes the only place it hurts, with no
loss anywhere queries or coverage live.

## What changes in code (small, for owner sign-off before building)

- **Autosync (lever 3) resync = graph-only.** Thread a `skipWiki` through
  `gatherInto`; the `__autosync` child and the block/inline resync set it. (The
  autosync ADR's open-Q4 is hereby answered: **skip wiki on auto-sync**.)
- **Optional `--no-wiki` on `add`/`sync`** for an explicit graph-only refresh
  (huge-repo escape hatch). Default explicit behavior is UNCHANGED (still
  regenerates the wiki when the graph changed).
- **No wiki layout change. No config knob required. No LLM. No threshold.**

## Explicitly rejected (and why)

- **Community-level default wiki (graphify style):** changes a liked artifact;
  the cost it solves isn't on the query path; unnecessary once resync skips wiki.
- **Page-size cap / opt-out threshold:** solving a non-hot-path cost with a
  behavior change to explicit runs. Deferred — revisit only if explicit `add` on
  huge repos proves painful even when people opt into the wiki.
- **Incremental deterministic wiki (old M2):** real but unneeded — the wiki is
  off the query path and now off the resync path; a browse artifact refreshed on
  demand doesn't need per-page incremental machinery.

## Success check

- After an edit, `autosync` (lazy/block) refreshes the **graph** in seconds at any
  scale — Linux resync no longer pays the ~24-min wiki tax — and queries are
  current immediately (they read the graph).
- Explicit `add` / `sync` still produce the full per-file wiki (unchanged);
  `--no-wiki` skips it when asked.
- `query`/`card`/`affected` answers and latency are byte-identical regardless of
  wiki state (a golden guard can assert query output is independent of `wiki/`).
