---
title: How it works
description: Tree-sitter compiled to WASI and hosted by wazero, a plain ndjson store with producer-scoped replace, a lookup index that fails safe, and boundary rules that ride the parser's existing walk.
---

This page is the machine, not the pitch. Every claim here is something you can check in the
repo — the [CHANGELOG](https://github.com/muthuishere/ctx-optimize/blob/main/CHANGELOG.md),
the ADRs under `openspec/changes/`, or the store on your own disk. For the data model — nodes,
edges, producers, what a "kind" is — read [What it is](/ctx-optimize/concepts/) first.

## Parsing: tree-sitter, compiled to WASI, hosted by wazero

Code extraction uses real tree-sitter grammars and real ASTs. It does not shell out to a
language server, and it does not link them with cgo. Each grammar is compiled ahead of time to
a WASI-target WebAssembly module, and a pure-Go WebAssembly runtime,
[wazero](https://github.com/tetratelabs/wazero), hosts it — one instance per worker goroutine,
fanned across the cores.

Why that shape:

- **One static binary.** `CGO_ENABLED=0`. Nothing to install, no toolchain on the user's
  machine, no per-language runtime.
- **It cross-compiles.** A grammar built once runs identically on every release target,
  including Windows, without a per-platform build of the parser.
- **New languages are data, not a fork.** 12 languages are embedded — Go, Python, JavaScript,
  TypeScript/TSX, Java, C, C++, C#, Rust, Zig, SQL. Anything else is a **grammar pack**: a
  `<name>.wasm` plus a `<name>.json` node-type mapping, dropped into `~/ctxoptimize/grammars/`
  (machine-wide) or `.ctxoptimize/grammars/` (travels with the repo). `ctx-optimize languages
  add <grammar>` builds one in pure Go; a Zig compiler is fetched once, sha256-verified against
  ziglang.org's index, and cached. A malformed pack fails loudly rather than silently
  extracting nothing.

**What it costs, measured.** A CPU profile of a Kubernetes gather (17.9k files) puts **58.1%
of the time in `runtime._ExternalCode` — that is tree-sitter parsing, inside wasm** — with a
further 15.6% in zeroing, roughly a quarter of which is wazero growing linear memory
(`openspec/changes/2026-08-14-parse-less/`). Guest instances start at 64 MB, and allocation
for a 2.5-second gather came to 8.3 GB, 71% of it wazero linear memory.

The honest reading of that profile: **the remaining lever is parsing fewer files, not tuning
Go.** The mutex and block profiles show no contention — the worker pool already accumulates
per-worker and merges serially, which is the shape lock-free structures exist to rescue you
from. There is nothing for `sync/atomic` to bite on. What Go-side tuning did buy is recorded:
a process-wide symbol-table cache took a 4-module gather from 12.39 GB to 11.00 GB (−11.2%).

Memory is capped by `GOMAXPROCS`, which is container-aware, so a pod with a 2-CPU quota on a
64-core host no longer spawns 63 wasm instances. That cap bounds wasm instances, not the graph
— on the full Linux kernel it makes almost no difference, because at 2.85M nodes the resident
cost *is* the graph. See [what we do not claim](/ctx-optimize/limits/).

## The store: plain sorted ndjson, atomically swapped

The graph is two newline-delimited JSON files plus a manifest.

```text
~/ctxoptimize/<repo>/
  graph/nodes.ndjson    # one JSON object per line, sorted by id
  graph/edges.ndjson    # same
  manifest.json         # sha256 + size per file — what changed since the last gather
```

Three properties follow from that, and each one is a design decision:

**Sorted, so a diff is a diff.** Lines are emitted in id order. Re-gathering a repo where one
function moved produces a diff of a few lines, not a rewritten blob. You can check this on your
own store: parse out every `id` and compare it to its own sorted order.

**Atomically renamed, so a reader never sees half a graph.** The writer writes a uniquely-named
temporary file and `os.Rename`s it into place (`internal/store/store.go`). A concurrent reader
sees either the old graph or the new one.

**Content-hashed, so freshness is cheap.** `manifest.json` records a sha256 and a byte size per
file, which is how the tool knows what changed without re-reading everything.

### Producer-scoped `Replace`

Every node and edge is stamped with the **producer** that emitted it — `code`, `markdown`,
`manifests`, `boundaries`, a database connector, your own adapter script. `add` uses
`store.Replace`, which is scoped to a producer: re-running the code extractor prunes stale
`code` nodes and leaves every other producer's slice untouched. Your adapter's ticket nodes do
not vanish because you re-parsed the source.

Two guards ride along. A `Replace` that would shrink a producer's slice by more than 50% is
**refused** unless you pass `--force`, because that is what a broken extractor looks like. And
`Merge` — the `--json` door — stamps a producer only when one is absent, so merging never
rewrites the provenance of a fact somebody else emitted.

This is also where a large chunk of the 0.14.0 speedup came from: `Replace` used to run once
*per producer*, each a full read-sort-write of the entire graph. Gathers are now roughly 2×
faster than 0.13.0 — Linux 124s → 55s, Kubernetes 14.6s → 7.3s, java-spring 6.1s → 2.9s.

## The lookup index: fast, and structurally unable to be wrong

Before 0.13.0, every verb materialized the whole graph to answer about one symbol. On a Linux
kernel store, reading all 2.1 GB costs 0.12s and `json.Unmarshal` of it costs 3.19s — so ~97%
of a lookup was deserialization paid *before the question was even known*. `card` took 1.8s.

`<store>/graph/index/` now holds four plain sorted text files — `labels.idx`, `ids.idx`,
`edges-by-source.idx`, `edges-by-target.idx`. A lookup **binary-searches them with 8KB `ReadAt`
windows** and parses only the records that match. `ReadAt` rather than mmap because this binary
targets Windows too. `card` on the kernel now answers in **under 20ms**.

It costs **0.43GB, 20% of the graph** (CodeGraph's index is 54% of their DB), builds in about
6s, and adds ~5% to a kernel gather.

**It fails safe, and that is the whole design.** Each index file's header records the source
file's size and modification time — you can read it yourself:

```text
$ head -1 ~/ctxoptimize/linux/graph/index/ids.idx
#ctxidx v1 size=880637115 mtime=1786809483352136868
```

On *any* mismatch — absent, stale, truncated, partially written — the caller silently falls
back to the full scan. The index can make an answer fast; it cannot make one wrong. It is
machine-local (byte offsets into this machine's graph), excluded from the manifest, and never
transported.

Scope is deliberately narrow. It is wired into `card` only, and only for exact-id and
exact-label resolution. Fuzzy and ambiguous names still pay a full scan **on purpose** — they
rank against every node, and refusing to guess is the point. A differential test resolves the
same symbols down both paths and compares whole result sets: 2002/2002 identical on the kernel
store. It caught four real defects while being built, including JSON escapes in index keys
(11,798 kernel labels, 0.414%) and a partial index reporting itself current.

## Confidence tiers, and what AMBIGUOUS costs you

Every edge carries one of three values, and the difference is operational, not decorative.

| tier | means |
|---|---|
| `EXTRACTED` | Parsed directly from source — the AST, the manifest, the live connector. |
| `INFERRED` | Name-matched or heuristically linked. Plausible, not certified. |
| `AMBIGUOUS` | Several candidates and no principled way to pick one. |

An AMBIGUOUS call is **not** guessed into the graph as a fact, and it is **not** thrown away
either. It becomes a shortlist, and every traversal verb filters it out by default. Verbs call
a shared `forTraversal` helper that filters unless an option says otherwise, so the safe
behaviour is the one you get for free and widening takes an explicit argument.

**The consequence you have to internalise: a blast radius is a floor, not the full set.**
Method calls through a receiver whose type was never established, and calls to a name defined
more than once, land in the shortlist — so `affected` on a method reports the callers it can
prove and stays quiet about the rest.

`--include-ambiguous` on `card`, `explain`, `affected`, `path`, `hubs` and `change-plan` opens
the door, with the maybes marked structurally rather than by a note you might skim: rows are
prefixed `?`, `card` puts them under their own `MAYBE called by (AMBIGUOUS — verify before
acting)` list, and `path` labels the hop as a candidate route. Fact fields never widen —
`CardData.CalledBy` contains only facts whatever flags you passed, so a consumer that has never
heard of the flag reads exactly what it read before.

```text
$ ctx-optimize affected Batch.Validate --include-ambiguous
...
 ?d2 TestMergeUpsertsAndDedupes  [function]  via calls on internal/store/store.go::Store.Merge
? 37 of these arrived on an AMBIGUOUS edge (--include-ambiguous): candidates, NOT facts. Verify before acting.
```

## The boundary lane rides the parser's walk

[`boundaries`](/ctx-optimize/boundaries/) models a repo's external surface — env reads,
spawned processes, HTTP hosts, served routes, web storage, websockets — as `port` nodes with a
direction, a transport, an identifier and a sensitivity flag. The rules are **declarative data,
not code**: JSON patterns over AST node shapes, merged repo > machine > embedded by rule ID,
with every emitted edge citing the rule that produced it and its `file:line`.

The interesting part is where they run. Rules are evaluated **inside the code extractor's
existing AST visit**, at the same call and member nodes it already walks to build `calls`
edges. There is no second read of the file and no second pass over the tree. The marginal cost
of a rule is a map probe on a parse that was already paid for.

That replaced a regex lane, and both halves of the trade were measured. The old lane re-opened
every file and scanned it once per rule — 10–11 scans per file — which cost the TypeScript
compiler repo **12.4 seconds of regex to produce 42 ports**. On the AST it is **0.06s**, and on
Kubernetes the lane's cost went from 5.29s to noise.

Accuracy moved with it, because the failures had one shape: **a regex sees text, so a variable
is invisible.** The shipped rules' own measured recall said so — `env-go` 0.81 (`os.Getenv(varName)`),
`process-go` 0.65, and `process-py` **0.00**, because in Python the argv is always a variable. A
parser sees a *call to `os.Getenv` whose argument is not a literal*, which is not a miss at all:
it is a known env read with a dynamic value. So `exec.Command(binVar)` is now visible as
AMBIGUOUS instead of invisible, and a repo that spawns processes stops reporting that it spawns
none. Rules for a language with no grammar pack installed are **declared** as raw-scan rather
than silently producing nothing.

Producer identity stays `boundaries` even though evaluation rides the code walk, so provenance,
`Replace` scoping and the audit trail all still key on it.

## What none of this does

No LLM call. No embedding. No MCP server. No credentials at rest — source URLs are stored as
env-var **names**, and the shell expands them at call time. The only network the binary touches
is something you explicitly asked for: `update`, a grammar download, `services add`, or the
remote push/pull command your own config declares.

Same input, same graph, byte for byte. The intelligence is your agent's job.
