# ADR 24 — never MCP: local is the freshness path

Status: DRAFT — owner, 2026-08-16 huddle: *"we are never mcp, our local will
be fresh and fast thats why we dont do mcp"* and *"we will do only docs here
for all others we will do an adr."*
Date: 2026-08-16
Scope: doctrine + docs framing. No MCP server. No product code in this
change. Site copy that cites this ADR may land on `docs/see-the-graph`.

## The lock

VISION 2026-07-11 already said **NO MCP**, firm. The huddle restated the
*why*, which `/compare` had inverted:

The field put the graph behind a **server** (CodeGraph 42 MCP tools,
GitNexus 16, Serena, potpie). A 2026-08 5-tool session bench on
r/ClaudeCode (261 runs): Codex called every tool every time; Claude Code
called Graphify 3/15, Serena 4/15, code-review-graph **0/15** — same
servers, same questions — because Claude loads MCP schemas on demand.

We put the graph in a **folder** and a **CLI**. `card` is milliseconds. A
no-change `sync` is ~0.25s. There is no daemon, no tool-list JSON in
context, no schema the harness can forget to load.

**No MCP is not a red cell.** It is the design. Local is how we stay
fresh and fast.

## What "fresh" means today (honest)

Lazy autosync is **off** by default (`internal/store/autosync.go`: absent
key → `off`). Freshness is `sync` / `up` / `add`, which are cheap, not a
watcher. Flipping the default to `lazy` is a **separate ADR**, not this
one. Do not write "always fresh" on the site until that lands.

## What this ADR is not

- Not an MCP server later. "Never" is the word.
- Not `--grok` / statusline / install changes. Those are ADR 23.
- Not a claim that the agent always calls us. Claude may still Grep. We
  do not block it.

## Docs consequence (this branch)

`/compare` must stop calling missing MCP "the most important red." The
line is: local CLI, no server, because that is the path that stays fast.
MCP-only hosts wire the binary themselves; that is accepted.

## Open

Whether `autosync: lazy` becomes the default so "local is fresh" needs
no config key. Asked in huddle; not decided.
