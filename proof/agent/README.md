# Headless benchmark — run it yourself

Don't trust our numbers. Run them. This harness clones a small repo, builds the
knowledge stores, and lets the **same model** answer a set of questions **three
ways** — shell only, ctx-optimize store first, and **graphify** store first —
then reports the real time, tokens, cost, and step count from **OpenRouter's own
usage accounting** (not our estimate).

No Anthropic account needed; it runs on any OpenRouter model.

## Locally

```sh
export OPENROUTER_API_KEY=sk-or-...      # your key; only read from the env, never logged
proof/agent/run-bench.sh                 # defaults: gorilla/mux, openai/gpt-4o-mini
```

Options: `--model <slug>` · `--repo <url> --name <short>` · `--questions <file>`
· `--bin <path>` (skip the `go build`) · `--out <dir>` · `--monorepo`
(multi-module build: `init --scan --yes && add .`) · `--smoke` (free
deterministic checks only) · `--smoke-monorepo` (both, on etcd).

## Answer-quality arm (correctness, not cost)

`run-bench.sh` measures what an answer *costs*. `run-quality.sh` measures whether
it is *right* — the primary metric, since ripgrep is already faster than us at
raw lookup.

```sh
proof/agent/run-quality.sh                       # 12 questions x 3 arms
proof/agent/run-quality.sh --only "i5 l2"        # pilot
node proof/agent/grade.mjs --results r1,r2,r3 \
  --name gorilla-mux-graded --questions proof/agent/questions-graded-mux.json
```

- Grading is **deterministic — no LLM judge.** `grade.mjs` scores the agent's
  final ANSWER against hand-verified facts declared in the questions file (each
  carries the `truth` file:line it was checked against), plus `forbid` patterns
  that catch false claims. The identical rule runs on every arm and the grader
  never sees which arm produced the answer.
- **One clone per arm**, so arm a's grep cannot read `graphify-out/` or
  `.ctxoptimize/`.
- `agent.mjs` records a `trace` of every tool call, a `truncated` flag (hit the
  step cap without answering) and `tool_errors`, so no "fast" number can be
  trusted without looking at what actually came back. grep exit 1 is reported as
  `[no matches]`, not an error.
- Aggregate several runs: the agent is not deterministic even at temperature 0,
  and single-run totals swing by up to 20 points.

Measured results and the honest caveats: [`RESULTS-QUALITY.md`](RESULTS-QUALITY.md).

## Monorepo arm (multi-module)

etcd is a real 12-module repo (api, client/v3, server, pkg, etcdctl, …).
`--monorepo` builds **one store per module + a navigator** and the agent
queries from the repo root, where answers federate across modules — the flow
graphify has no equivalent for (its per-directory graphs are built by hand
and never merged back into one query surface).

```sh
proof/agent/run-bench.sh --smoke-monorepo               # FREE: no key, no model
proof/agent/run-bench.sh --monorepo \
  --questions proof/agent/questions-monorepo.json       # paid three-way run
```

The smoke mode verifies the store's answers deterministically: every question
in `questions-monorepo.json` carries the exact `ctx-optimize` argv and the
file paths its output must contain (real, verified citations — e.g.
`watcher.Watch → client/v3/watch.go`), including two scope checks run from
*inside* `client/v3`: a module-scoped answer labeled `[client-v3]`, and a
cross-module `card` that answers from the owning module with
`[not in client-v3 — found in server]`. No API key, no cost — CI-friendly.

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

## The three arms

| | tools the model gets | steered to |
|---|---|---|
| **arm a** | `run_shell` (grep/rg/find/sed/cat) | find the answer however |
| **arm b** | `ctx_optimize` (query/card/affected/path/explain) + `run_shell` for gaps | prefer the store |
| **arm c** | `graphify` (query/explain/affected/path) + `run_shell` for gaps | prefer the graph |

Same model, same temperature (0), same question, same freshly-cloned repo.
Tokens and cost are compared honestly: the model and prompt are identical, so
the only variable is *how it looks things up*. Arm c only runs when the
`graphify` CLI is installed (`pipx install graphifyy`); both stores are built
offline with no LLM (`ctx-optimize add .`, `graphify update . --no-cluster`).

## What to expect

On a **small, well-named repo** like gorilla/mux — the terrain where plain
grep is already strong, i.e. the *hardest* case for us — the store still cuts
steps by roughly two-thirds (it answers most questions in a single `query`
call, vs a 2–4 step grep-and-read chain), which shows up as lower wall time and
lower cost. Token savings are modest here and grow with repo size and question
difficulty. On sprawling or unfamiliar code the gap widens; on tiny code it
narrows — we publish both rather than cherry-pick.

Against **graphify** specifically: both build an offline graph, but graphify's
`query` returns a raw BFS node dump (often 100+ nodes), so the model pays more
tokens to wade through it — in our runs graphify's token use lands at or above
plain shell, while ctx-optimize's `query`/`card` return a tight, cited,
signature-bearing hit and answer in a single call.

Quality is not sacrificed for cheapness: answers cite `file:line`, and a
cheaper-but-wrong answer is a loss, not a saving — inspect the `answer` field
in each record and judge for yourself.
