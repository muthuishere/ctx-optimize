---
title: Compared with other tools
description: "Where we stand against CodeGraph, GitNexus, Graphify, Serena, potpie, and ripgrep — including the columns we lose."
---

[CodeGraph](https://github.com/colbymchenry/codegraph) · [GitNexus](https://github.com/abhigyanpatwari/GitNexus) · [Graphify](https://graphify.com/) · [Serena](https://github.com/oraios/serena) · [potpie](https://github.com/potpie-ai/potpie) · ripgrep. We are CLI + skill, no server, no model in the gather.

## The board

| | ctx-optimize | CodeGraph | GitNexus | Graphify | Serena | potpie | ripgrep |
|---|---|---|---|---|---|---|---|
| What it is | Go CLI + skill | SQLite + MCP | MCP, 16 tools | Python skill | LSP over MCP | Neo4j + agents | line search |
| Model in the gather | no | no | no | labeling | no | LLM in the loop | no |
| MCP | **no** — folder + CLI | 42 tools | 16 tools | no | yes | API | no |
| License | MIT | MIT | noncommercial | open | MIT | open-core | MIT |
| Linux kernel gather | **118 s** · 2.85M nodes | 290 s | **did not finish** (45 min) | 528 s; cannot query at default cap | — | — | n/a |
| Kernel query | 3.70 s · **4 / 5 useful** | **0.79 s** · **0 / 5 useful** | — | 23 s · 0 / 5 | — | — | 1.59 s · lines, not symbols |
| Small corpora (pin-verified) | **leads** gather, warm, query, disk | 2nd | slowest / huge | middle | — | — | fastest *lines* |
| Graded agent (mux, mini) | **67% · 15 calls** | — | — | 40% · 26 calls | — | — | 35% · 43 calls |
| Session tokens (frontier) | parity (−0.2% / +3.0%) | — | — | — | — | — | — |
| Outer surface (env, hosts, bins) | **`boundaries`** | no | no | no | no | no | strings only |
| We have not run | | | | | no row | no row | |

Empty cells are un-run. Kernel gather/query is the 2026-08 arena (unpinned — re-run before treating it as a headline). Small-corpus run is pin-verified 2026-08-15 (CodeGraph `572d22bf`, Graphify `2fa6cd3d`, GitNexus `91b22676`). Graded run: gorilla/mux, 12 questions × 3, n = 36, no LLM judge.

CodeGraph: faster kernel query, 0/5 useful on that set. Graphify: cannot query linux at the default 512 MB cap. GitNexus: DNF 45 min. ripgrep: faster strings; cannot name a caller. Tokens: nobody hit 60% in the 48-task bench; we are parity on Claude/Codex. MCP: Claude called Graphify 3/15. Transcripts: [RESULTS-QUALITY.md](https://github.com/muthuishere/ctx-optimize/blob/main/proof/agent/RESULTS-QUALITY.md).

## Who to pick

| If you want | Take |
|---|---|
| MCP on every host today | CodeGraph |
| Deepest Claude MCP (noncommercial) | GitNexus |
| Type-exact rename | Serena (LSP) — more precise than any static graph, including ours |
| A funded Neo4j platform | potpie |
| A string | ripgrep |
| One binary, no server, no model, cited `file:line`, and what the system talks to | **us** |

The agent may still grep. We do not block it. Numbers and harnesses: [benchmarks](/ctx-optimize/benchmarks/). What we will not say: [limits](/ctx-optimize/limits/).
