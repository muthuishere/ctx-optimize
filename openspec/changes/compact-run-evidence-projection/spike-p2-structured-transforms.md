# Spike P2 - can bounded transforms replace recurring Python safely?

Status: PARTIAL RUN 2026-09-13. Closed-operator prototype: 10/10 supported
equivalent, 3 unsupported rejected. Newest-400 transcript census: 530 programs
/ 280 unique AST (aggregates only). 14,596-day replay, 1,000-label sample,
write sandbox, and agent authoring test are NOT run. Independent of P1.

## Existing Evidence

A 30-day local Claude transcript census found 14,596 Python programs: 14,022
inline and 574 written `.py`, with 13,194 unique source texts. Categories overlap
and MUST NOT be summed.

| Category | Occurrences | Share |
|---|---:|---:|
| JSON/NDJSON | 9,804 | 67.2% |
| file writes | 5,415 | 37.1% |
| text/regex | 3,268 | 22.4% |
| verification | 1,720 | 11.8% |
| aggregation | 1,504 | 10.3% |
| filesystem | 1,111 | 7.6% |
| SQLite | 544 | 3.7% |

These lexical categories establish prevalence only. They do not prove intent,
unique-template coverage, or behavioral replaceability.

## Question

Can a small closed operator set replace a material share of repeated ad hoc
Python with exact observable behavior and measurably simpler agent usage, without
becoming a scripting language?

## Method

1. Recompute the census using normalized Python AST hashes and preserve both
   occurrence-weighted and unique-template counts.
2. Human-label a stratified sample of 1,000 programs, oversampling rare classes
   and writes, then weight estimates back to the population.
3. Hold out complete AST-template clusters rather than individual occurrences.
4. Annotate intent, inputs, outputs, side effects, ordering, error behavior,
   required operators, and replaceability as one operation, <=3-operation
   pipeline, or not safely replaceable.
5. Build a throwaway reference runner in this change or scratch, not product
   code. Add primitives in this order: JSON parse/project/filter, NDJSON stream,
   sort/unique, regex extraction/grouping, aggregation, and assertions.
6. Evaluate bounded atomic filesystem writes only after the read-only core.
7. Replay at least 300 programs with captured stdin and temporary filesystem
   snapshots in a no-network, resource-limited sandbox.
8. Compare exit status, stdout, stderr, parsed value, diagnostics, and complete
   filesystem diff. Missing historical inputs are unverified, never passes.
9. Run randomized agent authoring tasks with Python versus transform syntax and
   measure correctness, retries, command length, and tool calls.

## Adversarial Fixtures

- null versus absent and duplicate JSON keys;
- large numbers, Unicode, malformed/truncated NDJSON, and huge records;
- stable sorting, duplicate preservation, and empty aggregations;
- regex backreferences, zero-width matches, multiline input, and invalid regex;
- newline/encoding differences and partial output on failure;
- symlink and path traversal, existing-target permissions, disk-full simulation,
  and interrupted atomic replacement.

## Metrics

- occurrence-weighted and unique-AST-template exact replaceability;
- one-operation and <=3-operation pipeline coverage;
- category-level coverage with 95% confidence intervals;
- observable replay equivalence and false-supported count;
- task correctness, retries, total tool calls, and command length;
- transform runtime and peak RSS versus Python;
- write sandbox escapes, symlink escapes, and partial-write corruption.

## Acceptance

- Lower 95% confidence bound >=50% occurrence-weighted exact replaceability.
- Unique-template coverage >=35%.
- >=70% coverage of JSON/NDJSON programs with complete replay inputs.
- 100% observable equivalence for every program declared supported.
- Zero unsupported programs falsely accepted.
- Agent task correctness within two points of Python.
- At least 20% fewer retries/tool calls or 40% shorter median command.
- No eval, embedded scripting, arbitrary callback, subprocess, or network escape.
- Write support ships only with zero root escape, symlink escape, or partial-write
  corruption; otherwise the read-only core is judged separately.

## Kill Criteria

- The lower replaceability confidence bound is below 40%.
- Coverage depends on a Turing-complete expression language.
- Any supported replay has an unexplained observable mismatch.
- Syntax is not materially easier for agents than the Python it replaces.
- Static AST classification is the only evidence; behavioral replay cannot be
  reproduced.

## Deliverables

- Reproducible census script and de-identified aggregate report.
- Frozen labeled sample and AST-cluster split manifest.
- Throwaway transform prototype and differential replay harness.
- Primitive-by-primitive coverage, equivalence, and usability table.
- Independent verdicts for read-only transforms and filesystem writes.
