# Cross-tool benchmark harness — methodology

> **Honesty contract (borrowed from the toolnexus benchmark doctrine).**
> Every number this harness publishes is tagged **[MEASURED]** — produced on the
> machine below by the committed runner. Competitor capability facts pulled from a
> project's own docs are **[FROM DOCS]** with a link. A judgement call is
> **[QUALITATIVE]**. Where a tool cannot be stood up to do the *same* work
> honestly, we **omit its row and say why** — a partial-but-real table beats a
> complete-but-fake one. We never invent a head-to-head we did not run.

This is the cross-tool companion to the graphify head-to-head already published
on the site (`../../bench/results.json`). graphify was the first rival run to a
full measured harness; this directory extends that to the rest of the field.

## The field, and what we can honestly measure

| Tool | What it is | Install | In this harness? |
|---|---|---|---|
| **ctx-optimize** | single static Go binary, code+routes+deps+k8s graph | one binary | baseline |
| **graphify** | Python central-store graph | pip | ✅ measured (see `../../bench/`) |
| **CodeGraph** | local code graph, SQLite + MCP, auto-sync | npm / npx | ✅ **cold-gather + footprint** |
| **GitNexus** | client-side graph, MCP + CLI (`npx gitnexus analyze`) | npx | ✅ **cold-gather + footprint** |
| **Serena** | language-server-as-MCP (no persistent graph) | uvx | ⚠️ **different shape** — no cold-gather step to time; we report symbol-op latency separately and do NOT put it in the gather table |
| **potpie** | funded platform, **Neo4j service** required | containers + DB | ❌ **omitted** — a DB-backed service cannot produce a fair single-machine cold-gather row; capability facts stay [FROM DOCS] on the compare page |

## What we measure (the same three axes as the graphify run)

1. **Cold gather** — time from "nothing indexed" to "graph ready to answer", on
   identical corpora, warm FS cache, wall-clock, best of N. Each tool timed on
   **its own fastest deterministic path** (no LLM/labeling step), and that path
   is named in the result row.
2. **Footprint** — on-disk store size after gather, and process/runtime
   dependencies required (services, DB, model key).
3. **Query latency** — median of N runs of a fixed question at a fixed budget,
   invoked cold from the shell, where the tool exposes a comparable query verb.
   Tools whose only query surface is an MCP server (no CLI query) are marked
   [QUALITATIVE] here, not timed, because we won't compare a CLI to a socket.

## Corpora (shared, pinned)

The same corpora as the graphify run, so rows are comparable:
`corpus-flask` (265 files), `corpus-gin` (159), `corpus-ctx-src` (75), and the
large `graphify-source` (12,484). Each pinned to a commit in the runner.

## Reproduce

`run.sh` installs each tool at a pinned version, runs the three axes per corpus,
and writes `results.json` beside this file. Versions and machine are stamped
into the output. Status: **scaffold — CodeGraph + GitNexus runs pending.** No
row is published to the site until it is produced here.

## Sources (competitor facts, [FROM DOCS])

- CodeGraph — https://github.com/colbymchenry/codegraph , https://github.com/codegraph-ai/CodeGraph
- GitNexus — https://github.com/abhigyanpatwari/GitNexus
- Serena — https://github.com/oraios/serena
- potpie — https://github.com/potpie-ai/potpie
