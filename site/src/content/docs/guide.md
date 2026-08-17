---
title: How to use it
description: Onboard a repo, pick the verb, read Flow, and what to say if Claude greps anyway.
---

Field claims vs numbers live on [compare](/ctx-optimize/compare/) and [limits](/ctx-optimize/limits/). This page is the work.

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

Cite the `file:line`. Open the file only for a body the store did not show. If `card`
says `unattributed callers`, believe that.

## As an architect

```bash
ctx-optimize serve
# Viewer → Flow
```

Same store. A card is a directory, an arrow is real edges, plates are
[`boundaries`](/ctx-optimize/boundaries/) (names, never values). [How to read it](/ctx-optimize/see/).

## If Claude does not use it

`CLAUDE.md` is not a contract. We do not block Grep.

```text
Use ctx-optimize for searching and all structural work — query, card, change-plan,
affected, path, boundaries. Grep only for exact literal strings. Cite the file:line.
```

If the next turn still only Greps: `run ctx-optimize change-plan <that name>`. Do not
forbid Grep. After you edit, `sync`.
