# ADR 34 — structure-aware command evidence projection

Status: SPIKED 2026-09-13. Not owner-accepted. **No product implementation.**
Throwaway prototype + 100 constructed fixtures live under `spike/`.
Handoff for other agents: `HANDOFF.md`.

Supersedes the draft titled "ADR 3" in this same file. Number 34 follows
ADR 33 (`2026-09-11-market-recon-the-seat-is-taken`).

## 1. The problem, measured before the spike

In a 30-day local Claude sample: 140.8M characters from `Read`, 90.3M from
`Bash`, 14,596 ad hoc Python programs (67.2% JSON/NDJSON). Agents spend more
context on command output and one-off transforms than on the graph.

Prefix truncation and line filters save bytes and can drop the late failure,
split a stack from its cause, or hide "no matches." The ranking unit is a
**canonicalized evidence block**, not a line or token.

This is an agent-facing presentation command. It is not a shell, a
byte-compatible pipeline stage, or an observability collector. The binary
stays deterministic: no LLM, embeddings, MCP, database driver, implicit
network, or background telemetry.

## 2. Spike results (constructed corpus, 2026-09-13)

Rerun: `python3 spike/p1/run.py` · `python3 spike/p1/exec_test.py` ·
`python3 spike/p2/run.py` · `python3 spike/stress_test.py`.

Corpus: **100** unique adversarial fixtures, 20 per family (logs, tests,
search, git, json). Oracle authored with the generator, not by the projector.
No historical transcript captures (secret risk). Split 51/29/20 train/dev/test;
3 template-cluster leaks.

### Late-fatal (80 INFO lines, crash on stderr, budget 256 tokens)

| method | critical recall |
|---|---:|
| prefix, source-order, exact-dedupe, Drain, Logram | **0.00** |
| head-tail | **1.00** |
| idf, idf+mandatory, submodular+mandatory | **1.00** |

Prefix is unsafe. Head-tail already keeps a *late* fatal. Ranking is not
required for that one shape.

### Tight percent budget (the non-vacuous regime)

| method | 10% critical | 10% weighted | median reduction | mandatory omissions |
|---|---:|---:|---:|---:|
| prefix | 0.460 | 0.294 | 0.48 | 58 |
| head-tail | 0.585 | 0.431 | 0.40 | 44 |
| **idf+mandatory** | **0.990** | **0.951** | 0.06 | 2 |
| submodular+mandatory | 0.980 | 0.951 | 0.06 | 3 |

idf+mandatory misses at 10%: `tests-maven`, `tests-race` (second critical line
dropped under an 8-token floor).

At a **512-token** cap, idf+mandatory is 1.00 critical in every family and
median reduction is ~2% — most fixtures fit whole. That budget does not test
the design. Do not quote 512-token scores as compression evidence.

### Ablations and contracts

- MMR gain over IDF = **0**. Submodular gain at 10% budget ≈ **0**.
- Drain did **not** merge PASS with FAIL.
- Determinism: 100 identical replays. Block conservation held.
- Fake canary `FAKESECRET_a1b2c3d4e5f6g7h8i9j0` absent from disclosure.
- Mandatory-over-budget emitted the FATAL line.
- Direct argv / execute-once / stdin / stdout≠stderr / exit status: passed
  (`spike/p1/exec_test.py`). No signal/PTY/1 GiB test.

### P2 structured transforms (independent gate)

| | |
|---|---|
| supported constructed replay | **10/10** equivalent |
| unsupported (eval / write / import) | **3/3** rejected, 0 false-supported |
| null vs missing | pass |
| malformed NDJSON | fail-closed |
| newest-400 transcript census | 530 programs, 280 unique AST, JSON 470 — **not** the 14,596-day set |
| occurrence-weighted 50% gate | **not measurable** |
| write sandbox / agent authoring | **not run** |

### Product gates vs this spike

| gate | result |
|---|---|
| 100% critical at every budget | FAIL (0.99 at 10%) |
| weighted ≥90% at 512 | PASS (vacuous) |
| ≥50% median reduction at accepted quality | FAIL (~6% at 10%) |
| +2 points for MMR/submodular | FAIL (0) |
| +2 points for graph impact | NOT RUN — keep **off** |
| agent-benchmark correctness / tool calls | NOT RUN |
| two-annotator α ≥ 0.80 | NOT RUN |

**Verdict:** the hypothesis is supported and prefix truncation is killed.
The feature is **not** ready to implement. Complexity without a session-level
win is the standing critique (`docs/CRITIQUE.md`).

## 3. Decisions

### D1 — direct argv execution, not shell emulation — ACCEPTED

Interface: `ctx-optimize compact run [flags] -- executable arg...`.
Everything after `--` is an argument vector. Never joined or reparsed.
Shell syntax requires an explicit `-- sh -c '...'`.

V1 rejects terminal-attached stdin before launch. PTY is a separate design.

Launch at most once. Projection failure, resource fallback, or "recall"
must never rerun the child.

### D2 — bounded in-memory capture, complete-output fallback — ACCEPTED

Separate stdout/stderr buffers. No claimed total cross-stream order.
No spool files (secrets). Over ceiling: emit buffered bytes, pass the rest
through, disclose abandonment, keep the child's exit status.

### D3 — source events → canonicalized evidence blocks — ACCEPTED

Family-specific segmentation. Emit **original bytes**. Canonicalize only for
template identity (timestamps, PIDs, UUIDs, hashes, durations). Paths, line
numbers, PASS/FAIL, expected/actual stay discriminative. A PASS/FAIL template
collision kills that canonicalizer (Drain passed this test).

Unknown family or low confidence → complete pass-through.

### D4 — mandatory evidence before the budget — ACCEPTED

Family gates cover: outcome/summary, unique errors, first complete stack,
location/target of a failure, no-result/truncation notices. Mandatory is
never displaced. If it exceeds budget, emit over budget and disclose.

Measured: regex mandatory without stack-following-fatal dropped location
frames; attaching continuation lines to a fatal fixed late-fatal+mand.
Tiny budgets still omit a second critical line if it is not itself mandatory.

### D5 — measurement chooses the optional ranker — ACCEPTED, winner is IDF

On this corpus the simplest method on the frontier is **output-local
canonical-template IDF + mandatory gates**. Integer/fixed-point scores,
source-order tie-break, no prior-run state.

MMR and submodular coverage are **not** accepted. They did not beat IDF by
2 points.

### D6 — graph impact off — ACCEPTED until an independent spike

Off by default. Must not change mandatory classification. Not run. Leave off.

### D7 — disclosure complete; durable raw recall deferred — ACCEPTED

Conservation: `source blocks = emitted + omitted`. Disclosure has IDs and
canonical summaries, never omitted bodies, never secret-bearing argv.

Encryption and regex redaction do **not** satisfy no-secret-storage for
arbitrary output. V1 has no recall handle. This is not a maybe.

`compact run` is presentation, not a byte-compatible substitute for the child.

### D8 — projection failure preserves child evidence and status — ACCEPTED

Internal projector error → emit captured output + fallback disclosure.
Does not rewrite a successful child's exit. Launch failure is a wrapper
failure because no child ran.

### D9 — structured transforms are a separate gate — ACCEPTED

Closed read-only operators only. No eval, callbacks, imports, network,
subprocesses. Writes are a later sub-gate. P2 constructed replay is
encouraging; coverage is **not** proven. Failure of P2 must not block P1,
and the reverse.

## 4. Risks that the spike confirmed

- [Prefix looks like compression] → it destroys late and mixed evidence.
  Kill it as a product default.
- [512-token caps lie on short output] → evaluate at percent-of-raw budgets.
- [Mandatory regex ≠ oracle-critical] → family gates must include stacks and
  secondary failure lines, or 100% recall is a lie.
- [Sophisticated rankers] → measured zero. Do not ship them for taste.
- [Raw recall vs secrets] → mutually incompatible. Do not "fix" with chmod.
- [Two capabilities, one blob] → keep P1 and P2 independently killable.

## 5. What another agent must not do

- Implement `internal/` packages, CLI dispatch, or docs that claim the verb
  exists.
- Reopen D1 (implicit `sh -c`) or D7 (durable raw bodies) without a new spike
  and owner sign-off.
- Ingest `~/.claude/projects` command output as fixtures.
- Quote token or bill savings from byte reduction.
- Turn graph impact on to "see if it helps" in product code.

## 6. Remaining open questions (owner)

- Accept this ADR as the contract and wait for a sanitized historical corpus
  plus the session benchmark, or kill the feature now?
- Default in-memory ceiling: 16 / 32 / 64 MiB per stream?
- Human presentation only, or also a `--json` envelope?
- Which families, if any, are first-class if historical evidence never lands?

## 7. Research basis (unchanged)

- Carbonell & Goldstein, MMR, SIGIR 1998 — measured, **not selected**.
- Lin & Bilmes, submodular summarization, ACL 2011 — measured, **not selected**.
- Drain (ICWS 2017), Logram, LogZip — used as canonicalizers, not rankers.
- Spärck Jones, IDF, 1972 — the ranker that survived, applied to *templates*
  not raw tokens (raw IDF treats UUIDs as precious).
