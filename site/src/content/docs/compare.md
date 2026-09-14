---
title: Compared with other tools
description: "Where ctx-optimize stands against CodeGraph, GitNexus, Graphify, Serena and potpie — including the columns it loses."
---

[CodeGraph](https://github.com/colbymchenry/codegraph) · [GitNexus](https://github.com/abhigyanpatwari/GitNexus) · [Graphify](https://graphify.com/) · [Serena](https://github.com/oraios/serena) · [potpie](https://github.com/potpie-ai/potpie).

ctx-optimize is a Go CLI plus an agent skill: no server, no model in the gather, no MCP.

## What each one is

| | ctx-optimize | CodeGraph | GitNexus | Graphify | Serena | potpie |
|---|---|---|---|---|---|---|
| Shape | Go CLI + skill | SQLite + MCP | MCP, 16 tools | Python skill | LSP over MCP | Neo4j + agents |
| Model in the gather | no | no | no | labeling | no | LLM in the loop |
| MCP | no — folder + CLI | 42 tools | 16 tools | no | yes | API |
| License | MIT | MIT | noncommercial | open | MIT | open-core |
| Outer surface (env, hosts, binaries, routes) | <span class="win">`boundaries`</span> | no | no | no | no | no |
| Add a language yourself | <span class="win">grammar URL</span> | no | no | no | via LSP | no |
| Lives outside your repo | <span class="win">yes</span> | yes | yes | no — `graphify-out/` | yes | server |

MCP tool counts and licences are as published by each project; we have not audited them.

## Where the numbers are

Serena and potpie have no rows below — we have not run them, and an empty cell stays empty.

### Linux kernel · v6.9 · 144,011 files <span class="prov">ᴬ</span>

| | gather | query |
|---|---|---|
| ctx-optimize | <span class="win">118.2 s</span> — 2.85M nodes, 5.54M edges | <span class="lose">4.11 s</span> |
| CodeGraph | 289.9 s | <span class="win">0.98 s</span> |
| Graphify | 527.7 s | 22.8 s — only after raising its 512 MB cap |
| GitNexus | did not finish within 45 min | — |

On that run we built the graph 2.5× faster, and we are the only tool that produces a complete kernel graph. CodeGraph answers **~4.2× faster than we do** at this scale: it seeks in SQLite, we deserialize the whole graph per invocation. (An earlier version of this page said 0.79 s and 7.5×. That came from a single-word query against everyone else's full phrase; re-measured fairly on 2026-08-16 it is 0.98 s and 4.2×. The original figure is left in the result file so the error stays visible.)

### Small corpora · 253–1,474 files <span class="prov">ᴮ</span>

ctx-optimize leads all four tools on cold gather, warm re-gather, query and disk.

### Big repos · 723–10,142 files <span class="prov">ᴬ</span>

Cold gather: we lead all six. **Warm re-gather: we lose all six to CodeGraph**, 10–31× — we re-gather where its `sync` is a true incremental. Full tables on [benchmarks](/ctx-optimize/benchmarks/).

### Graded agent · gorilla/mux · gpt-4o-mini <span class="prov">ᶜ</span>

| | ctx-optimize | Graphify |
|---|---|---|
| Correct | <span class="win">67%</span> | 40% |
| Tool calls per session | <span class="win">15.0</span> | 26.0 |

---

ᴬ **Unpinned.** Kernel and big-repo runs predate our pinning harness and were measured on ctx-optimize v0.8.0–v0.12.0 (HEAD is v0.15.2); single run, not best-of-3. Being re-run — the drift runs against us, since v0.14 roughly halved gather. **Our column in that run included wiki generation**, which left the default path in v0.12 — so it measures work the tool no longer does. Re-measured on HEAD, the same kernel gather is **61.2 s median** (60.13 / 61.17 / 63.24, byte-identical output). Competitors have not been re-run, so we state no new ratio until they are.
ᴮ **Pin-verified** 2026-08-15 (CodeGraph `572d22bf`, Graphify `2fa6cd3d`, GitNexus `91b22676`), but recorded at load average 8.77.
ᶜ 12 questions × 3 runs, n = 36, no LLM judge, one small repo. [Transcripts](https://github.com/muthuishere/ctx-optimize/blob/main/proof/agent/RESULTS-QUALITY.md).

## Who to pick

| If you want | Take |
|---|---|
| MCP on every host today | CodeGraph |
| Fastest queries on a very large repo | CodeGraph |
| Deepest Claude MCP (noncommercial) | GitNexus |
| Type-exact rename | Serena — LSP is more precise than any static graph, ours included |
| A funded Neo4j platform | potpie |
| An exact string | ripgrep — and your agent should still use it |
| One binary, no server, no model, cited `file:line`, and what your system talks to | <span class="win">ctx-optimize</span> |

What we will not claim: [limits](/ctx-optimize/limits/).
