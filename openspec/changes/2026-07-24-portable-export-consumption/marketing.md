# Marketing — "Ask your code graph. No jq. No Python. One binary."

Companion to `proposal.md`. How we position and launch native cross-module
filtering (`nodes`/`edges`/`deps` verbs, `export` filter flags, and filtered
`query`). Numbers are from `spikes.md` (real, measured) — every claim traces to
a spike, per the repo's evidence-first rule. Branding: **muthuishere**, no
deemwar links.

## The one-line

**Query your codebase's knowledge graph natively — the CLI answers, not `jq`.**

Sub: *One static binary. Every node and edge kind. Every module. Extremely fast,
everywhere — including a stock Windows or Alpine box with nothing else installed.*

## The wedge (why anyone cares)

Every "graph tool" hands you a JSON blob and a homework assignment: `| jq`,
`| python`, learn a query DSL, stand up a graph DB. That homework is the product
tax. It's slow, it's not portable, and on a fresh CI runner the tool you need
isn't even there.

ctx-optimize deletes the homework. The graph is already parsed fact on disk —
you ask, it answers. That's the whole pitch, and now it's literally true for
the questions people actually ask: *"list the prod k8s services", "which files
use react", "dev-scope deps and who imports them", "the auth handler, ranked."*

## The proof (lead with the numbers — they're the feature)

On a real 160,836-node / 220,566-edge store (mastra, federated), emitting every
`imports` edge:

| path | wall time | peak memory | on a stock Windows/Alpine box? |
|---|---|---|---|
| **ctx-optimize (native)** | **176 ms** | **11.9 MB** | **yes** |
| `export \| jq` | 730 ms | 561 MB | **no — jq absent** |
| `export \| python` | 670 ms | 771 MB | often no |

**~4× faster. 47–64× less memory. The only one that runs everywhere.** And the
gap *grows* with store size — native streams the ndjson record-by-record (O(1)
memory); the pipe must reassemble and re-parse the whole document every time.

> The headline everyone remembers: **"jq needs half a gig of RAM and a second.
> We answer in 12 MB and 176 ms — with no jq installed."**

## Positioning vs. the field

- **vs. graph databases (Neo4j/FalkorDB):** no server, no import step, no query
  language to learn. A static binary and a folder of plain files.
- **vs. `graph.json + jq`:** we don't emit a blob and wish you luck. Native
  verbs, portable, an order of magnitude leaner. `--jq` (gojq, built in) stays
  for muscle memory — but it's the convenience, not the fast path.
- **vs. "just grep":** grep finds strings; this answers *structure* — kinds,
  relations, blast radius, ranked relevance — across every module at once.

## Messages by audience

- **The agent/CLI power user:** "Stop teaching your agent to shell out to jq.
  One verb, cited file:line facts, works in every sandbox."
- **The CI/platform engineer:** "No runtime deps. No `apt install jq`. One
  binary answers on Alpine, on Windows, offline."
- **The monorepo team:** "Ask the *whole* repo — every module federated at the
  root in one pass — not one package at a time."

## Feature beats to show (demo order)

1. `ctx-optimize nodes --kind service --where namespace=prod` → readable table,
   instantly. (The k8s ask.)
2. `ctx-optimize deps --scope dev --importers` → dependency answer, no flag soup.
3. `ctx-optimize query "auth handler" --kind decl` → **ranked search, filtered**
   — the combination nothing else gives: most-relevant *of this kind*.
4. Same three commands on a fresh Alpine container with nothing installed —
   they just run. That's the mic-drop.

## Channels & assets

- **README consumption section**: rewrite every example to use zero external
  binaries (this is also a `proposal.md` success check — marketing and product
  ship together).
- **Launch post (X / HN / LinkedIn via muthuishere):** the two-command Alpine
  GIF + the memory/speed table. Title candidate: *"Your codebase graph, queried
  in 176ms with no jq — one static binary."*
- **Short asciinema/GIF:** the four demo beats above, ~20s, autoplaying in the
  README.
- **Issue #5 follow-up note:** the exact snippets that used to pipe to jq, now
  one native command — closes the loop that triggered the work.

## Guardrails (don't overclaim)

- Cite the corpus and machine with every number; never a bare "4× faster."
- `--jq` exists — say so; positioning is "native is the fast path," not "jq is
  gone."
- The speed floor is CI-pinned (golden gate) — so the claim stays true release
  over release. *That* is marketable: "the benchmark can only move up."

## Success signals

- README/issue examples use no external binary (matches proposal success check).
- A first-run user answers a k8s / deps / ranked-filter question in one command,
  no docs detour.
- The speed table shows up in third-party posts unprompted — the number did the
  selling.
