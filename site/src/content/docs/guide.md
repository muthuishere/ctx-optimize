---
title: How to use it
description: What the field claims versus what holds, then how to onboard a repo, work as a developer, read it as an architect, and what to say when Claude greps anyway.
---

## What they claim, and what it is

The category (Graphify, CodeGraph, GitNexus, Serena, rtk, and the rest) repeats the same
lines. We measured some of them. Independent session benches measured the others. None of
this is a 79× story.

| They say | What holds |
|---|---|
| **60–90% / 71× / 79× fewer tokens** | A 2026-08 session bench on 48 Django tasks: **nobody hit 60%**. Best full-session cut ~32% (someone else's tool). Graphify ~9%. [JetBrains](https://blog.jetbrains.com/ai/2026/07/rtk-claude-code-token-savings/) on rtk: advertised 60–90%, measured **+7.6% more expensive**. We measured Claude Code **−0.2%**, Codex **+3.0%**, and **killed** the universal token claim. What moved for us: **15 tool calls vs 43**, **67% vs 35%** correct on the graded mux run. |
| **Install MCP and the agent will use the graph** | Same bench, same servers: Codex called every tool every time. Claude Code called Graphify **3/15** and code-review-graph **0/15**. Claude loads MCP on demand and often never looks. We do **not** ship MCP. The store is a folder and a CLI. |
| **Always fresh** | A watcher or a server does not make that true if nobody calls it. Our default autosync is **off**. After you edit, `ctx-optimize sync` (no-change is ~0.25s). `"autosync": "lazy"` is opt-in. |
| **The pretty graph is the architecture** | A force-directed dump of every symbol is a hairball. Flow is a **sample**: a card is a directory, an arrow is N real edges, the plates are ports. The footer says what it hid. |
| **This replaces grep** | No. Exact strings, comments, config **values** stay grep. The store answers structure: callers, blast radius, what this talks to. |

[What we do not claim](/ctx-optimize/limits/) · [Compared with the field](/ctx-optimize/compare/)

## Onboard a repo

```bash
cd your-repo
ctx-optimize up
```

`up` writes `.ctxoptimize/` if needed (commit that), gathers into
`~/ctxoptimize/<name>/`, and is a no-op when the store is already fresh. Monorepos: it
scans modules; curate `config.json` if the list is wrong, then `up` again.

Optional, for the team: `ctx-optimize init` writes the pointer block into `AGENTS.md` /
`CLAUDE.md`. Optional, for your agent CLI: `ctx-optimize install --claude` (or
`--codex` / `--copilot` / `--devin`). Grok already reads the Claude skill directory.

There is no API key and no server.

## As a developer

Pick the verb by intent. Do not default to `query` for everything.

```bash
ctx-optimize query "refund tax"          # find — 2–4 words
ctx-optimize card Store.Merge            # inspect — signature, callers, callees
ctx-optimize change-plan Store.Merge     # about to edit — callers, blast, tests
ctx-optimize affected Store.Merge        # blast radius only
ctx-optimize path A B                    # how A connects to B
ctx-optimize boundaries                  # what this talks to
ctx-optimize sync                        # you edited — refresh the store
```

Cite the `file:line` it returns. Open the file only for a body the store did not show.
Ambiguous callers are a shortlist, not a guess — if `card` says `unattributed callers`,
believe that line.

## As an architect

```bash
ctx-optimize serve
# Viewer → Flow — derived architecture
```

Same store as the CLI. A card is a directory. An arrow is real `imports`/`calls`, summed.
The hub is whatever everything depends on. The dashed plates are
[`boundaries`](/ctx-optimize/boundaries/) — hosts, env **names**, spawned binaries, never
a value. Click a card to drill into files, then declarations.

The picture does not grade the module. A 241-in hub is a number you can point at in a
review. [How to read it →](/ctx-optimize/see/)

## If Claude does not use it

That is the common failure in this category, including ours. `CLAUDE.md` is not a
contract. We do not block Grep.

**Say this, once:**

```text
Use ctx-optimize for searching and all structural work — query, card, change-plan,
affected, path, boundaries. Grep only for exact literal strings. Cite the file:line
it returns.
```

Then watch the tool log. If the next turn still only Greps, say it again and name the
symbol: `run ctx-optimize change-plan <that name>`.

Do not install a hook that forbids Grep. Edge cases need it, and a blocked Grep is how
the other tools became “spammy.”

You can also run the verb yourself in the same repo. The store does not care who typed
it. After you edit, `sync` — the agent will not notice a stale graph on its own unless
you opted into lazy autosync.
