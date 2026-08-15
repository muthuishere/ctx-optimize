---
title: Quick start
description: "Install the binary, gather a repo, and ask the first question — three commands, no service to run."
---

## Install

```bash
npm install -g @muthuishere/ctx-optimize   # one static binary per platform
```

Or `go install github.com/muthuishere/ctx-optimize/cmd/ctx-optimize@latest`, or download a
binary from [Releases](https://github.com/muthuishere/ctx-optimize/releases). There is no
service, no daemon, no database and no API key.

## Gather

```bash
cd your-repo
ctx-optimize up
```

`up` is the one verb that always does the right thing: bootstrap a config if there is none
(scanning for modules in a monorepo), gather the graph, or pull the team's prebuilt store
when the config declares a remote. It is a safe no-op ever after.

The store lands in `~/ctxoptimize/<repo-name>/` — outside your repo, one per machine. The
only thing written **into** your repo is `.ctxoptimize/` (config, instructions card,
optional adapters), which is meant to be committed.

## Teach your agent

```bash
ctx-optimize install --skills     # Claude Code + Codex
```

This installs the agent skill and writes a pointer block into `AGENTS.md` / `CLAUDE.md`, so
an agent reaches for the store without being told. The full usage card is committed at
`.ctxoptimize/instructions.md` — teammates inherit it with zero installation.

## Ask

```bash
ctx-optimize query "store merge producer"   # find: ranked hits with file:line
ctx-optimize card Store.Merge               # inspect: signature, doc, callers, callees
ctx-optimize change-plan Store.Merge        # about to edit: callers + blast radius + tests
ctx-optimize affected Store.Merge           # blast radius only
ctx-optimize boundaries                     # what this system talks to
```

Every answer carries an exact `file:line`. Pick the verb by intent rather than reaching for
`query` every time — the [cookbook](/ctx-optimize/cookbook/) maps the question you would ask
a teammate to the one command that answers it.

## Keep it current

```bash
ctx-optimize sync      # incremental resync; a no-change run is milliseconds
```

Or set `"autosync": "lazy"` in `.ctxoptimize/config.json` and a stale read resyncs itself in
the background.
