# ADR — `#` is a comment far more often than it is a heading

Status: **IMPLEMENTED** — 2026-07-26. Owner chose the simple rule over the
measured threshold rule, and elevated it to a **core promise**. Supersedes the
first "drop `.txt`" framing in issue #14, whose *reasoning* was wrong even though
its conclusion was close.

## The proposal that was wrong, and why

Onboarding chromium surfaced `third_party/hunspell_dictionaries/README_*.txt` —
shell-comment licence headers where every line starts with `#`, so the doc
producer read each line as an H1 and emitted mid-sentence prose as `section`
nodes. The obvious fix, and the one first proposed: **treat `.txt` as plain
text**, on the reasoning that markdown headings are a `.md` convention and a
`.txt` is by definition plain text — a category error, not a quality trade-off.

An adversarial review and an impact analysis both rejected it. Three reasons:

1. **The category claim is false.** `llms.txt` (llmstxt.org) is markdown *by
   definition* — H1 title, `##` sections, markdown links — deliberately named
   `.txt`. It is spreading through repo roots. Not in this repo today, so nothing
   regresses now, but the premise does not hold.
2. **A blanket drop deletes real content.** Measured across 22 repos: **341
   genuine `.txt` headings** in 5 of them — `opencode` stores its LLM system
   prompts as `.txt` (121 sections: `Tone and style`, `Task Management`,
   `Workflow`), and one repo holds a **book manuscript** written in `.txt` (111
   sections). Dropping section parsing makes those files unreachable by content:
   the surviving `document` node's only lexical handle is its filename.
3. **The supporting measurement was wrong.** The claim "`.txt` produces zero
   sections on the corpora" was made from the two *repo* corpora only.
   `internal/golden/testdata/golden/pydeps.txt:30-33,96-100` pins **four `.txt`
   section nodes** from pip-compile comment headers. The first proposal
   contradicted a pinned golden its author had not checked — and inverted
   `internal/extract/markdown/crlf_test.go:56`, a test written the same day,
   which asserts that exact hunspell line "must still be extracted".

## Measured (30,289 real `.txt` files, 22 repos)

| corpus | `.txt` files | `section` nodes | from comment-char files | genuine headings |
|---|---:|---:|---:|---:|
| chromium | 28,105 | 3,687 | 3,538 (96.0%) | ~1 |
| linux v6.9 | 1,695 | 541 | 518 (95.7%) | **0** |
| Newtonsoft.Json | 3 | 0 | 0 | 0 |
| 18 other repos | 486 | 2,674 | 2,333 (87.2%) | ~340 |
| **total** | **30,289** | **6,902** | **6,389** | **~341 (4.9%)** |

**95.1% of every `.txt`-derived section node is a comment line or a prose
fragment.** Linux's 1,695 `.txt` files yield **zero** genuine headings.

And it is not cosmetic — ranking does **not** bury them:

| store | top-10 slots taken by `.txt` sections | worst |
|---|---:|---|
| ghostty-agent (junk) | **47/180 = 26.1%** | `case folding` 9/10 |
| vscode-agent (junk) | **18/60 = 30.0%** | `apple sdks` 9/10 |
| opencode (**correct answers**) | 14/60 = 23.3% | — |

`query "install prefix"` in ghostty returns `cmake --install build --prefix
/usr/local [section] CMakeLists.txt L16-L17` at **rank 1**. ghostty-agent's 35
`.txt` files produce **16.1% of that store's entire node count**.

The mechanism is symmetric: whatever `.txt` section parsing yields takes ~25–30%
of top-k on content queries. Junk in chromium/linux/ghostty/vscode; **the right
answer** in opencode. So the axis to split on is not the extension.

## Decision (as shipped): a `.txt` yields the document node and nothing else

**Owner's call, and it overrules the recommendation below.** The threshold rule
needed two invented numbers (4 consecutive lines, 20% density), and this project
had already refused a wiki page cap that same day for exactly that reason — a
number nobody can justify. 95% junk means the extraction is not worth having, not
that it needs tuning. "If it's not value, don't do it, instead of all bad."

Shipped:
- `.md` → document node + heading sections + link edges, unchanged.
- `.txt` → **document node only** (`internal/extract/markdown/markdown.go`,
  `extractFile(..., markdown bool)` with one early return).
- `internal/navigator/navigator.go` — the SECOND copy of the same
  `#`-is-a-heading rule, applied to `README.txt` — now treats `#` as a heading
  only in a `.md`. Two subsystems must not disagree about the same file.
- `internal/extract/markdown/crlf_test.go` — the assertion that
  `# LICENCE / TRWYDDED` is "a real CRLF heading" is **inverted**, with the
  reversal explained in the test. The CR-stripping and empty-heading cases move
  to a `.md` fixture, where headings actually live.
- `internal/golden/testdata/golden/pydeps.txt` — 4 section nodes + 4 `contains`
  edges removed (60→56 nodes, 55→51 edges). Every removed node is a pip-compile
  comment line. `requirements.txt | document` and its three `declares` edges
  survive, which was the load-bearing requirement.

Verified: corpus counts unchanged (linux 8163/12258, Newtonsoft 10120/24130 —
consistent with `.txt` contributing zero sections there), judged tiers unmoved
(16.5 / 13.0), `task ci` green.

### Elevated to a core promise

Documented in `docs/cli.md` ("The core promise: we do not invent structure that
isn't there") and on the agent surface in `SKILL.md`, because every abstention in
the tool is the same rule wearing different clothes — ambiguous callees,
unresolved receivers, fuzzy ties, redaction, partial gathers, and now `#` in a
`.txt`. **If it cannot be parsed honestly, it is not indexed, and you are told to
grep.** `TestCorePromiseIsOnTheAgentSurface` pins it, including the requirement
that the doc carry the measurement — an unmeasured promise is a slogan.

## Rejected: ask whether `#` is a comment character in THIS FILE

*(kept as the record of what was designed and why it lost)*

**Ask whether `#` is a comment character in THIS FILE**, and apply it to `.md`
as well as `.txt` — a `.md` file with the same shape leaks identically today, and
an extension-based rule misses it by construction.

The test is structural, not a content guess: **≥4 consecutive `#`-prefixed lines,
or >20% of non-blank lines starting with `#`.** In hunspell that is 100% of
lines; in `requirements-lock.txt` lines 1–6; in a real `.md` or `llms.txt` it
never happens, because prose separates headings.

Validated on all 30,289 files: **6,389 junk sections caught, zero false positives
on the authored side.** Its 172 "markdown-ish" outliers in chromium+linux were
hand-read individually and are junk too — so it **under-catches rather than
over-catches**, which is the only acceptable error direction here.

**The per-file `document` node is kept unconditionally.** This is not a
preference: `internal/extract/manifests/manifests.go:104` emits `declares` edges
with `Source: rel` (`requirements.txt`) and emits **no node for the manifest file
itself** — verified, zero `Kind: "file"`/`"document"` in that package. The
markdown producer's `document` node is the only anchor for every python
dependency edge, and `PartitionValidate` deliberately does not quarantine absent
endpoints (`internal/schema/schema.go:168-171`), so dropping it yields a
*silently* dangling edge rather than an error.

## Why this is not the heuristic the previous ADR forbade

`openspec/changes/2026-07-26-chromium-onboarding-defects/proposal.md:87-90`
rejected "heuristically detect 'this is not markdown'" as a guess. The
distinction:

- that would have been a judgement about a file's *nature* from its content;
- this is a **measurable structural property** of the file (density and
  consecutiveness of `#` prefixes), with a false-positive rate measured at zero
  over 30k files, and a failure direction that leaves junk in rather than
  deleting real headings.

It is still a rule with a threshold, and the thresholds (4, 20%) are the part
that must stay honest: they were derived from the measurement above, and the ADR
records them as such rather than as intuition.

## The measurability gate, and why it did not block this

The huddle's strongest objection was that the change could not be proven by this
repo's instrument: `min_nodes`/`min_edges` are floors it can only push *down*,
`min_score` can only move *up* and cannot rise on two corpora where `.txt`
contributes ~nothing, and chromium is not pinned. That objection was aimed at a
**heuristic**, where a false positive would silently delete real content and only
a judged question could catch it.

It does not apply to a scope decision. "We do not parse `.txt` as markdown" has
no false-positive mode to detect: the behaviour is total, visible in one golden
diff, and the cost is stated up front rather than discovered. What the objection
correctly demanded — *a measurement of what `.txt` extraction is worth* — was
supplied: 95.1% junk over 30,289 files, zero judged questions depending on it.

Still worth doing, and now unblocked rather than blocking:

## (Superseded) Prerequisite — this must be made measurable first

The change cannot currently be proven by this repo's own instrument, and that is
the strongest argument the huddle raised. `min_nodes`/`min_edges` are **floors**
that this change can only push *downward*; `min_score` may only move *up* and
cannot rise on two corpora where `.txt` contributes ~nothing; and **chromium is
not a pinned corpus**. So shipping today means editing a signed-off golden and
inverting a signed-off test with **no ratcheted number moving in either
direction** — indistinguishable from a regression under our own rules.

Order of work:

1. **Land a comment-char fixture** in `internal/golden/testdata/repos/` (hunspell
   shape: CRLF, `#`-prefixed licence prose) **plus a judged question that fails
   today** because a licence fragment outranks the real answer. This is the
   measurement the previous ADR demanded and never got.
2. Implement the comment-char test in `internal/extract/markdown`.
3. Watch that question go green; regenerate `pydeps.txt` (4 nodes / 4 edges, all
   junk — a reviewed diff).
4. Rewrite `crlf_test.go`: keep the CR-stripping and empty-heading assertions,
   move the "a real heading still lands" case to a `.md` fixture, and add a
   comment-char `.txt` case asserting the sections are NOT emitted.
5. **Fix `internal/navigator/navigator.go:211-231`**, which applies the identical
   `strings.HasPrefix(line, "#")`-is-a-heading rule to `README.txt` in a
   completely separate code path. Fix only the extractor and a module's one-line
   summary is still a licence fragment while the store says otherwise — two
   subsystems disagreeing about the same file.

## Explicitly NOT doing

- **Dropping `.txt` entirely** — orphans every python `declares` edge, silently.
- **A config key** (`"docs": {"txt": "plain"}`) — relocates the guess to the user
  and makes store contents non-reproducible from the repo alone, which collides
  with the git-diffable/deterministic invariant: two people with different config
  produce different graphs for the same commit.
- **Dropping link extraction from `.txt`.** `mdLinkRe`
  (`internal/extract/markdown/markdown.go:26`) only matches targets ending in
  `.md`, so a link from `INSTALL.txt` to `docs/build.md` is explicit syntax
  pointing at a real file — recall with no precision cost. Comment-header files
  contain no such links, so link extraction was never part of the measured
  pollution.

## Not claimed

- The 4-line / 20% thresholds are derived from one (large) sample. They are the
  weakest part of the design and should be revisited if a corpus is found where
  they misfire.
- With sections suppressed, reference edges from a comment-char file coarsen from
  section-anchored to file-anchored (`currentScope`,
  `markdown.go:294`, returns `docID` when no section is open). Stated because it
  is a real behaviour change, not discovered later.
- The 47 hunspell `document` nodes remain, and each still produces a
  content-free wiki page (`internal/wiki/wiki.go:52-53`). That is a separate
  question — whether a `document` node with no sections earns a page — and is not
  addressed here.
- `~341 genuine headings` is a hand-read count over sampled files, not an
  exhaustive audit of all 30,289.
