# ADR 4 — doc links as edges: doc→code, anchors, and what measurement killed

Status: DRAFT — owner review pending 2026-08-14. No product code until agreed.
Scope: `internal/extract/markdown` only. No schema change, no new node kind,
no new producer. HTML is explicitly deferred (D5).

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

- Cost: two extra regexes and one `os.Stat`-class existence check per link on a
  file set already walked and line-scanned. Budget: **≤ +2%** on markdown
  extraction, measured on this repo and reqsume before/after.
- Gates: hermetic tests beside the code; `task ci`; `task golden` including the
  corpus tier — D3 is expected to move snapshot targets and every moved line is
  reviewed individually.
- Kill criterion: if D2+D3 together add fewer than 50 edges across the two
  measured repos **or** any dangling/dead edge survives D1, the slice is not
  worth its surface — ship D3 alone (the anchor half carries the volume) and
  drop D2.

## Open question for the owner

D2's honest volume is 10 resolvable links across two repos. It is the *right*
hop but a thin one. Worth landing on its own merits, or worth waiting until a
doc-heavy corpus (the pinned linux clone has thousands of `Documentation/*.rst`
— a different format entirely) justifies a broader doc-link slice?
