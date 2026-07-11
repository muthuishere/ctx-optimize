# Benchmarks

Honest head-to-head against graphify, deterministic paths only.

- `bench.py` — the harness (corpus paths are machine-local; adapt before running)
- `results.json` — raw results, 2026-07-11, Apple M5 Pro (18 cores, 48 GB), graphify 0.9.12

Methodology: graphify timed on its fastest deterministic path (`update
--no-cluster` — extraction only, no clustering, no LLM). ctx-optimize timed on
`add`, which additionally prunes stale nodes and regenerates the full wiki.
Query latency = median of 5 runs, same question, 2000-token budget, both CLIs
cold from the shell. Node counts on shared corpora are within a few percent —
both tools parsed the same code; where graphify finds more edges (gin, its own
source) that is reported as-is.

Published at https://muthuishere.github.io/ctx-optimize-site/ with the same numbers.
