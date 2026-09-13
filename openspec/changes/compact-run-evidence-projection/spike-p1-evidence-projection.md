# Spike P1 - does evidence projection preserve diagnosis?

Status: PARTIAL RUN 2026-09-13. 100 constructed adversarial fixtures (20/family).
No historical corpus, no two-annotator alpha, no agent benchmark, no graph
impact. Measured numbers: `spike/results-p1.json`. No product code.

## Question

Can canonicalized evidence blocks reduce returned context while preserving the
facts needed to diagnose and act, and does any sophisticated ranker outperform a
simple exact-deduplication plus rarity baseline?

## Frozen Corpus

Create 300 immutable fixtures, 60 per family. Each family contains 40 sanitized
historical captures and 20 constructed adversarial captures.

| Family | Required coverage |
|---|---|
| logs | repeated templates, timestamp/PID variance, multiline stacks, late fatal, interleaved services, ANSI/progress |
| tests/builds | success, compiler error, one/many failures, retries, nested causes, warnings plus late summary |
| search/read | context hunks, duplicates, no matches, long lines, binary notice, explicit truncation |
| git | status, diff, log, rename, conflict, binary diff, detached HEAD, dirty tree, failing hook |
| JSON/NDJSON | arrays, scalars, malformed/truncated records, repeated objects, heterogeneous errors, huge records, exit-zero semantic failure |

Capture stdout/stderr events, per-stream byte offsets, exit status, exact argv,
tool version, locale, timezone, terminal width, tokenizer version, and graph
snapshot hash. Split train/dev/test 50/25/25 by session, repository, and
canonical template so near-duplicates cannot cross splits.

Fixtures MUST contain planted fake secret canaries, never real secrets. Historical
captures are sanitized before they enter the repository.

## Ground Truth

Two annotators inspect raw output before seeing a candidate projection. Every raw
event receives a stable ID. Annotators define oracle evidence blocks and label:

- critical, relevant, supporting, or noise;
- outcome class and diagnostic facets: cause, location, affected target,
  summary, remediation, and no-result;
- context dependencies and complete causal groups;
- exact-duplicate and semantic-template cluster;
- expected diagnosis and next action;
- whether collapse is safe and which variable fields must survive.

Adjudicate the frozen test set. Require Krippendorff's alpha >=0.80 for
criticality and template equivalence; otherwise revise the rubric before running
candidates. Candidate blocks retain raw-event provenance so boundary differences
can be scored against oracle blocks.

## Methods

Evaluate every method at 256, 512, and 1,024 tokens and at 10% and 25% of raw
bytes:

1. raw prefix truncation;
2. raw head-plus-tail truncation;
3. structural blocks packed in source order;
4. structural blocks plus exact deduplication;
5. Drain-style and Logram-style template normalization independently;
6. output-local canonical-template IDF;
7. IDF plus MMR;
8. weighted submodular diagnostic-facet coverage;
9. each viable ranker with mandatory gates off and on;
10. the winner with graph impact off and on.

Mandatory blocks are selected first. If they exceed budget, preserve them over
budget. Graph boosts are capped and eligible only for exact file, location, or
symbol resolution against the frozen fresh snapshot.

## Metrics

Quality:

- critical-block recall and false mandatory omission count;
- weighted evidence recall: critical 5, relevant 2, supporting 1;
- diagnostic-facet recall, block-boundary F1, and template-cluster purity;
- NDCG and quality-per-token frontier;
- blinded downstream diagnosis and next-action correctness;
- follow-up retrievals, reruns, and total tool calls.

Contract:

- argv, stdin, exit, signal, and execute-once preservation;
- stdout/stderr attribution and block conservation;
- byte-identical replay over 100 runs and varied `GOMAXPROCS`;
- zero fake-secret canaries in durable artifacts;
- wall time, peak RSS, returned bytes, and estimated tokens;
- 1 GiB output, one 100 MiB line, invalid UTF-8, cancellation, and capture-limit fallback.

Run the best three variants through the existing multi-pass agent benchmark,
randomized and blinded against full output and both truncation baselines. Use
five runs per condition and case-level bootstrap 95% confidence intervals.

## Acceptance

- 100% critical-block recall at every budget.
- Zero failure/success template collisions.
- 100% argv, exit, stream attribution, execute-once, conservation, and
  determinism checks.
- At 512 tokens, weighted recall >=90% overall and >=85% per family.
- Downstream correctness no more than two percentage points below full output.
- At least five points better correctness than head-plus-tail, or at least 15%
  fewer follow-up/rerun tool calls.
- Median returned-context reduction >=50% at the accepted quality point.
- MMR or submodular coverage must improve weighted recall by >=2 points over
  exact deduplication plus rarity; otherwise use the simpler method.
- Graph impact must improve graph-relevant recall by >=2 points with no family
  regressing by more than one point; otherwise remove it.

## Kill Criteria

- Any method omits mandatory evidence on the held-out set.
- A canonicalizer merges different outcomes or diagnostic values.
- Any family regresses by more than five quality points.
- Projection improves neither downstream correctness nor follow-up tool count.
- Value depends on transparent shell emulation or durable complete raw recall.
- Resource fallback reruns a command, changes its outcome, deadlocks, or leaks a
  managed child.

## Deliverables

- Sanitized fixture manifest and annotation rubric.
- Reproducible prototype and one command that runs every ablation.
- Machine-readable per-case results and bootstrap intervals.
- A verdict table: accepted family/ranker, pass-through families, killed
  canonicalizers, and graph-impact decision.
- ADR update replacing every open ranking choice with measured evidence.
