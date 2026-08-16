# ADR 26 — the docs lens: the backbone is `contains`, not `references`

Status: SPIKE COMPLETE — design implication decided, build not started
Date: 2026-08-16

## The ask

> "so now we have a new ADR for next gen — look into it and do some spikes"

Following ADR 25, which drew doc links for the first time and then measured that
almost none of them appear in a repo-level picture.

## The problem, restated precisely

Retrieval already works. Measured on reqsume: `query "uptime monitoring"`
returns five doc sections with `file:line`, federating across module stores —
`docs/PRODUCTION-BLOCKERS-CHECKLIST.md L89-L93`, `apps/ui/adr/HLD.md L632-L638`.
Documents are found by filename and sections by heading text; body prose is
never indexed, which is stated policy, not a gap.

What has no docs in it is the PICTURE. The flow view ranks subsystems by
cross-directory edge weight, and reqsume has 45 cross-directory `references`
against roughly 30,000 code edges. No card slot will ever go to documentation,
however many slots there are — and that ranking is not wrong, it is honestly
reporting where the weight is. So the answer is a separate LENS, not a thumb on
the scale.

## THE SPIKE: 22 sections for every 1 link

`scripts/spikes/doc-lens.py`, over every store on this machine — reading the
STORE rather than the source, because the question is what a lens could draw
from what already exists:

```
TOTAL, 51 repos:  17,031 documents · 176,852 sections · 7,971 doc-to-doc links
                  3,591 linked · 13,440 ORPHANS (78%)
```

Per repo, the orphan rate is the finding:

```
repo                      docs   sects  links  orphan%  comps  largest  hub
deemwar-one-os            4667    7623    926      85%     20      589  wiki/02-verification-doctrine.md (26)
the-factory               3375   58338   2835      68%     70      200  docs/project/README.zh.md (35)
linux                     1696       2      0     100%      0        0
reqsume                   1200    5362    132      93%     13       40  infra/setup/AGENTS.md (5)
deepseek-harness-master   1013    5297   1878      20%    235      321  docs/subsystems/core.md (95)
mastra-main                568   47632     18      96%      5       10  DEVELOPMENT.md (3)
ctx-optimize               290    1436     31      95%      1       13  docs/remote-github.md (6)
video-ai                    75     996    187      26%      2       53  specs/gpu-burst.md (34)
```

**A lens built on links would be dust on the median repo.** reqsume: 93%
orphans. ctx-optimize, this repo: 95%. mastra: 96% — with 47,632 sections. A
force graph of `references` would draw a handful of islands surrounded by
thirteen thousand unconnected dots and call it a documentation map.

**The backbone that does exist is `contains`.** 176,852 sections against 7,971
links is 22:1. Every document has structure; almost none have neighbours.

## Decision

**The lens is a HIERARCHY — folder → document → section — with links as an
overlay.** Not a link graph. The thing to draw is the thing that is there.

- Nodes come from `contains`, which every doc has.
- `references` are drawn ON TOP, where they exist, in the ink ADR 25 gave them.
- The orphan rate is itself a fact worth printing. "1,200 documents, 82 linked"
  says something true and useful about a repo's documentation, and it is the
  number this spike exists to have measured rather than assumed.

**It adapts rather than assuming.** Four repos in the corpus have a real link
graph — deepseek (20% orphans, 1,878 links, one component of 321), video-ai
(26%), QuantDinger (21%), OpenCompany (38%, a component of 183). For those the
overlay is the story. The lens must not be tuned to either extreme; it shows
the hierarchy always and the links when there are links.

## Sub-findings worth keeping

- **linux: 1,696 documents, 2 sections, 0 links.** Not a defect — 1,695 of them
  are `.txt` files, correctly recorded as documents with no headings to become
  sections. It is the clearest example of why the orphan number must never be
  read as "bad docs".
- **mastra-main: 568 documents, 47,632 sections, 18 links.** Eighty-four
  sections per document and essentially no cross-references: deep structure,
  no web. The opposite failure mode from linux, and both break a link-first
  lens.
- **the-factory: 70 components, largest 200.** Even where links are plentiful
  the graph is islands, not one story. A lens that assumes a single connected
  map is wrong about the most link-rich repos too.

## Kill criterion

If the hierarchy lens cannot show a repo's documentation more usefully than
`query` already does — sections with `file:line`, federated across modules —
it does not ship. Retrieval is the bar, not an empty flow view: the lens has to
earn its place against a verb that already answers the question well.

And it must never draw a link the store did not extract. The doc graph is 78%
orphans; a lens that fills that space with inferred neighbours would be the
world view's failure with different nodes.
