# Proof matrix — the pre-committed composite test

This is the kill-criteria test from `docs/CRITIQUE.md`, run for real:

> Thin slice (cards + deterministic wiki) on kernel + one true legacy repo:
> **composite <25% on hostile terrain → stop, or pivot to
> impact-analysis-only.**

## What is measured

Same harness, same model, same question, same corpus — twice:

- **Arm A (baseline):** the agent everyone already has. Shell search + file
  reads only. Explicitly forbidden from using ctx-optimize.
- **Arm B (product):** told the ctx-optimize store exists and to prefer it
  (`query` / `card` / `explain` / `path` / `affected` / wiki pages); may still
  open files to fill gaps — that's the honest product experience, not a
  store-only straitjacket.

Across three harnesses: **Claude Code** (`claude -p`, JSON usage report),
**Codex CLI** (`codex exec --json`, token_count events), **Devin CLI**
(`devin -p --export`). Each harness runs with its own defaults (model,
system prompt) — identical in both arms.

## Honesty rules

- **Tokens are compared within one harness only.** The three CLIs count
  differently (cache accounting, system prompts, reasoning tokens);
  cross-harness token comparisons would be fiction. The published number per
  harness is the A→B change in **fresh tokens** (input + cache-creation +
  output; cache reads reported separately).
- **Quality is graded before tokens count.** Every answer is judged against a
  ground-truth key (`questions.json`) derived by reading the source directly —
  not via the store, so the key can't be biased toward arm B. A cheaper wrong
  answer scores as a loss, not a saving.
- **Corpus is hostile terrain** — `linux/block` (98 files / ~73k lines of
  kernel C), the S1d corpus where the terrain law says the graph should help
  most. Per S1b, on modern well-named repos the store can be **worse** than
  grep; that finding stands and is published in the benchmarks. This test asks
  whether the composite (query + symbol cards + wiki) clears the pre-committed
  25% bar on the terrain the product claims.
- **Runs are resumable, raw payloads preserved** (`run.py` skips existing
  results; every run's full stdout/usage lands in `results/`).

## Reproduce

```sh
# corpus
git clone --depth 1 --filter=blob:none --sparse https://github.com/torvalds/linux
cd linux && git sparse-checkout set block
ctx-optimize init   # set name: linux-block in .ctxoptimize/config.json
ctx-optimize add .

# runs (any subset; results dir is resumable)
python3 proof/run.py --corpus /path/to/linux --out results --harness claude,codex,devin

# extract metrics + answers for judging
python3 proof/score.py results
```

Results: `RESULTS.md` (written after judging; raw metrics in
`results-metrics.json`).
