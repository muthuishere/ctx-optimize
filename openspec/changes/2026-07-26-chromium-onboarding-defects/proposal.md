# ADR — what onboarding chromium found

Status: **PARTIALLY IMPLEMENTED** — 2026-07-26. Two defects fixed, one
non-defect ruled out, one design issue recorded and NOT fixed.

## Context

`ctx-optimize up` was run on a full chromium checkout — by far the largest tree
this tool has met (366,277 nodes in the root residual alone). It completed, which
is the headline: the gather did not fall over, the store answers. But the output
carried four separate signals worth reading rather than skimming.

## 1. Module discovery: 241 "modules", 90% of them vendored — FIXED

`scan` reported 241 module directories. **217 (90%) were under `third_party/`**,
plus `out/Default` — the GN build output — and nested cases like
`net/third_party/quiche/src/depstool` and
`chrome/test/data/third_party/spaceport`.

None of those are modules. `pruneDirs` (`internal/scan/scan.go:102`) already
refuses `vendor`, `dist`, `build` and `target` on exactly this reasoning; it
simply did not know the names Google-style repos use. Added `third_party` and
`out`.

**Measured on chromium: 241 → 21 modules.**

The 21 that remain are mostly real (`v8/tools/turbolizer`, `tools/grit`,
`docs/website`), plus six `tools/crates/gnrt/sample_package*` Cargo fixtures. No
general rule distinguishes a fixture from a project, and `.ctxoptimize/config.json`
is curatable by hand, so six leftovers is the right stopping point.

Note this is **scan-only**. The code producer still walks those trees with its
own ignore rules, so nothing stops being indexed. What changes is that a vendored
subtree no longer gets its own store and its own line in the module list.

Risk accepted: a repo whose real code lives under `third_party/` or `out/` loses
automatic module discovery there. That is the same risk `build` and `target`
already carry, and `--markers` / hand-edited config are the escape hatch.

## 2. `quarantined 18 invalid item(s) … label is required` — FIXED

The id was `third_party/hunspell_dictionaries/README_cy_GB.txt::` — an empty
slug. Two defects stacked:

1. **CRLF was never stripped.** `strings.Split(content, "\n")`
   (`markdown.go:165,284`) left `\r` on every line of a CRLF file. Those files
   are shell-comment licence headers where every line starts with `#`, so the
   markdown producer read them as headings — and a bare `# ` line yielded the
   title `"\r"`, which slugs to nothing.
2. **An empty heading was emitted rather than skipped.** A heading with no text
   has no name to cite and no slug to build an id from. The schema correctly
   refused it; the producer should never have offered it.

Fixed both: `splitLines` strips the CR (so no stored value carries one), and an
empty title is skipped. Verified against the real chromium file: 28 nodes, **0
empty labels, no quarantine**. Pinned by `TestCRLFHeadingsDoNotEmitEmptyLabels`.

The CR fix matters beyond this crash: every value extracted from a CRLF file was
carrying a trailing carriage return into the store.

## 3. `skip … no such file or directory` ×2 — FIXED (as noise, not as an error)

Dangling symlinks in `third_party/nearby`. The walker saw the link, the read
followed it to nothing. That is the repo's state, not a problem with the file or
with us — and a tree this size has many. Reporting each one at the same volume as
a real parse failure trains the reader to ignore the channel that carries real
skips.

Now counted and summarized once (`skipped N broken symlink(s)`). Real skips keep
their per-file line.

## 4. Progress stopped at `[47/48]` — NOT A DEFECT

Investigated: every task ticks from a `defer` (`internal/app/multimodule.go:861`),
so the 48th cannot be skipped. `[48/48]` is the **root residual**, which is the
slowest task by far (366k nodes) and finishes after the quarantine/skip messages
that were the last lines pasted. Nothing to fix — recorded so the next person does
not re-investigate.

## Recorded, NOT fixed: `.txt` is parsed as markdown

The hunspell licence file now extracts cleanly — and what it extracts is junk:
`"addasu dan delerau Trwydded Gyhoeddus Gyffredinol Lai GNU (yr LGPL) fel"` is a
`section` node, because a `#`-prefixed prose line in a `.txt` file is
indistinguishable from a markdown heading.

Valid nodes, useless content. Fixing it means either dropping `.txt` from the doc
producer (losing real `.txt` docs) or heuristically detecting "this is not
markdown" — a guess, and the motto applies. Left alone deliberately; it deserves
its own ADR and a measurement of how much `.txt` extraction is worth in the first
place.

## Verified

- `task ci` green; hermetic + corpus golden green; judged tiers unmoved
  (16.5 / 13.0); corpus node/edge counts and gather times unchanged.
- `TestCRLFHeadingsDoNotEmitEmptyLabels`, `TestSplitLinesStripsCR`,
  `TestVendoredAndBuildOutputAreNotModules`.

## Not claimed

- The 241 → 21 measurement is one repo. Other large trees will have other
  vendoring conventions and this prune list will keep needing names.
- No claim that chromium's store *answers well* — only that it builds, and that
  the module list is now sane. Query quality on a 366k-node store is unmeasured
  and is the obvious next thing to check.
