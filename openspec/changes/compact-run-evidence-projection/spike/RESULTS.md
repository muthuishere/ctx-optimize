# Spike results 2026-09-13

```
python3 spike/stress_test.py
python3 spike/p1/run.py
python3 spike/p1/exec_test.py
python3 spike/p2/run.py
python3 spike/p2/census.py
```

## Artifact stress

0 fails. `openspec validate --strict` passes.

## P1 — 100 constructed adversarial fixtures (20 per family)

No historical transcript captures (secret risk). Split 51/29/20; 3 template-cluster leaks.

**Late-fatal, budget 256:** prefix 0.00 · head-tail 1.00 · idf+mand 1.00

**10% budget (the non-vacuous regime):**

| method | critical | weighted | median reduction | omissions |
|---|---:|---:|---:|---:|
| prefix | 0.460 | 0.294 | 0.48 | 58 |
| head-tail | 0.585 | 0.431 | 0.40 | 44 |
| idf+mand | 0.990 | 0.951 | 0.06 | 2 |
| submodular+mand | 0.980 | 0.951 | 0.06 | 3 |

idf+mand misses at 10%: `tests-maven`, `tests-race` (second critical line not packed under 8-token floor).

At 512 tokens every family is 1.00 critical for idf+mand and reduction is ~2% — fixtures fit, budget is vacuous.

MMR gain over IDF = 0. Contracts: determinism 100, conservation, canary, execute-once argv/stdin/streams/exit, mandatory-over-budget all passed.

**Product gates:** still not met (not 100% critical at every budget; reduction << 50%).

## P2

Constructed replay: 10/10 supported equivalent; eval/write/import rejected; null vs missing pass.

Newest-400-transcript census (text never printed): 530 programs, 280 unique AST, JSON 470/530. This is **not** the 14,596 30-day census and cannot claim the 50% coverage gate.

## Do not implement product code
