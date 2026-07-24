# Commands & AI-agent reach — ctx-optimize vs the field

> Honesty tags: [MEASURED] = ran the CLI here · [FROM DOCS] = tool's own docs/`--help` ·
> [QUALITATIVE] = judgement. Agent-host lists move fast — re-verify before quoting.
> ctx-optimize verbs are [MEASURED] (`ctx-optimize --help` / `install --help`, 2026-07-24).

## 1. Verb-for-verb — same job, each tool's command

| Capability | ctx-optimize | CodeGraph | GitNexus | graphify | Serena | ast-grep |
|---|---|---|---|---|---|---|
| Build/index | `add` / `up` | `init` | `analyze` | `update` / `extract` | — (LSP, no build) | — (no index) |
| Find (search) | `query` | `query` | `query` | `query` | `search_for_pattern` | `run -p <pat>` |
| Inspect a symbol | `card` | `node` | `context` | — | `find_symbol` | — |
| Who calls it | `card` / `change-plan` | `callers` | `context` | — | `find_referencing_symbols` | — |
| Impact / blast radius | `affected` / `change-plan` | `impact` | `impact` | `affected` | — | — |
| Path between two | `path` | — | `trace` | `path` | — | — |
| List/filter by kind or relation (no jq) | `nodes` / `edges` / `deps` | — | `cypher` (raw) | — | — | `scan` (rules) |
| Explain a region | `explain` | `explore` | — | `explain` | — | — |
| Freshness / re-sync | `fresh` / `sync` / `up` (+ autosync, in progress) | `sync` / auto | `detect-changes` / sync | (static dump) | live (LSP) | — |
| Dashboard / serve | `serve` | VS Code extension | — | — | — | — |
| One "about to edit X" call | `change-plan` (callers+impact+tests, one shot) | — (compose) | — (compose) | — | — | — |

Reading: ctx-optimize and CodeGraph/GitNexus cover the same core verbs. ctx-optimize's
distinct surface is the **generic `nodes`/`edges`/`deps` filter** (structured, no jq,
all modules — v0.8.0) and the **composed `change-plan`** (one call = callers + blast
radius + tests). Serena is symbol-ops only (no build/index/path). ast-grep is
structural search/lint only (no symbol graph). [MEASURED for ctx; FROM DOCS otherwise]

## 2. AI-agent reach — how each tool gets INTO the agent

| Tool | Mechanism | Agent hosts it installs into |
|---|---|---|
| **ctx-optimize** | **agent skill + hook + committed CLAUDE.md/AGENTS.md pointer** (no MCP, by choice) | Claude Code, Codex, Copilot, Devin (skill+hook); OpenCode + any AGENTS.md/CLAUDE.md reader via the committed pointer [MEASURED: `install --help`] |
| **CodeGraph** | **MCP (42 tools)** + VS Code extension + skill | Claude Code, Codex, Gemini, Cursor, OpenCode, AntiGravity, Kiro, Hermes (~8 via MCP) [FROM DOCS] |
| **GitNexus** | **MCP (16 tools)** + skills + hooks | Cursor, Claude Code, Antigravity, Codex, Windsurf, Cline, OpenCode [FROM DOCS] |
| **graphify** | **per-agent skill/pointer installers** (widest list) | claude, codex, cursor, gemini, copilot, opencode, codebuddy, kilo, kiro, trae, windsurf, aider, amp, antigravity, devin, droid, hermes, pi, vscode [FROM DOCS: graphify `--help`] |
| **Serena** | **MCP server** | any MCP host [FROM DOCS] |
| **ast-grep** | CLI (+ community MCP) | any MCP host / manual [FROM DOCS] |

## 3. The honest read (this is the deliberate tradeoff, not an oversight)

- **Breadth**: graphify (≈19 hosts) and the MCP tools (CodeGraph 8, GitNexus 7)
  reach MORE hosts than ctx-optimize's 4 native + pointer. On an MCP-only host
  (Cursor, Kiro, Gemini CLI) you wire ctx-optimize's CLI in yourself — there is
  no MCP server, by choice, and won't be. **This is a real ctx-optimize weakness
  on raw reach.** [QUALITATIVE]
- **Depth on the hosts we target**: a *skill + hook* is richer than a fixed MCP
  tool list — it teaches the agent WHEN and HOW to use the store (query-craft,
  the "graph before grep" discipline), auto-fires via the hook, and the committed
  pointer means the whole team's agents inherit it from one commit. An MCP tool
  is a menu; a skill is a playbook. [QUALITATIVE — this is the "skill over MCP" bet]
- **Verb parity holds**: for the core jobs (index/find/inspect/callers/impact/
  path) ctx-optimize matches or exceeds the field; the two things only we ship
  are the generic `nodes`/`edges`/`deps` filter and the one-shot `change-plan`.

## Sources (re-verify — moves fast)
- ctx-optimize: `ctx-optimize --help`, `ctx-optimize install --help` [MEASURED 2026-07-24]
- graphify: `graphify --help` verb + install list [FROM DOCS, local 0.9.12]
- CodeGraph: github.com/colbymchenry/codegraph, github.com/codegraph-ai/CodeGraph
- GitNexus: github.com/abhigyanpatwari/GitNexus
- Serena: github.com/oraios/serena
- ast-grep: ast-grep.github.io
