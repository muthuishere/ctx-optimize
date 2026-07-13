# Proposal — store freshness signal (store vs git HEAD)

## What

Give the store a **freshness signal**: at `add` time record the source repo's git HEAD;
at read time compare the recorded HEAD to the repo's *current* HEAD and tell the agent
whether the store is up to date or **stale** — so it knows when to re-`add` instead of
answering from a snapshot behind HEAD.

Two surfaces:
- `ctx-optimize status` gains a `fresh:` line and a `freshness{}` block in `--json`.
- a new thin verb `ctx-optimize fresh` — one line + **exit code** (`0` fresh, `1` stale,
  `2` unknown) so a hook / another organ can gate cheaply.

## Why

This organ is every other organ worker's **code-navigation organ**: an agent runs
`query→card→affected` *instead of* grep. That substitution is only safe if the store
reflects the code. The retrieval-freshness literature is blunt about the failure mode:

- **"When Retrieval Hurts Code Completion: A Diagnostic Study of Stale Repository Context"**
  (arXiv 2605.14478, 2026) — stale repository context actively *degrades* code answers;
  freshness is not cosmetic.
- **"The RAG Freshness Problem: stale embeddings silently wreck retrieval"** — production
  recall observed drifting 0.92→0.74 with no code change, only index age.
- Best practice across the incremental-indexing literature: **content-hash change
  detection + re-embed only changed files** (ctx-optimize already does content-hash
  invalidation via `manifest.json`, per `openspec/changes/2026-07-11-graphify-gaps/design.md`).
  The missing half is *surfacing* staleness to the consumer.

RepoGraph (arXiv 2410.14684, ICLR 2025) and RANGER (2509.25257) show a repo-level
def/ref graph boosts agent SWE performance +32.8% — but only a *current* graph. A silent
stale graph is worse than grep because the agent trusts it.

Measured gap (2026-07-13): the one-os store was built `Jul 12 21:47`; the repo HEAD moved
to a `Jul 13 00:47` commit; `status` reported nodes/edges but gave zero staleness signal.

## Scope (this change)

- `internal/freshness` — a **pure, stdlib-only** comparator (`Evaluate`) + report type.
- Store persistence: `add` writes `source.json` (per source root: abs path, head sha,
  head unix, added unix). New store methods `RecordSource` / `Sources`.
- A best-effort git reader in the CLI layer (`git -C <dir> rev-parse HEAD` + committer
  time). **Read-only, never fatal** — no git / not a repo ⇒ freshness `unknown`, not error.
- `status` surfaces freshness; new `fresh` verb with exit codes.

## Out of scope

- No auto re-add / watcher / daemon — we *signal*, the agent (or a hook) decides. Keeps
  the binary deterministic and side-effect-free on the read path.
- No commits-behind count (needs full git log walk). Boolean fresh/stale + both SHAs +
  age is the actionable signal.
- No change to the content-hash manifest or sync — freshness is source-provenance, a
  different axis from artifact-sync fingerprints.

## Non-negotiables preserved

- Read path never creates store dirs (dashboard rule); `fresh`/`status` only read.
- Deterministic binary: git call is read-only reflection, not intelligence. Absence is
  handled, not fatal. Secrets rule untouched (no env values printed).
- `task ci` green + a new test proving the CEO stale-detection case.

## Prior art / credit

Freshness framing from the RAG-staleness literature above; content-hash invalidation
already in-repo (graphify-gaps design). Git-HEAD-as-provenance is the standard
incremental-reindex trigger.
