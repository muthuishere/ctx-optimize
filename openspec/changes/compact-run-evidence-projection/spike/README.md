# compact-run spikes (throwaway)

Not product code. Other agents: start at `../HANDOFF.md`, then ADR 34
`../design.md`. Run from this directory:

```
python3 stress_test.py
python3 p1/run.py
python3 p2/run.py
```

P1 is constructed-only: no historical transcript corpus and no agent benchmark.
P2 replays 11 authored programs, not the 14,596-census.
