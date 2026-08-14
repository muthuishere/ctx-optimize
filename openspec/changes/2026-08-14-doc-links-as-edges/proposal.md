# ADR 4 — the markdown producer: fence correctness first, then doc links

Status: DRAFT — owner review pending 2026-08-14. No product code until agreed.
Scope: `internal/extract/markdown` only. No schema change, no new node kind,
no new producer. HTML is explicitly deferred (D5).

**Headline: this started as a link-coverage feature and measurement turned it
into a bug fix.** 9.4% of reqsume's section nodes are fabricated from inside
code fences (D0). The link work (D2–D4) is the smaller half and must not land
first.

## Context — what the markdown producer does today

`markdown.go:26` — `mdLinkRe = \]\(([^)#]+\.md)[^)]*\)` — **only targets ending
in `.md` become edges.** Everything else in a link is discarded silently.
Measured on a 4-link fixture: 1 of 4 survived. Store-wide, all 66 `references`
edges are doc→doc.

Link inventory across two real repos (`.md` only, vendored/testdata excluded):

| link class | ctx-optimize | reqsume | today |
|---|---|---|---|
| → `.md` | 60 | 170 | ✅ captured |
| → external `http(s)` | 20 | 121 | ❌ dropped |
| → anchor only (`#sec`) | 6 | 77 | ❌ dropped |
| → code file | 4 (**2** resolve) | 33 (**8** resolve) | ❌ dropped |
| → dir / other | 88 (**84 DEAD**) | 35 | ❌ dropped |

Two numbers decide this ADR, and both cut against the obvious design.

**The 84 dead links.** ctx-optimize's largest "other" class is absolute
`/private/tmp/claude-…/scratchpad/proof/linux/block/bio.c:1828` paths inside
generated benchmark transcripts under `proof/results/`. A widened regex that
emits an edge per link would add **84 dangling edges to another machine's temp
directory** on our own repo. Resolution is not a nicety here; it is the
precision gate (D1).

**The URL hosts.** The hypothesis worth testing was that doc URLs are the
*declared* leg ADR 1's `drift` was designed around but never received — giving
it "documented endpoint that no code calls". Host breakdown of all 141 external
URLs across both repos:

```
41 developer.chrome.com   40 github.com      7 img.shields.io
 6 web.dev                 6 vite-plugin-…   3 pkg.go.dev
 3 developers.google.com   2 taskfile.dev    2 www.postgresql.org
```

**Zero are service endpoints.** Not one `api.*` host; the population is
reference documentation, source links and CI badges — bibliography, not
architecture. reqsume's code calls `api.openai.com`, and no doc mentions it.

⇒ **The doc-URL-as-port idea is REJECTED on measurement**, before code. Docs
are not a declared-endpoint source in real repos, and feeding 141 bibliography
links into the boundary lane would have poisoned `drift` with noise dressed as
signal. Recorded here so it is not re-proposed.

## D0 — the fence bug: the producer parses code blocks as prose

`markdown.go` has **no fenced-code-block tracking** — no `` ``` `` state
anywhere in the file. Every line is matched against `headingRe`/`mdLinkRe`
regardless of whether it is inside a fence. Reproduced on a 3-heading fixture:

```
README.md::install-the-thing  "Install the thing"   ← a bash COMMENT in ```bash
README.md::example-doc-title  "Example Doc Title"   ← inside a ```markdown fence
README.md::real-heading       "Real Heading"        ← the only true heading
+ references edge README.md::example-doc-title -> docs/guide.md   ← from EXAMPLE code
```

Two of three section nodes are fabricated, and an edge was minted from sample
markdown inside a fence.

**Measured blast radius** — headings inside fences, across three corpora:

| corpus | real sections | phantom | share |
|---|---|---|---|
| ctx-optimize | 2151 | **67** | 3.0% |
| reqsume | 5315 | **551** | **9.4%** |
| linux `Documentation/` | 0 | 0 | n/a — `.rst`, not `.md` |

**Nearly one in ten section nodes in reqsume's store is fabricated from a code
block.** Those nodes are queryable, rankable, and citable today: a `query` can
return a phantom section with a real file:line, and a reader who follows the
citation lands inside an example. That is the store telling a confident lie,
which is the one failure mode this project exists to avoid.

Link damage is currently **0** in both repos — but only by luck of the narrow
`.md`-only regex: fenced examples rarely link to `.md` files. Widening link
extraction (D2–D4) without D0 converts a zero into a live false-positive
source, since fences are exactly where example links live.

This is a **correctness defect in shipped output**, not a missing feature — and it hits us hardest precisely where we document
ourselves: every `# comment` in every shell example in every README is a
candidate phantom section. It also means widening link extraction (D2–D4)
*without* fixing fences would multiply the fabrication, since fences are where
example links live.

Fixing this is mandatory and lands first.

## D0b — a byte scanner, not regex, and not tree-sitter

The obvious question is why markdown does not ride the tree-sitter/WASI lane
that `internal/extract/code` uses. Two reasons, one of them measured.

**Measured — regex is 4.4× slower than a hand scanner on our own corpus.**
Benchmarked over this repo's 316 markdown files (~3.5 MB), same work
(headings + link candidates), Go 1.22, `-benchtime 3x`:

| approach | throughput | per-pass |
|---|---|---|
| today's 3 regexes per line | 347 MB/s | 10.2 ms |
| plain byte scanner | **1535 MB/s** | **2.3 ms** |

Worth stating precisely, because "regex is slow" is not quite the finding: Go's
`regexp` is RE2, so it is *linear* — no catastrophic backtracking, which is
exactly why the `search` verb uses it and should keep using it. The cost here is
constant-factor, and in absolute terms markdown extraction is ~10 ms of a
multi-second gather. **Regex is not the bottleneck. It is simply beaten on both
axes** — the scanner is 4.4× faster *and* fence state is trivial to carry in a
scanner (one bool, toggled on ` ``` `) whereas a per-line regex fundamentally
cannot see block context. Correctness is the reason to switch; the speed is
change we get back.

**Tree-sitter is the wrong tool for this specific grammar.** `markdown` is not
in `internal/grammar/registry.go` at all, and adding it is not the usual pack
drop: upstream `tree-sitter-markdown` is a **split grammar** — block structure
and inline content (links, emphasis) are two separate parsers, and links live in
the *inline* one. Our lane embeds one grammar per language with one node-type
mapping, so markdown would be the first language needing two parsers chained.
That is real integration work plus WASM weight, to land somewhere between the
scanner and the current regex on speed. ⚠️ Verify the split-grammar shape at
build time before anyone acts on this paragraph — it is the current upstream
layout, not a measurement of ours.

⇒ Rewrite the producer's line loop as a byte scanner with fence state. Keep
regex where it earns its place (the `search` verb's RE2 sweep, boundary rules).

## D1 — resolve-or-drop (the precision gate)

A link becomes an edge **only if its target resolves to a path that exists in
the walk.** Unresolvable targets are dropped silently, exactly as D7's
`importresolve` drops external specifiers: a miss is honest, a guessed join is
not. This is what keeps the 84 dead scratchpad links out.

Resolution is relative to the linking file's directory (already how `mdLinkRe`
joins today), `.`/`..` normalized, fragment stripped before the existence check.
Absolute filesystem paths never resolve — by construction, they are not
repo-relative.

## D2 — doc → code `references` edges

Any link whose resolved target is a file in the graph gets
`section --references--> <file node>`, `EXTRACTED` (the path is a literal and it
exists on disk — there is nothing inferred about it).

Volume is small and the ADR should say so plainly: **2 links here, 8 in
reqsume.** The value is not volume, it is which links they are — reqsume's set
is specs pointing at their implementation
(`docs/specs/published/023-ui-route-lazy-loading.md → apps/ui/src/routes/AppRoutes.tsx`,
`apps/extension/docs/adr/002-…md → src/background/eventhandler.ts`). That is
the "where is this documented / what does this doc govern" hop, and it is the
one link class the store cannot answer at all today.

A `#L42` fragment is kept in edge metadata `anchor`; the edge still targets the
file node. Resolving an anchor to a *decl* node is deliberately out of scope —
line numbers drift and we would be asserting a join the file cannot confirm.

## D3 — anchors resolve to section nodes (changes existing output)

The producer already emits `section` nodes with deterministic slug ids
(`README.md::title`, dup-suffixed `-2`/`-3`). Anchored links should land on them:

- `[x](#title)` → `section --references--> <same-doc>::title`, EXTRACTED. New; 83 links across the two repos.
- `[x](docs/cli.md#usage)` → target `docs/cli.md::usage` **when that section id exists**, else fall back to the file node as today.

⚠️ **This is the one change to existing behavior** — a `.md#frag` link that
resolves to a real section moves its target from the file node to the section
node. That is strictly more precise, but it rewrites edges the golden snapshots
pin, so it lands under `task golden` with the diff reviewed, not auto-accepted.
If a snapshot shows a target moving to a section that does not exist, that is a
bug, not a floor to raise.

## D4 — images

`![alt](assets/logo.png)` → `references` with metadata `link:"image"`, same
resolve-or-drop rule. Cheap, correct, near-zero volume. **Not** `uses_image` —
that relation is owned by `dockercompose.go`/k8s for *container* images and
collapsing the two vocabularies would make `edges --relation uses_image` lie.

External URLs get **no edge and no node** (see Context). If a future repo class
proves otherwise, that is a new measurement, not a reopened decision.

## D5 — HTML: deferred, with the reason recorded

There is no `.html` producer. `html` exists in the grammar registry
(`internal/grammar/registry.go:116`) as a *buildable pack*, not embedded — and
a pack emits declaration nodes via node-type mapping, so `href`/`src` would
still not become edges without dedicated work.

Deferred because the dominant `.html` in real repos is **build output**. This
repo ships `internal/dashboard/ui/` as committed built assets; an HTML producer
would immediately index minified bundles and emit link edges from generated
markup — the vendored-corpus false positive of ADR 1, repeated. Any future HTML
slice must start from an exclusion rule for built output, and must be justified
by a repo class that actually keeps hand-authored HTML (docs sites, Rails/JSP
templates). Not this ADR.

## Perf, gates, kill criterion

- Cost: the scanner rewrite (D0b) is expected to make markdown extraction
  **faster in absolute terms** even with D1–D4 added — 4.4× headroom against
  one existence check per link, on a file set already walked. Budget: markdown
  extraction must not get slower than today's 10.2 ms/3.5 MB baseline.
- Gates: hermetic tests beside the code; `task ci`; `task golden` including the
  corpus tier. **Two separate expected snapshot movements, reviewed
  independently:** D0 REMOVES phantom sections/edges minted inside fences
  (node counts go DOWN and that is the fix landing — the golden floors move
  down with a recorded note, exactly as linux-block 8163→8162 did), and D3
  moves anchored link targets from file nodes to section nodes. A snapshot
  change that is neither of those is a bug.
- D0's before/after is the headline: 67 phantom sections here and 551 in
  reqsume must go to **zero**, with real-section counts unchanged (2151 /
  5315). A drop in REAL sections means the fence state machine over-consumed —
  the likeliest bug in this slice, so it gets a dedicated test with nested
  fences, `~~~` fences, indented fences and an unterminated fence at EOF.
- Kill criterion: if D2+D3 together add fewer than 50 edges across the two
  measured repos **or** any dangling/dead edge survives D1, the slice is not
  worth its surface — ship D3 alone (the anchor half carries the volume) and
  drop D2.

## Open question for the owner

D2's honest volume is 10 resolvable links across two repos. It is the *right*
hop but a thin one. Worth landing on its own merits, or worth waiting until a
doc-heavy corpus (the pinned linux clone has thousands of `Documentation/*.rst`
— a different format entirely) justifies a broader doc-link slice?
