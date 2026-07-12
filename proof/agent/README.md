# Headless benchmark — run it yourself

Don't trust our numbers. Run them. This harness clones a small repo, builds a
ctx-optimize store, and lets the **same model** answer a set of questions
**twice** — once with shell only, once with the store first — then reports the
real time, tokens, cost, and step count from **OpenRouter's own usage
accounting** (not our estimate).

No Anthropic account needed; it runs on any OpenRouter model.

## Locally

```sh
export OPENROUTER_API_KEY=sk-or-...      # your key; only read from the env, never logged
proof/agent/run-bench.sh                 # defaults: gorilla/mux, openai/gpt-4o-mini
```

Options: `--model <slug>` · `--repo <url> --name <short>` · `--questions <file>`
· `--bin <path>` (skip the `go build`) · `--out <dir>`.

It prints a per-question table and the three headline deltas, and writes
`results/SUMMARY-<name>.md` plus one raw JSON record per run.

## On GitHub Actions (fully headless)

1. Fork the repo.
2. Settings → Secrets and variables → Actions → add `OPENROUTER_API_KEY`.
3. Actions → **benchmark** → **Run workflow** (pick a model / repo, or take the
   defaults).

The result table lands in the run's **job summary**; the raw records are
uploaded as the `benchmark-results` artifact. Workflow file:
[`.github/workflows/benchmark.yml`](../../.github/workflows/benchmark.yml).

## The two arms

| | tools the model gets | steered to |
|---|---|---|
| **arm a** | `run_shell` (grep/rg/find/sed/cat) | find the answer however |
| **arm b** | `ctx_optimize` (query/card/affected/path/explain) + `run_shell` for gaps | prefer the store |

Same model, same temperature (0), same question, same freshly-cloned repo.
Tokens and cost are compared honestly: the model and prompt are identical, so
the only variable is *how it looks things up*.

## What to expect

On a **small, well-named repo** like gorilla/mux — the terrain where plain
grep is already strong, i.e. the *hardest* case for us — the store still cuts
steps by roughly two-thirds (it answers most questions in a single `query`
call, vs a 2–4 step grep-and-read chain), which shows up as lower wall time and
lower cost. Token savings are modest here and grow with repo size and question
difficulty. On sprawling or unfamiliar code the gap widens; on tiny code it
narrows — we publish both rather than cherry-pick.

Quality is not sacrificed for cheapness: answers cite `file:line`, and a
cheaper-but-wrong answer is a loss, not a saving — inspect the `answer` field
in each record and judge for yourself.
