# Benchmarks

Honest head-to-head against graphify, deterministic paths only.

- `bench.py` — the harness (corpus paths are machine-local; adapt before running)
- `results.json` — raw results, 2026-07-11, Apple M5 Pro (18 cores, 48 GB), graphify 0.9.12

Methodology: graphify timed on its fastest deterministic path (`update
--no-cluster` — extraction only, no clustering, no LLM). ctx-optimize timed on
`add`, which additionally prunes stale nodes and regenerates the full wiki.
Query latency = median of 5 runs, same question, 2000-token budget, both CLIs
cold from the shell. Node counts are shown for scale, not equivalence — the tools disagree about
what counts as a node (ctx-optimize graphs import modules and markdown docs
as first-class nodes; graphify includes some file types we skip): ctx-optimize
shows ~34% more nodes on flask while the 12k-file totals land within 1%.
Where graphify finds more nodes/edges (gin, its own source) that is reported
as-is.

Published at https://muthuishere.github.io/ctx-optimize-site/ with the same numbers.
