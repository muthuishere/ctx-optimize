# ADR — `overview` verb: project orientation from the graph (wiki follow-up)

Status: DRAFT — 2026-07-24. For DISCUSSION before any code (repo change-flow).
Follow-up to `2026-07-24-wiki-scale/` (accepted). Deterministic, **no LLM**.

## Findings this rides on (all verified 2026-07-24)

1. **Nothing in the binary reads the wiki.** Every reference audited: `query`/
   `card`/`affected`/`change-plan` read the graph; the dashboard **skips** the
   wiki dir (`dashboard.go:397`); `multimodule.go` only stats/skips it; the
   store/navigator only write it. Removing `wiki/` entirely leaves query output
   and latency identical (measured: 0.00–0.02 s on the 107k-node java-spring
   store, with and without the wiki present).
2. **The graph already holds every facet an orientation needs** — java-spring
   store: 76k methods, 13.5k classes, 8.3k files, 6.3k modules (package deps),
   808 config_keys (property files), 108 configs, 39 DB tables, docs/sections.
   All enumerable today: `nodes --kind table`, `deps`, `hubs`, `edges
   --relation R`, `manifests`, `routes`, `status`.
3. **The one gap: no single orientation verb.** `overview`/`summary` are
   unknown commands. An agent (or an LLM wanting "the wiki") must assemble
   hubs + nodes-by-kind + status itself, or read `wiki/index.md` off disk —
   the only remaining job the wiki actually performs.

## Proposal

Add **`ctx-optimize overview [--json]`** (alias `summary`): one deterministic,
graph-only digest printed on demand — the wiki `index.md` content as a verb:

- counts by kind (methods/classes/files/tables/config_keys/…)
- top hubs (god nodes) with in/out degree
- community/subsystem breakdown (existing `analyze.Communities`), each with its
  top members
- key inventories: DB tables, routes, config files, top-level deps
- modules + freshness line (what `status` knows)
- budgeted like `query` (default fits an agent context; `--budget N`)

Federates at a multi-module root like the other read verbs. No store writes.

## Why

- Gives an LLM the orientation it would want a "wiki" for, from the graph, at
  query time — no stored pages, no regen cost, always current.
- Makes the per-file wiki fully optional: its last real value (orientation)
  moves into a verb everything already reads. A later `wiki: on|off|auto`
  default discussion (deferred with the owner's "go back to wiki later")
  becomes zero-risk.
- One command for the agent surfaces ("Orient — where do I start") instead of
  a hub/nodes/status assembly recipe.

## Open questions for the owner

1. Verb name: `overview` (lean) vs `summary` vs both as aliases?
2. Should the hook/instructions card replace the `hubs` orientation row with
   `overview` once it exists?
3. Wiki default after this lands: keep generating on explicit add (today) or
   flip to opt-in (`wiki: on`)? (Deferred — "we will go back to wiki".)

## Success check

- `overview` on the java-spring store prints the digest in < 0.5 s, byte-stable
  across runs, and answers "what is this project / what's in it / where do I
  start" without any wiki on disk.
- Golden: snapshot the overview of a pinned corpus; judged questions about
  project orientation score no worse than with the wiki present.
