---
title: Use cases
description: "What you actually get: onboarding, safe refactoring, impact analysis, monorepos, sources and team sharing."
---

Five situations where a knowledge graph changes what an engineer or an agent can do — framed by the outcome, not the mechanism. Each one is honest about where it stops helping.

## Onboarding a new repo — yours, or a teammate's.

The first hour in an unfamiliar codebase is the most expensive hour an agent (or a new hire) spends — grep the obvious names, open the wrong file, guess at the architecture from folder names. The outcome you want isn't "a graph exists" — it's **being able to ask a real question and get a cited answer in the first minute**.

### What you do

```text
cd unfamiliar-repo
ctx-optimize up                    # no config → bootstraps + gathers; existing config → pulls or refreshes
ctx-optimize hubs --top 10         # the load-bearing nodes — usually the real entry points
ctx-optimize query "how does auth work"
```

### What you get

A queryable map of the repo before you've read a single file end to end — signatures, doc comments, callers, and (if the repo has them) route bindings and dependency edges, all cited to `file:line`. The generated `wiki/` gives a second way in: a readable, regenerated-on-every-gather markdown map that names your symbols instead of showing raw graph dumps. If a teammate already gathered and shared the store ([remote pull](#sources)), `up` gets you their graph instead of re-doing the work.

:::note
**Where it stops helping.** The graph tells you what's connected to what — it doesn't explain *why* a design decision was made, and it won't replace reading the code you're about to change. It's the map, not the terrain. See [the benchmarks](/ctx-optimize/compare/) for the measured head-to-head, including where other tools win.
:::

## Safe refactoring — `change-plan` and `affected`.

The dangerous part of a refactor isn't writing the new code — it's not knowing everything that depends on the old shape. The outcome you want is a single, trustworthy answer to "if I change this, what do I have to also touch or re-test" — before you touch anything.

### What you do

```text
ctx-optimize change-plan validateRefund
```

### What you get

One composed call — the signature, every caller, the blast radius to a bounded depth, **which tests to run** (affected nodes filtered down to test declarations), and historical co-change data pulled from git log, all in one answer instead of a query→card→affected→grep chain. Confidence is split honestly: extracted (parsed fact) vs inferred (name-matched) evidence, so you know which lines to double-check by hand and which you can act on directly (see [EXTRACTED vs INFERRED](/ctx-optimize/concepts/)). **One composed call in place of a query → card → affected → grep chain.** We do not claim this saves you tokens — that claim was measured and killed ([CRITIQUE.md](https://github.com/muthuishere/ctx-optimize/blob/main/docs/CRITIQUE.md)). What was measured in the graded run is **tool calls per run: 15.0 with the store against 42.7 with a shell** ([the numbers](/ctx-optimize/#quality)).

Want just the blast radius, no test/caller framing? `ctx-optimize affected X --depth N` is the narrower verb underneath.

:::note
**Where it stops helping.** Ambiguous call sites are *dropped*, not guessed — so a rename that changes call resolution (e.g. dynamic dispatch, reflection, string-built symbol names) can undercount blast radius. Confidence-mark every answer, and for anything load-bearing, cross-check the "tests to run" list against your own judgment rather than trusting it blind.
:::

## Impact analysis — before a change, not after an incident.

"What breaks if I change this" isn't only a refactor question — it's the question a reviewer, an on-call engineer, or a security auditor asks about a proposed change, a flagged function, or a shared dependency. The outcome is the same reverse-dependency walk `change-plan` uses, available standalone and composable.

### What you do

```text
ctx-optimize affected PaymentService --depth 2
ctx-optimize path "cmdAdd" "Store.Merge"      # how do two symbols actually connect?
ctx-optimize query "dev dependency" --json    # pull dep: nodes into a script/CI check
```

### What you get

A reverse-impact walk — every node that reaches the one you named, to whatever depth you ask for — with the same EXTRACTED/INFERRED confidence split as `change-plan`. Because `dep:` nodes federate across every build tool in the repo (package.json, go.mod, pom.xml, csproj+sln, Gradle, Cargo), asking "what depends on this shared library" answers across module boundaries in a polyglot repo, not just within one language's manifest.

:::note
**Where it stops helping.** This is static analysis, not runtime tracing — feature flags, dependency injection resolved at runtime, and dynamically-loaded code aren't in the graph unless a producer explicitly models them. Treat "affected" as a strong prior for where to look, not a guarantee of completeness.
:::

## Monorepos — a graph that scales with the module, not the repo.

A monorepo makes "just gather the whole thing" actively wrong: one giant graph means every query is ranked against modules nobody asked about, and one edit invalidates the whole store. The outcome you want is a store that costs what the change costs — edit one service, refresh one service.

### What you do

```text
cd my-monorepo
ctx-optimize up                    # detects the monorepo, scans, writes config, fans out the gather
# — or, with review before it commits to a module list:
ctx-optimize scan                          # read-only preview — what WOULD be declared
ctx-optimize init --scan --yes && ctx-optimize add .   # curate config.json, then gather explicitly
```

### What you get

One store per declared module, plus a root **navigator** — not a merged mega-graph (see [how federation works](/ctx-optimize/concepts/)). Ask a question inside `services/billing` and it answers from billing's store alone; a miss escalates repo-wide and labels which module actually answered. Editing one service re-gathers in a second or two — not the whole tree. Measured on `apache/beam`: **310 modules discovered at depth 8, all gathered in 14.5s**, including maven modules nested inside other modules' resource trees. Adding a module to `config.json` and running `up` again gathers exactly that module — nothing else is touched, and a module removed from config is reported as an orphan, never silently deleted.

:::note
**Where it stops helping.** Cross-module answers go through the navigator's ranking, which is a step slower and coarser than a single-module query — for a genuinely cross-cutting question, `merge api worker --into everything` gives you one combined (but now-stale-the-moment-either-side-changes) view. Source/test splits across separate directories need an explicit multi-path module declaration or calls between them won't resolve.
:::

## Indexing docs and a live database schema — via sources.

A lot of "what does this system do" questions have their real answer outside the repo: a table your code reads but never declares in an ORM model, a Kafka topic, an OpenAPI contract another team owns. The outcome is asking a code question and a schema question in the same breath, with one citation format for both.

### Docs — built in, no setup

Markdown in the repo is gathered natively on every `add`/`sync` — headings become sections, nested by level, indexed the same way as code. No adapter, no flag.

### Database, bucket, queue, or API — one env var

```text
ctx-optimize adapters help postgres     # the setup card: value format, credential params, a paste-ready command
export BILLING_DB_URL='postgres://reader:$PG_PASS@db.internal:5432/billing'
ctx-optimize add BILLING_DB_URL         # resolve → dial → capture → merge → recorded in config sources
```

Nine wire-protocol connectors — postgres, mysql, mongodb, redis, kafka, nats, s3, mssql, openapi — capture the **logical shape** a developer actually reasons about: system schemas skipped, a partitioned table collapses to one node with a `partitions: N` fact, samples bounded, every cap reported. Credentials are structural, not a policy you have to remember: argv and committed config carry env-var **names** only — a literal password in a committed entry is a hard error at load — and the resolution ladder (process env → repo-root `.env` → machine-global `~/.config/ctx-optimize/.env`) means a teammate without the credential still runs `up` cleanly (one skip line, prior nodes stay) instead of failing the whole gather.

### Team sharing — after the graph exists

```text
ctx-optimize remote push    # runs YOUR declared script — a git repo, S3, anything
# teammate:
ctx-optimize up             # pulls the prebuilt store instead of dialing the DB themselves
```

Only the person with credentials needs to dial the database. Once captured, the schema travels with the rest of the store through whatever `remote push`/`pull` script your team declared — nobody else needs the connection string.

:::note
**Where it stops helping.** Sources refresh on a 24h TTL by default (`--sources=always` forces a re-capture) — this is a periodically-refreshed snapshot of external state, not a live query against production. Treat schema answers as "true as of the last capture," and for anything time-sensitive, dial the source directly instead of trusting the graph.
:::

**[](/ctx-optimize/)[](/ctx-optimize/concepts/)[](/ctx-optimize/cookbook/)[](/ctx-optimize/cli/)[](https://github.com/muthuishere/ctx-optimize)[](https://github.com/muthuishere)
