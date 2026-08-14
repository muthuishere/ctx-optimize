# ADR 4 — the markdown producer: fence correctness first, then doc links

Status: ACCEPTED 2026-08-14 (owner: "happy to send ast goldmark and all stuff
… if there is something better thats okay as well"). Alternative checked on the
same fixture: `gomarkdown/markdown` parses it equally correctly with zero
transitive deps, but goldmark keeps the decision on two grounds — full
CommonMark spec compliance with a conformance suite, and clean source positions
(`Lines().At(0).Start`), which our `section` nodes need for `L#-L#` and
gomarkdown does not expose as reliably.
Scope: `internal/extract/markdown` only. No schema change, no new node kind,
no new producer. HTML is explicitly deferred (D5).

**Headline: this started as a link-coverage feature and measurement turned it
into a bug fix, then into a parser swap.** 9.4% of reqsume's section nodes are
fabricated from inside code fences (D0); the fix is to stop hand-rolling a
markdown parser and adopt goldmark (D0b). The link work (D2–D4) is the smaller
half and must not land first.

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

This is a **correctness defect in shipped output**, not a missing feature, and
it hits hardest exactly where a project documents itself: every `# comment` in
every shell example in every README is a candidate phantom section.

Fixing this is mandatory and lands first.

## D0b — parse with goldmark: a real CommonMark AST

An earlier draft of this ADR proposed a hand-written byte scanner with a fence
bool. **That was wrong, and the measurement says so.** Recorded here with the
evidence, because the reasoning generalizes: a scanner fixes the bug you just
found, a parser fixes the class.

**Why the scanner loses.** It was chosen on speed. Benchmarked over this repo's
316 markdown files (~3.5 MB), same extraction work, `-benchtime 3x`:

| approach | throughput | per-pass | correctness |
|---|---|---|---|
| today's 3 regexes per line | 347 MB/s | 10.2 ms | fences ✗ setext ✗ ref-links ✗ |
| hand byte scanner | 1489 MB/s | 2.4 ms | fences ✓ setext ✗ ref-links ✗ |
| **goldmark AST walk** | 147 MB/s | **24.1 ms** | **all ✓** |

goldmark is the slowest — 2.4× slower than today's regex — and it does not
matter. The absolute number is **24 ms for 3.5 MB of markdown**, against
multi-second gathers; the scanner's win is 8 ms of a run that takes seconds.
Buying a permanent correctness class for 14 ms is not a trade, it is a gift.
(The RE2 note still holds and still matters elsewhere: Go's `regexp` is linear,
no catastrophic backtracking, which is why the `search` verb keeps it.)

**Why the scanner loses on the axis that counts.** The hand-rolled surface does
not end at fences. Measured across both repos:

| construct | today | scanner-with-fence-bool | goldmark |
|---|---|---|---|
| fenced phantoms (618) | ✗ | ✓ | ✓ |
| indented-code phantoms (3) | ✗ | ✗ | ✓ |
| setext headings (16, false NEGATIVES) | ✗ | ✗ | ✓ |
| reference links `[t][r]` (21) | ✗ | ✗ | ✓ |
| link definitions `[r]: dest` (15) | ✗ | ✗ | ✓ |
| autolinks `<https://…>` | ✗ | ✗ | ✓ |

Verified on one fixture containing every row at once: goldmark skipped all three
phantom headings (```bash fence, ```markdown fence, 4-space indented block),
caught the setext heading, and **resolved `[ref link][r]` through its
`[r]: docs/cli.md` definition elsewhere in the file** — a scanner would need its
own definition table, which is the point at which we are writing a markdown
parser badly instead of importing one written well.

The long tail is honestly small *on our two repos* — ~40 facts against 618 from
fences alone. But **this tool runs on other people's repositories**, and our
corpus is not representative of theirs: reference-style links and setext
headings are ordinary in doc cultures we do not write in. The scanner's bug list
is the list we happen to have hit; the parser's is bounded by a spec with a
conformance suite.

**Cost, measured, against this repo's dependency doctrine.** The main binary
today pulls exactly three modules — `wazero`, `xz`, `x/sys`; every DB driver
lives in the `ctx-optimize-adapters` companion. goldmark would be the fourth:

- **+1.19 MB** on a 46.66 MB binary (**+2.5%**), measured by building with and without
- **zero transitive dependencies** (`go list -m all` = the module and nothing else), pure Go, no cgo
- **zero init cost** — a parser, not a client; this is precisely what separates it from `minio-go`, which was banned for a 15 ms init, not for existing

**Tree-sitter is the wrong tool for this one grammar.** `markdown` is not in
`internal/grammar/registry.go`, and it is not a normal pack drop: upstream
`tree-sitter-markdown` is a **split grammar** — block structure and inline
content are separate parsers and links live in the *inline* one, so markdown
would be the first language in our lane needing two chained parsers, for more
WASM weight than goldmark's 1.19 MB. ⚠️ Confirm that split at build time before
anyone acts on this paragraph; it is upstream's current layout, not our
measurement.

⇒ **Replace the line loop with a goldmark AST walk.** Headings come from
`ast.Heading` with `Lines().At(0).Start` mapped to a line number by counting
newlines in the prefix (verified working). Links/images/autolinks come from
`ast.Link`/`ast.Image`/`ast.AutoLink` with destinations already resolved.

**One thing goldmark does not give us: `[[wikilinks]]` are not CommonMark.**
Keep that regex, but apply it to AST *text* nodes rather than raw lines — which
is strictly better than today, since text nodes exclude fenced and indented code
by construction, fixing wikilinks' share of the fence bug for free.

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

- Cost: goldmark makes markdown extraction ~2.4× slower in relative terms and
  **+14 ms in absolute terms** on 3.5 MB (10.2 → 24.1 ms), plus one existence
  check per link. Budget: markdown extraction stays **under 50 ms per 3.5 MB**
  and total gather time must not move measurably on this repo or reqsume. If a
  doc-heavy corpus ever makes this visible, the fix is caching by content hash
  (the manifest already exists), not a hand-rolled parser.
- Dependency: goldmark becomes the main binary's 4th module (+1.19 MB, +2.5%,
  zero transitive deps, zero init cost). `task ci` must confirm the adapters
  companion is unaffected and that no DB driver leaks into main.
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
