# ADR 10 — doc citations: connect the 39% of the graph that is currently islands

Status: DRAFT — owner review pending 2026-08-15. No product code until agreed.
Scope: `internal/extract/markdown` only — one more edge kind from evidence
already sitting in the files. No schema change, no new node kind.
Follows ADR 4, which made the doc graph trustworthy; this makes it *reachable*.

## The gap, measured on this repo's own store

| | |
|---|---|
| section nodes | **1,556** |
| code nodes (function/method/class/type/interface/file) | 2,429 |
| sections with ANY edge other than `contains` | **37 — 2.4%** |
| **section ↔ code edges** | **0** |

Sections are **39% of the graph by node count and contribute nothing to
answering a question about code.** `internal/schema/schema.go` advertises
"cross-batch links are how code↔docs↔schema connect"; for docs↔code that
number is currently zero.

ADR 4 already established why the explicit-link route does not close this: real
repos contain almost no `[text](path/to/file.go)` markdown links — 2 resolvable
in this repo, 8 in reqsume. The links people actually write are **citations in
backticks**, and this codebase is built on them.

## The measurement that decides the design

Every backticked span in every `.md` here, matched against the store:

| form | hits | distinct | verdict |
|---|---|---|---|
| **path resolving to a real file node** | **448** | 42 | **use it** |
| symbol resolving to exactly one declaration | 2,379 | 186 | **reject** |
| symbol ambiguous (name declared >1×) | 750 | — | reject |
| matches nothing (flags, commands, prose) | 44,536 | — | ignore |

**The symbol join is a trap, and its own numbers say so.** The top "resolving"
symbols are `add` (500), `path` (188), `Error` (108), `declares` (76), `edges`
(72) — CLI verbs, a relation name, and ordinary English that collide with
declaration names. This independently reproduces the verdict already recorded
in `openspec/changes/2026-07-25-structured-formats/spike-p45.md`: *"DEFER the
raw-label join. It scores 0% on every real repo measured."* Recording the
confirmation here so it is never proposed a third time.

**The path join is different in kind, not degree.** `internal/app/app.go` in
backticks is not a name that might coincide with a file — it is a repo-relative
path that either resolves in the walk or does not. Top hits are exactly what a
reader would want: `internal/app/app.go` (48), `internal/extract/code/code.go`
(40), `internal/store/store.go` (32), `internal/app/multimodule.go` (28).

## D1 — backticked paths become `references` edges, EXTRACTED

Inside the AST walk ADR 4 already performs, a code-span (`ast.CodeSpan`) whose
text resolves — via the same resolve-or-drop gate as D1 of ADR 4, meaning it
must exist **in the walk**, not merely on disk — emits
`section --references--> <file node>`, `EXTRACTED`.

Same rules as ADR 4's links, deliberately: resolve-or-drop, no external URLs,
metadata `anchor` for a `:L42` suffix, and **no new node is ever minted** — a
citation to something outside the graph is silently dropped, not invented.

Expected effect: sections carrying at least one edge should rise from 2.4%
toward 15–20%, and `card`/`query` on a file gains "documented in" for the first
time.

## D2 — what stays rejected, and why it must be written down

- **Bare symbol names in backticks.** Measured above: dominated by `add`,
  `path`, `edges`. Even restricted to unique declarations it is a name
  coincidence, not a citation.
- **Prose mentions without backticks.** Strictly weaker than the above.
- **Fenced code blocks matched to symbols.** A fence shows illustrative code;
  attributing it to a declaration is a guess.

If a future measurement changes this, it is a new ADR with new numbers — not a
reinterpretation of these.

## D3 — the one form worth spiking separately

A backticked path with a line range (`internal/store/store.go:256`,
`code.go:1107-1120`) is the strongest citation in the corpus: it names a file
AND a location. This repo's own ADRs are full of them, and the `verify` verb
already exists to check exactly that shape. Worth measuring whether the range
should ride the edge as metadata (cheap) or whether `verify` should be able to
sweep every doc citation in a repo and report the stale ones (a feature — "which
docs now cite lines that moved"). Measure the count first; do not build the
sweep on a hunch.

## Gates

- Resolve-or-drop verified: zero dangling edges added (ADR 4's D1 lesson —
  the first cut of that shipped 10 danglers because it checked the disk instead
  of the walk).
- Judged scoreboard may not move DOWN. Note the risk honestly: adding edges to
  section nodes could pull doc sections UP in query ranking, which is the exact
  problem `docDemote` (`internal/query/query.go:325`) exists to counteract.
  If marks fall, the join is not paying for itself.
- `task ci`, hermetic + corpus + judged golden; markdown extraction stays inside
  its budget (currently ~24 ms per 3.5 MB, and the AST walk already visits code
  spans, so the added cost should be a map probe).
- Byte-identical output across two gathers.

## Kill criterion

If fewer than 5% of sections gain an edge on both this repo and reqsume, the
signal is too sparse to justify the surface — report the number and stop.
