# ctx-optimize docs

Task-shaped guides — each one answers "when do I need this and why does it
work that way". Start with the [main README](../README.md) for install and
the 60-second tour.

| Doc | Read it when |
|---|---|
| [cli.md](cli.md) | you want the full verb reference — every command, when to use it, why that one |
| [monorepos.md](monorepos.md) | your repo has many modules — per-module stores, the navigator, scope rules, how `up` reconciles the declared module set |
| [remote-github.md](remote-github.md) | you want the team to share one prebuilt store — GitHub repo as the remote (recommended), S3, or any script |
| [sources.md](sources.md) | a database / bucket / queue / external API should be part of the graph — the env-var-URL contract, credentials, the resolution ladder |
| [adapters.md](adapters.md) | something has no native connector — drop-in scripts, the `--json` door, composing with `capture` |
| [agents.md](agents.md) | wiring coding agents (Claude Code, Codex, Copilot, Devin, custom runtimes) — skills, pointer blocks, the committed instructions card, small-model protocol |
| [languages-packs.md](languages-packs.md) | your language/framework/build tool isn't extracted — grammar packs, route packs, manifest packs, all drop-in |
| [cookbook.md](cookbook.md) | you want it SHOWN — 32 executed-and-verified scenarios with real output, from first `up` to guards firing (reproduce: `proof/scenarios/run.sh`) |
| [troubleshooting.md](troubleshooting.md) | **the tool refused or warned you** — every guard message verbatim, its cause, and the one command |
| [VISION.md](VISION.md) | the long-term design position |
| [CRITIQUE.md](CRITIQUE.md) | the standing counter-argument we keep honest against |

Design decisions live in [`openspec/`](../openspec/) — every behavior above
traces to an ADR with measurements.
