# ADR 25 — the document graph, and where it actually is

Status: SPIKE COMPLETE — D0 shipped, the rest awaits a decision the numbers changed
Date: 2026-08-16

## The ask

> "the links within — that's what the markdown parser we shipped does, right?
> and is this shown anywhere? I think these document links should also be shown
> across … I feel all documents in a monorepo itself as a separate node, do some
> spike how we can represent in graph"

## What already worked, and what did not

Measured, not assumed. The markdown lane extracts `[text](target)` and
reference-style links as `references` edges, and it attaches each one to the
nearest containing node — a section when the file has headings, **the document
itself when it does not**:

```
docs/no-headings.md            -references-> docs/adr-0001.md
docs/no-headings.md            -references-> docs/runbook.md
docs/with-headings.md::design  -references-> docs/adr-0001.md
```

So a headingless markdown file is not dead weight: it contributes a `document`
node findable by filename AND an edge per link. Bare URLs in prose are not
extracted (only `[text](url)` and reference definitions), which is why the six
headingless ADRs in reqsume emit nothing — they contain no markdown links, only
a URL sitting in a sentence.

Two real gaps behind the ask:

**Gap 1 — nothing DREW them.** `card` prints `references`, `edges --relation
references` lists them, and `liftedRelations` in the scene was `imports` and
`calls` only. An ADR half the repo points at looked like an orphan in every
picture. **Shipped**: `references` is lifted and drawn, with its own ink (a
dependency written by a human, not the compiler) and its own line in the key.

**Gap 2 — a link that escapes its module is silently dropped.** On a two-module
fixture, `[UI notes](../../ui/docs/notes.md)` produced NO edge, while both
documents existed as nodes in their own stores. Not dangling, not AMBIGUOUS —
gone. The same-module control `[other](./other.md)` produced its edge. This is
the doc analogue of ADR 22's cross-module problem, and it is *easier*: a code
symbol has no global identity, but a doc link literally writes the path.

## THE SPIKE: module-to-module doc links are RARE. Root-to-module is where the graph is.

`scripts/spikes/doc-graph.py`, over every multi-module repo in the local store,
reading the SOURCE (the interesting links are exactly the ones the store does
not contain, so reading the store would measure the gap as zero by
construction):

```
TOTAL links by kind, 17 multi-module repos, 8,711 markdown files

  intra       3,485   resolves inside its own module — already an edge today
  cross         630   module → SIBLING module — dropped today
  root_into   5,629   a doc OUTSIDE every module → into one — dropped today
  outside     5,826   both ends outside every declared module
  external   10,850   http(s) — a URL, not a document relationship
  anchor      1,714   #section — already covered by `contains`
  dangling    9,195   resolves to nothing on disk (SEE THE CAVEAT)
```

**This inverts the ask, the same way the HTTP spike inverted ADR 22.** I
expected cross-module doc links to be the prize. They are 630, and **615 of
those are one repo** (deepseek-harness-master, 232 modules). reqsume: 0.
volentis: 1. tabby-master: 0. the-factory, at 228 modules and 3,385 docs: 6.

What is actually there is **root_into + outside = 11,455** — links involving a
document that sits outside every declared module. The repo's own README, its
`docs/`, its ADRs, its THIRD_PARTY_NOTICES pointing *into* the packages. The
document graph of a monorepo is not modules talking to each other; it is the
repo's own documentation pointing at its parts.

That lands on something already built: ADR 22 D4 had to add the residual root
store as a card because it held 90% of AI-company-master. The same store is
where the document graph lives.

Three honest caveats:

- **One repo dominates.** Strip deepseek and `cross` falls to 15 across 16
  repos. Any design tuned to 615 links is tuned to one repository.
- **`dangling` is not trustworthy and must not be quoted.** Sampling
  the-factory's 6,537 found vendored `references/picoclaw-main/...` trees whose
  internal links are genuinely broken, plus prose that merely looks like a
  link: `[...](...)` in a changelog, `[x](/abs/path/app.py:12)` inside a prompt
  fixture. It is a mix of real broken links and classifier noise, and
  separating them is its own piece of work.
- **The first run of this spike was wrong by 10x.** It counted a root doc
  linking into a module as "cross", reporting 6,259 module-to-module links.
  `module_of()` returns None for a repo-root doc, and `None != 'packages/x'`
  read as two different modules. Caught by looking at the top pairs —
  `None -> packages/...` — rather than at the total.

## What the numbers imply for representation

Not decided here; recorded so the decision is made against the measurement.

- A repo-level document graph is worth having, but its NODES are mostly the
  residual root store's docs, not each module's.
- `cross` at 630 does not justify a new cross-store identity scheme on its own.
  `root_into` at 5,629 might, and it is the cheaper case: one end is already in
  the root store the repo scene now draws.
- Both need a decision this ADR does not take: what a doc node's id is when the
  link crosses a store. Every current id is module-relative, and inventing a
  repo-relative one for documents alone would give the store two id schemes.

## Kill criterion

If a design for cross-store doc links cannot be expressed without a second id
scheme, it does not ship: two ways to name a node is a worse defect than a
missing edge, and the missing edge is currently worth 630 links outside one
outlier repo.
