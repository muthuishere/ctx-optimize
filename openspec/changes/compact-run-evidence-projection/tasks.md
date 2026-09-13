## 1. Freeze Spike Inputs

- [ ] 1.1 Record the reproducible 30-day Python census command, AST-normalization method, and de-identified aggregate output for P2
- [x] 1.2 Define the P1 event-fixture schema with stream, byte-range, exit, command, tool-version, environment, and graph-snapshot metadata
- [x] 1.3 Write the P1 annotation rubric and stable block-ID rules
- [ ] 1.4 Sanitize and freeze 200 historical P1 fixtures without admitting real secret values
- [x] 1.5 Add 100 adversarial P1 fixtures covering every family and resource edge case
- [x] 1.6 Split P1 fixtures by session, repository, and canonical template and verify no cluster leakage
- [ ] 1.7 Complete two-person test-set annotation and reach alpha >=0.80 before evaluating candidates

## 2. Run Evidence-Projection Spike

- [x] 2.1 Implement scratch-only structural segmenters that retain source-event provenance and emit original bytes
- [x] 2.2 Implement scratch-only prefix, head-plus-tail, source-order, and exact-deduplication baselines
- [x] 2.3 Implement and collision-test Drain-style and Logram-style canonical template variants
- [x] 2.4 Implement output-local IDF, MMR, and submodular-coverage ablations with stable integer scoring and tie breaks
- [x] 2.5 Implement family-specific mandatory gates and prove mandatory-over-budget behavior
- [ ] 2.6 Run all methods at the five precommitted budgets and publish per-family quality, reduction, and resource results
- [ ] 2.7 Run deterministic replay, execute-once, stream attribution, signal, capture-limit, and fake-secret artifact checks
- [ ] 2.8 Run the top three methods through the randomized blinded agent benchmark with five runs per condition
- [ ] 2.9 Run the graph-impact ablation against a frozen fresh store and record an independent keep/remove verdict
- [ ] 2.10 Record the P1 verdict, killed variants, pass-through families, confidence intervals, and raw result artifact hashes

## 3. Run Structured-Transform Spike

- [ ] 3.1 Recompute occurrence and unique-template counts from normalized Python AST hashes
- [ ] 3.2 Draw and human-label the weighted 1,000-program sample and freeze AST-cluster train/test splits
- [x] 3.3 Implement the scratch-only closed read-only transform prototype in primitive order
- [x] 3.4 Build the no-network differential replay harness using captured stdin and temporary filesystem snapshots
- [ ] 3.5 Replay at least 300 programs and report exact observable equivalence and false-supported counts
- [ ] 3.6 Run randomized agent tasks comparing Python and transform syntax for correctness, retries, command length, and tool calls
- [ ] 3.7 Evaluate filesystem writes separately for root escape, symlink safety, atomicity, permissions, disk-full, and interruption
- [ ] 3.8 Record independent P2 verdicts for read-only transforms and filesystem writes with confidence intervals

## 4. Decision Gate

- [ ] 4.1 Update the ADR with measured P1 and P2 results, replacing open algorithm choices with evidence
- [ ] 4.2 Resolve the memory ceiling, output contract, first supported families, and cross-platform process semantics
- [ ] 4.3 Remove any capability or subfeature that misses its precommitted acceptance gate
- [ ] 4.4 Obtain explicit owner acceptance for command projection before touching product code
- [ ] 4.5 Obtain separate explicit owner acceptance for structured transforms before touching product code

## 5. Implement Accepted Command Projection

- [ ] 5.1 Add direct argv parsing and pre-launch non-interactive validation under the `compact run` namespace
- [ ] 5.2 Implement cross-platform execute-once child lifecycle, stdin, signal forwarding, and exit mapping
- [ ] 5.3 Implement bounded in-memory per-stream capture and complete-output resource fallback
- [ ] 5.4 Implement only the segmenters, canonicalizers, mandatory gates, and ranker variants accepted by P1
- [ ] 5.5 Implement deterministic omission disclosure and block-conservation checks without durable raw retention
- [ ] 5.6 Add graph impact only if its independent P1 threshold passed
- [ ] 5.7 Add hermetic unit and integration tests for every command-evidence-projection scenario

## 6. Implement Accepted Structured Transforms

- [ ] 6.1 Add only the closed operators and syntax accepted by P2 under the `compact` namespace
- [ ] 6.2 Implement deterministic JSON/NDJSON, text, aggregation, and assertion semantics accepted by differential replay
- [ ] 6.3 Reject unsupported operations before input consumption or side effects
- [ ] 6.4 Add bounded atomic filesystem writes only if the separate write gate passed
- [ ] 6.5 Add hermetic differential and security tests for every structured-transforms scenario

## 7. Verify And Document

- [ ] 7.1 Run deterministic replay 100 times across supported concurrency settings and platforms
- [ ] 7.2 Run the full multi-pass benchmark and report correctness, diagnostic recall, tokens, bytes, tool calls, latency, and confidence separately
- [ ] 7.3 Run `task ci` and all supported cross-platform process tests
- [ ] 7.4 Document direct argv semantics, non-interactive limits, complete-output fallback, disclosure, and the absence of durable recall
- [ ] 7.5 Update `docs/VISION.md` and `docs/CRITIQUE.md` with accepted evidence, limits, and killed claims
