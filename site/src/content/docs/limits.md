---
title: What we do not claim
description: "The sentences we will not print. Each one died on a measurement, or was never ours."
---

## We do not save you 79× tokens

The category’s number. Independent 2026-08 session bench: **nobody hit 60%**. JetBrains on rtk: advertised 60–90%, measured **more expensive**. Us: **−0.2%** Claude Code, **+3.0%** Codex. A thin mini harness did cut tokens (−36%, 4 questions). That is not “saves tokens.” What moves every time is **15 tool calls vs 43**.

## We are not the fastest query at kernel scale

CodeGraph answers in **0.79 s**, ripgrep in **1.59 s**, we in **3.70 s**. They get the line (or a type name) sooner. We get the **symbol**. On gather we are ahead of those graphs (118 s vs 290 s vs 528 s; GitNexus did not finish). “Fastest code graph” is not a sentence we use.

## We do not replace grep

Exact strings, comments, config **values** stay ripgrep. Locate questions: grep **47%**, us **42%**. The store is callers, blast, what this talks to.

## The agent will not always call us

Claude often never looks at an MCP graph (Graphify 3/15, code-review-graph 0/15 on the same bench Codex used every time). We do not ship MCP. We do not block Grep. If it ignores the store, [say this](/ctx-optimize/guide/#if-claude-does-not-use-it).

## The store is not always fresh

Autosync default is **off**. A watcher you never invoke is not freshness. After you edit: `ctx-optimize sync` (no-change ~0.25 s). `"autosync": "lazy"` is a key, not the default.

## We do not invent callers

A name declared more than once is **AMBIGUOUS** — a shortlist, filtered out of `affected` / `change-plan` / `path` unless you ask. Blast radius is a **floor**. `Match` eight times in mux returns the file, not a guessed caller.

## We have not beaten Serena or potpie on a clock

No row. Empty stays empty. Serena’s LSP edges are more precise than our name-resolve. That is their column.

## `query` is not a concept engine and not embeddings

“What does it shell out to” is `boundaries`, not a smarter embedding. Ranking sees **label + path**, not doc bodies. Neighbours are shown, not re-scored. The next retrieval ADR does not add a vector index.

Standing counter-weight in the repo: [CRITIQUE.md](https://github.com/muthuishere/ctx-optimize/blob/main/docs/CRITIQUE.md).
