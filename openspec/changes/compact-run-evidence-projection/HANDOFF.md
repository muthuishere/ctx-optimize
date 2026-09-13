# Handoff — compact-run evidence projection

Read this first. Then `design.md` (ADR 34). Then rerun spikes if you change
anything. **Do not write product code.** Owner has not accepted the ADR.

## What this is

A measured ADR + throwaway spikes for `ctx-optimize compact run -- argv…`:
project command output as canonicalized evidence blocks under a budget, without
becoming a shell, a secret store, or a second Python.

Committed on `main` as `dde702a` (ADR 34). Re-verified 2026-09-13 on main:
`results-p1.json` and `results-p2.json` reproduce byte-identical, every P1
contract passes, `openspec validate --strict` ok. `stress_test.py`'s decision
needles were updated to ADR 34's wording (they still asserted the pre-rewrite
draft's phrasing and "Status: PROPOSED", so the documented rerun failed 5
checks with no change to any decision).

`census.py` is NOT reproducible by construction: it scans the newest 400
transcripts in `~/.claude/projects`, so its count drifts as sessions accrue
(530 when committed, 563 on re-run). The committed JSON is the record.

## Rerun (scratch only)

```
cd openspec/changes/compact-run-evidence-projection
python3 spike/stress_test.py
python3 spike/p1/run.py
python3 spike/p1/exec_test.py
python3 spike/p2/run.py
python3 spike/p2/census.py    # aggregates only; never prints program text
openspec validate compact-run-evidence-projection --strict
```

Numbers live in `spike/RESULTS.md`, `spike/results-p1.json`,
`spike/results-p2.json`, `spike/results-census.json`.

## Proven (constructed, 2026-09-13)

- 100 adversarial fixtures, 20 each: logs / tests / search / git / json.
- Prefix truncation is unsafe: late-fatal critical **0.00**; 10% budget **0.46**.
- `idf + mandatory` is the winner on this corpus: late-fatal **1.00**;
  10% budget **0.99** critical / **0.95** weighted; 512-token **1.00** all families.
- Head-tail already keeps a *late* fatal. Ranking earns its keep on mixed
  shapes, not that one case.
- MMR / submodular gain over IDF ≈ **0**. Prefer the simpler ranker.
- Drain did not merge PASS/FAIL. Canary never entered disclosure.
- Direct argv, execute-once, stdin, stream split, exit status: passed in
  `spike/p1/exec_test.py`.
- P2 closed ops: 10/10 equivalent; eval / write / import rejected.
- Durable raw recall is **incompatible** with no-secret-storage. Deferred.

## Not proven — do not claim

- Historical / human-annotated corpus (task 1.4, 1.7).
- Agent-benchmark task correctness or tool-call reduction (2.8).
- Graph-impact boost (2.9) — leave **off**.
- 50% median reduction (measured ~6% at 10% budget; 512-token budget is vacuous
  because fixtures fit).
- 100% critical at every budget (2 misses at 8-token floor: `tests-maven`,
  `tests-race`).
- 14,596-program replaceability. Newest-400 census = 530 programs / 280 unique
  AST / JSON 470 — **not** that census.
- Interactive / TTY / shell-emulation / Windows process-group.

## If you continue

1. Do not implement `internal/` or CLI verbs.
2. Do not ingest Claude transcripts as fixtures (secrets).
3. Next useful work is a *sanitized* historical corpus, or stop and wait for
   owner accept/kill.
4. Structured transforms are a **separate** gate from the projector.
5. Cite ADR 34 decisions D1–D9; do not reopen shell transparency or raw recall
   without a new spike.

Owner question: accept the ADR as the contract and keep waiting on historical
evidence, or kill the feature as complexity without a session-benchmark win.
