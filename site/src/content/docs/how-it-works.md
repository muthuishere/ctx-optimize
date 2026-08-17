---
title: How it works
description: Tree-sitter in wasm, goldmark for docs, ndjson store, fail-safe card index, boundaries on the same walk.
---

Model and schema: [What it is](/ctx-optimize/concepts/). This page is the machine.

## Parse

Tree-sitter grammars compiled to WASI, hosted by [wazero](https://github.com/tetratelabs/wazero). `CGO_ENABLED=0`. One instance per worker. Embedded: Go, Python, JS, TS/TSX, Java, C, C++, C#, Rust, Zig, SQL. Anything else is a pack (`<name>.wasm` + `<name>.json`) in `~/ctxoptimize/grammars/` or `.ctxoptimize/grammars/`. `languages add` builds one; Zig is fetched once, sha256-checked.

Kubernetes gather: **58% of time is tree-sitter inside wasm**. The remaining lever is parsing fewer files. `GOMAXPROCS` caps workers (container-aware). That bounds wasm, not the graph — linux (~2.85M nodes) still wants ~14 GB.

## Markdown

goldmark AST. Headings are sections. Fences are not. A link is an edge only if the target resolved in the walk. The old line-regex minted **618 phantom sections** (a `#` inside a bash fence). 10 ms → 24 ms on this repo’s 3.5 MB of docs.

## Store

```text
~/ctxoptimize/<repo>/graph/{nodes,edges}.ndjson   # sorted, atomic rename
                  manifest.json                   # sha256 per file
```

`add` uses producer-scoped `Replace` (re-parse code, keep your adapter). Shrink &gt;50% refused without `--force`. `Merge` (`--json`) never steals another producer’s stamp.

## `card` index

`graph/index/{labels,ids,edges-by-source,edges-by-target}.idx` — sorted text, binary search + `ReadAt`. Kernel `card` **&lt;20 ms**. Header is size+mtime of the graph; any mismatch falls back to a full scan. `query` does **not** use this index. [How query scores](/ctx-optimize/concepts/#why-query-is-not-grep--and-not-a-vector-store).

## Confidence

| | |
|---|---|
| EXTRACTED | parsed |
| INFERRED | name-matched |
| AMBIGUOUS | shortlist; out of `affected` / `path` / `change-plan` unless `--include-ambiguous` |

Blast radius is a floor. `?` rows are candidates, not callers.

## Boundaries

Rules are JSON over AST shapes, evaluated on the **same walk** that builds `calls`. TypeScript repo: 12.4 s regex → **0.06 s**. A variable argv is visible as AMBIGUOUS, not missing. [Boundaries](/ctx-optimize/boundaries/).

## Flow

`GET /api/scene` derives the picture in Go. 7,663 nodes → **4.4 KB**. [See](/ctx-optimize/see/).

No LLM, no embeddings, no MCP, no credentials at rest. Network only when you ask (`update`, grammar, remote script).
