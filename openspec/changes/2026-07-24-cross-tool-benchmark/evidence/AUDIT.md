# Adversarial audit — cross-tool benchmark (2026-07-24 run)

**Verdict: PUBLISH-WITH-FIXES**

The run is honest in its bones — real commands, real logs, real losses present,
node/edge counts trace to tool stdout, query-latency medians recompute
correctly from the raw logs I spot-checked. But I found one data-integrity bug
(broken ast-grep pattern silently timed as a real query on 2/4 corpora), one
mislabeled-methodology bug (GitNexus "warm" runs that were actually full
rebuilds on 2/4 corpora), and one live site-conflation risk (an existing page
already publishes "12,484 files" for "graphify's own source" — the new run's
`corpus-graphify-src` is 754 files, same label). None of these were invented
to flatter ctx-optimize — they're runner slip-ups — but all three must be
fixed or caveated before anything from `results-multi.json` reaches the site.

## What I checked

- Reconstructed the query-latency median for corpus-flask ctx-optimize by
  hand from logs 073–077 (`dt` = 0.098/0.059/0.071/0.037/0.045 → median
  0.059s = 59ms) — matches `results-multi.json` exactly.
- Verified `cold_s` is `min` of the 3 logged cold runs (not mean, not
  cherry-picked) for ctx-optimize (flask: logs 001–003 → 0.664/0.529/0.530 →
  reported 0.529 ✓) and CodeGraph (flask: logs 010–012 → 0.470/0.461/0.458 →
  reported 0.458 ✓; gin: 019–021→0.566 min matches). Same rule applied to
  both — no tool gets "best of 3" while another gets "first run only."
- Verified node/edge counts against each tool's own stdout for ctx-optimize,
  CodeGraph (`status -j`: `nodeCount:2705, edgeCount:5168, dbSizeBytes:5206016`
  — all three match `results-multi.json` verbatim), and GitNexus (stdout
  summary line `13,767 nodes | 26,749 edges` for corpus-graphify-src matches).
- Confirmed no store-dir leakage as of this run: `corpora/*/{.codegraph,
  .gitnexus,graphify-out}` exist (left in place after the run, not deleted),
  but the quality-capture baseline grep (`quality/corpus-ctx-src/baseline__q3.txt`)
  was actually invoked with `--exclude-dir={.codegraph,.gitnexus,graphify-out}`
  — the fix RUN-NOTES claims did hold at capture time.
- Confirmed the 754-vs-12k caveat is stated plainly in RUN-NOTES, and did NOT
  find "12k files" anywhere in `results-multi.json`, `AUDIT.md` inputs, or
  RUN-NOTES framing it as measured. **But** I checked the live site and found
  it already publishes a different, older "12,484 files" graphify-source
  claim — see Issue 3.

## Issues found

| # | Severity | Finding | Evidence |
|---|---|---|---|
| 1 | **HIGH** | ast-grep's query-latency cell for **corpus-flask** (40ms) and **corpus-graphify-src** (80ms) is timing a pattern that matches **zero results** (`rc=1`, empty stdout) — the exact "trailing colon breaks return-type-annotated defs" bug RUN-NOTES documents fixing for the *quality* capture pass, but the *speed*-axis logs (093–097, 168–172) still use the broken `def $NAME($$$):` pattern and were never re-run. ast-grep is not "querying nothing in 40ms," it's failing to match in 40ms. Real (matching) timings would almost certainly be higher. | logs `093-097-ast-grep-corpus-flask-query*.log` and `168-172-ast-grep-corpus-graphify-src-query*.log`: all `rc=1`, empty stdout, non-zero `dt`. Compare to corpus-gin (118, working Go pattern) which returns real matches. |
| 2 | **MEDIUM** | GitNexus's "warm" gather for **corpus-ctx-src** (log 054, 9.22s) and **corpus-graphify-src** (log 072, 27.928s) are NOT incremental re-syncs — the tool printed `index schema changed ... forcing a full rebuild` and did a second cold-equivalent pass. RUN-NOTES' blanket claim ("warm = same command again, GitNexus detects the unchanged tree and does an incremental pass") is true only for corpus-flask (log 018: `Incremental: changed=0, added=0, deleted=0`) and corpus-gin (log 036, same). This makes GitNexus's `warm_s` for 2 of 4 corpora look artificially bad relative to what a real warm re-sync would cost — a mistake that happens to hurt GitNexus, not ctx-optimize, but it's still a wrong number sitting next to a false description. | logs 018, 036 (real incremental) vs 054, 072 (forced full rebuild, message printed in stdout). |
| 3 | **HIGH (site-level, not this run's fault)** | The existing published site (`site/index.html` L265, L370, L433; `site/compare.html` L169, L178) already claims **"graphify's own source, 12,484 files"** with a 13.3× cold-gather win. The new run's `corpus-graphify-src` is the **same graphify repo, same label idea, 754 files** — a completely different number for what could look like "the same benchmark." If the new numbers go up under a similar "graphify source" label without hard-partitioning from the old page, a reader (or a future agent updating the site) will conflate two different runs into one inconsistent story. | `grep 12,484 site/*.html`; RUN-NOTES's own caveat about 754 vs the ADR's assumed ~12k. |
| 4 | **LOW** | Node/edge counts differ by large margins purely due to each tool's own definition of "node"/"edge" (e.g. corpus-flask: ctx-optimize 2381/3835, CodeGraph 2705/5168, graphify 1473/2801, GitNexus 2201/3658). Neither the ADR's "What we measure" section nor RUN-NOTES states outright "these counts are not comparable across tools, don't use them to claim X finds more than Y." Nothing in the current data *makes* that claim, but there's no explicit guardrail stopping a future site-copy pass from writing "ctx-optimize's graph has more/fewer nodes" as if it meant something. | `results-multi.json` per-tool node/edge fields; absence of a disclaimer in ADR §"What we measure" item 2 or RUN-NOTES. |
| 5 | **LOW** | CodeGraph's query-latency cell uses a single keyword (`route`/`router`/`store`/`graph`) via its symbol-search CLI, not the same free-text question the other 4 tools got. This is disclosed (`codegraph_query_term` field + RUN-NOTES paragraph), so it's not hidden — but it is still not a like-for-like "same question" comparison and should be labeled as such wherever the query table is rendered, not just in the JSON. | `results-multi.json` query blocks; RUN-NOTES "CodeGraph's `query` command is a symbol-name search..." |
| 6 | **INFO / no action** | ctx-optimize's own "warm" gather is *not* meaningfully faster than cold on any corpus (e.g. corpus-graphify-src: cold 1.174s vs warm 1.233s — warm is slower), unlike CodeGraph's `sync` (0.2s warm vs 1.9s cold) or GitNexus's real incremental runs (5.3s vs 6.5s). ctx-optimize's `add` re-does full extraction even when nothing changed (log 004: "added 0 nodes" but full-cost pass). This is real and already visible in the JSON, not hidden — but the site must not imply ctx-optimize has an incremental fast-path the way CodeGraph demonstrably does; it would be an unearned claim in the other direction (flattering, but false) if worded loosely. | logs 004, 022, 040, 058 ("added 0 nodes", full-length `dt`); compare CodeGraph sync logs 013, 031, 049, 067 (0.13–0.2s flat). |

## Cells where a rival beats ctx-optimize — MUST stay visible in the published table

- **CodeGraph, corpus-flask, cold gather**: 0.458s vs ctx-optimize's 0.529s — CodeGraph wins.
- **CodeGraph, ALL 4 corpora, warm/sync**: 0.143–0.2s vs ctx-optimize's 0.557–1.233s — CodeGraph wins by 3–8×, consistently, every corpus. This is the single most important loss cell: ctx-optimize has no fast incremental path, CodeGraph clearly does.
- **ast-grep, corpus-flask, query latency**: 40ms vs ctx-optimize's 59ms — *but see Issue 1: this ast-grep number is a broken (zero-match) run, not a real win.* Re-run with a fixed pattern before deciding whether this cell is real.
- **ast-grep, corpus-graphify-src, query latency**: 80ms vs ctx-optimize's 82ms — same caveat as above, near-tie either way, re-run before publishing as a "loss."
- **GitNexus/graphify/CodeGraph node or edge counts exceeding ctx-optimize's** on individual corpora (e.g. CodeGraph corpus-flask 2705 nodes vs ctx-optimize's 2381) — not a "loss" in a judged sense, but must not be memory-holed if the site ever shows a node-count column.

None of these were buried in the JSON or RUN-NOTES — they're all present as plain numbers. The risk is entirely in how `site/` chooses to *render* the table, not in what was measured.

## Confirmed clean (tried to break these, couldn't)

- **Corpora identity / no store-dir leakage into another tool's timing**: verified the documented baseline-grep-sweeping-stores bug fix actually landed in the executed command (`--exclude-dir=...` present in the logged baseline command), not just claimed in prose.
- **Fastest-path fairness**: every rival is on its own genuinely-fastest deterministic flag (`--no-cluster`, `--skip-git --index-only`, no `--embeddings`), and ctx-optimize's `add` does *more* work than the rivals' gather (wiki regen — confirmed in log 001's "wiki: 119 pages" line), which is the fair direction of asymmetry, not a cheap trick.
- **Invocation parity**: best-of-3 cold / 1 warm / median-of-5 query applied identically to every tool — checked by hand for ctx-optimize, CodeGraph, GitNexus.
- **Omissions stated**: Serena and potpie both have explicit, defensible omission reasons in RUN-NOTES (no persistent-graph artifact; requires standing up Neo4j) — not silently dropped after a bad result.
- **754-not-12k**: RUN-NOTES states this plainly and no number in `results-multi.json` claims 12k. (The conflation risk is with the *pre-existing* site content, not this run — see Issue 3.)
- **No cell is unlogged**: every one of the ~10 cells I spot-checked (cold/warm timings, node/edge counts, query-latency median) traced exactly to a real log file's `dt`/stdout.

## Concrete framing rules for the site

1. **Fix or drop Issue 1 before publishing ast-grep query numbers** for corpus-flask and corpus-graphify-src — re-run with a working Python pattern (drop the trailing `:`, same fix already applied to the quality-capture pass) and use the corrected timing, or omit those two ast-grep query cells with a stated reason.
2. **Re-label or footnote GitNexus's warm numbers** for corpus-ctx-src and corpus-graphify-src as "forced full rebuild (schema version bump mid-run), not a true incremental resync" — do not present all four GitNexus warm cells as apples-to-apples incremental timings.
3. **Never let the new `corpus-graphify-src` (754 files) numbers sit under a "graphify source" label without an explicit file count next to it, every single time.** The existing site already has a "graphify's own source, 12,484 files" claim from a different, older run. Any new page/table referencing "graphify source" must state "754 files (this run)" inline, not just in a footnote, and should not be laid out near the old 12,484-file claim without a clear "different run, different corpus size" callout — ideally, retire or clearly archive the old 12,484 claim rather than let two numbers for "the same thing" coexist.
4. **Always show CodeGraph's warm/sync win** (0.13–0.2s vs ctx-optimize's 0.55–1.2s, every corpus) wherever a warm/incremental row appears — this is real, consistent, and the biggest single hole in ctx-optimize's story in this dataset.
5. **State plainly that node/edge counts are not comparable across tools** (different extraction granularity, different definitions of "node") anywhere they're shown side by side — don't let a bare number imply "finds more/better."
6. **Keep the CodeGraph symbol-term caveat visible in the rendered table**, not just the JSON (`codegraph_query_term` field) — a reader glancing at a results table with no asterisk will assume it's the same free-text question every other tool got.
7. **State ctx-optimize has no incremental fast-path** in this measured run (`add` on an unchanged tree costs the same as cold) if the site discusses "warm" performance at all — don't imply parity with CodeGraph's genuine sync path.

## Top 3 things that would make this dishonest if ignored

1. Publishing ast-grep's corpus-flask/corpus-graphify-src query-latency cells as real wins/losses for ctx-optimize when they're timing a broken, zero-match pattern (Issue 1) — this is the one number in the whole dataset that is not actually measuring what it claims to measure.
2. Letting the new 754-file `corpus-graphify-src` numbers get laid next to or blended with the site's existing "graphify's own source, 12,484 files" claim (Issue 3) — two different corpora under one name is the most likely way this benchmark misleads a reader, and the raw material for that mistake already exists on the site today.
3. Dropping or soft-pedaling CodeGraph's clean, consistent, 3–8× warm/sync win across all four corpora — it's real, it's uncomfortable, and it's exactly the kind of loss the honesty contract exists to keep visible.
