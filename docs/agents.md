# Agent integration — skills, hooks, instruction cards, small models

ctx-optimize is built to be driven by coding agents (Claude Code, Codex,
Copilot, Devin, or your own runtime). Three layers make an agent actually
use the store unprompted — install once, commit once, done.

## 1. `ctx-optimize install` — the machine-level surface

```sh
ctx-optimize install            # every agent CLI it detects
ctx-optimize install --claude --skills   # narrow by platform / scope
```

Installs, per platform:

- **The skill** (`~/.claude/skills/ctx-optimize/`, `~/.agents/skills/…`):
  the full playbook — the shell-command rule (it's a CLI, not a callable
  tool), the pick-by-intent router (`query` vs `card` vs `change-plan` vs
  `affected`), store-first discipline, the sources and adapters and
  push/pull references.
- **The hook** (where the platform supports one): injects a store reminder
  at prompt time and gates on `fresh` so agents don't answer from a stale
  snapshot.
- **The global rule** (`~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`): the
  standing "knowledge graph before grep" block — self-gating on
  `command -v ctx-optimize`, so it's inert on machines without the tool.

`ctx-optimize update` refreshes all of it from the current binary; 
`uninstall` removes exactly what install wrote.

## 2. The committed repo surface — works for every teammate's agent

`init` (or `up`'s bootstrap) writes two things you commit:

- **Pointer blocks** in `CLAUDE.md` / `AGENTS.md` (whichever the repo
  already has; both created if neither exists; `--instructions NONE` to opt
  out). Marker-fenced, idempotent, self-gating — this one block is the
  mechanism measured to make agents use the store unprompted.
- **`.ctxoptimize/instructions.md`** — the full usage card: verify
  discipline, store-vs-grep ladder, sources, remote push/pull. It carries a
  version-stamped managed block that `init`/`up` refresh upgrade-only;
  **your own text outside the markers is never touched** — add repo-specific
  notes there and every agent reads them.

## 3. Small models & custom runtimes

Frontier agents follow the skill. Small models (gpt-4o-mini class) need the
protocol **pinned in the system prompt** — without it they answer from
priors. The measured-good prompt ships in `.ctxoptimize/instructions.md`
under "Small models & custom runtimes": query first via shell, answer only
from tool output, cite `file:line`, say "not found" after two empty
queries. Measured on a linux-kernel store (8-question bench, blind-judged):
23/80 without the protocol → 54/80 with it — ~70% of frontier quality at
~1/100th the cost. One-shot per question beats a continuous conversation
(same score, 7× cheaper, no cross-question bleed).

Full numbers: [`benchmarks/agent-model-bench/`](../benchmarks/agent-model-bench/).

## The agent-facing verbs, in one breath

`query` (find) · `card` (inspect, no file read) · `change-plan` (about to
edit: callers + blast radius + which tests) · `affected` (impact) · `path` ·
`verify` (check a citation before a human acts on it) · `fresh` (exit-code
gate: 0 fresh / 1 stale / 2 unknown). Everything `--json`.

## What agents must never do

- Call a tool named `ctx_optimize` — no such tool exists; it's a shell
  command.
- Print or store secret values — sources take env-var NAMES; all output is
  scrubbed.
- Expect the binary to think — it's deterministic; the agent supplies all
  semantics.
