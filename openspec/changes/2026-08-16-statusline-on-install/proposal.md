# ADR 23 — a statusline verb the harnesses that have one can call

Status: DRAFT — owner: "create an adr for it we will come back."
Date: 2026-08-16
Scope: a new read verb `statusline`, and optional vacant-only wiring from
`install --claude` / `--copilot`. No MCP. No schema change. No store format
change. No product code until this is accepted.

Raised in the 2026-08-16 site huddle after the field research (Graphify /
CodeGraph / GitNexus / Serena / rtk) and the lock that we are **never MCP**
because the local CLI is the fresh, fast path.

## The ask

> "whatever supports cli status line add that as part of installing from any
> agent if they install for claude or codex or copilot … how much used"

The human cannot see whether the agent is using the store. Reddit's dominant
complaint about the whole category is that the tool is installed and never
called. A bar that says `12 served` or `0 served` is the honest answer to
that, without a daemon.

## What the harnesses actually have (checked 2026-08-16)

| Harness | Skill today | Command-backed statusline? | Install today |
|---|---|---|---|
| Claude Code | `~/.claude/skills` | **Yes** — `statusLine.command` in `~/.claude/settings.json` | skill + UserPromptSubmit hook |
| Copilot CLI | `~/.agents/skills` | **Yes** — same shape, `experimental: true` | skill + sessionStart hook |
| Codex | via `AGENTS.md` | **No** — `/statusline` is their quota bar; command-backed is [openai/codex#20140](https://github.com/openai/codex/issues/20140) | skill pointer + hooks.json |
| Grok | already reads `~/.claude/skills` | **No** — built-in line is only “N commands still running”; usage is OTEL | none (`--grok` does not exist; `--claude` covers the skill) |
| Devin CLI | `~/.agents/skills` + Claude hook, natively | **No** — “status line” in their changelog is MCP-get / handoff spinner | `--devin` writes no extra file on purpose |

This machine already has a Claude `statusLine` (`ctx-guard`). Overwriting it
on `install` would delete a bar the owner chose.

## D0 — one verb, not five scripts

`ctx-optimize statusline`

- Resolves the store for cwd the same way `query` does.
- Prints **one line**, no header, exit 0.
- No store → print nothing, exit 0 (a statusline that yells is worse than
  none).
- No gather, no index, no network. Read `status --json` / usage summary /
  freshness already on disk. Budget: well under 50 ms on a laptop store.
- Not an autosync trigger (a footer must not spawn a child).

Proposed line, when a store exists:

```
ctx wkdemo · fresh · 12 served
```

Stale: `ctx wkdemo · stale — sync · 12 served`.
No events yet: `ctx wkdemo · fresh · 0 served`.

`--json` exists for tests and for a harness that wants to compose. The
default is the line.

## D1 — what the line must not say

`internal/usage.Summary` already computes `est_tokens_saved` from a
replaced-grep model. That model is the one `docs/CRITIQUE.md` killed for
public claims (Claude Code −0.2%, Codex +3.0%). **The bar does not print
saved tokens, saved USD, or a 79×.** Served count is a count of our own
ndjson events. Fresh/stale is the store's own gate. Both are facts.

## D2 — install wires only a vacant slot

On `install --claude` and `install --copilot` (and first-run, if that path
already writes hooks):

- If `statusLine` is **absent**, set

  ```json
  "statusLine": { "type": "command", "command": "ctx-optimize statusline" }
  ```

  Copilot also needs their `experimental` flag only if we are the ones
  creating the file; do not flip it on a file that already has other
  settings.
- If `statusLine` is **present**, leave it. Print one line:

  ```
  statusline: left yours; add `ctx-optimize statusline` to it, or see uninstall
  ```
- Surgical JSON merge, same class as `InstallClaudeHook`: existing settings
  preserved; second install is a no-op.
- Codex, Grok, Devin: **do not write a statusline key we cannot see.** The
  verb is documented; they can call it when they grow a command slot.
- `uninstall` removes **only** a `statusLine.command` that is exactly
  `ctx-optimize statusline`. Anyone else's command stays.

No `--grok`. Grok already loads the Claude skill. A `--grok` that recopies
the same files is theatre.

## D3 — this is not MCP

VISION 2026-07-11: **NO MCP**, firm. The 2026-08-16 huddle restated the
why: local CLI is the fresh, fast path; Claude often never loads MCP
schemas. A statusline that shells out to our own binary is the same shape
as the rest of the product, not a server.

## What this does not change

- Autosync default stays **off** until a separate call (asked, not decided).
- First-run skill install (ADR 19) is unchanged.
- Dashboard Overview can keep showing the usage summary; it is a different
  surface.

## Gates

- Hermetic: no store → empty stdout, exit 0; store with N events → line
  contains `N served`; `fresh` / `stale` matches `status`.
- Install: existing `statusLine` byte-identical after `install --claude`.
- Uninstall: our command gone; a foreign command untouched.
- `task ci`. The verb is not on the golden scoreboard (it is not a retrieval
  answer).

## Kill criterion

If the line cannot be produced without reading the whole graph (i.e. it
cannot stay a metrics + freshness read), this ADR stops. A 3-second footer
is a defect, not a feature.

## Open for the owner when we come back

1. Accept D0–D3 as written?
2. Is `12 served` enough, or do you also want today's count (`3 today`)?
3. Still vacant-only, even on a machine whose only statusline is ctx-guard?
