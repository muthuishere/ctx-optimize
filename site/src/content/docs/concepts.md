---
title: What it is
description: "The mental model an agent needs: two arrays, several worlds of fact, and an edge that tells you how sure it is."
---

Not "we have a graph." Here's what's actually on disk, how a fact gets in, how honest it is once it's there, and how a question finds it in milliseconds instead of a grep-and-read chain. Everything below is what the binary really does — nothing aspirational.

## Two arrays. Nodes and edges. That's the whole schema.

Every producer in the system — code extraction, markdown docs, framework routes, build-tool manifests, k8s YAML, a database connector, your own adapter script — emits the same shape: a **Batch** of nodes and edges, tagged with a **producer** name. The store validates it, upserts it, and moves on. There's no special-cased "code graph" versus "doc graph" — one graph, many producers feeding it.

```text
Batch {
  producer: "code" | "markdown" | "routes" | "manifests" | "source:BILLING_DB_URL" | "tickets" ...

  nodes: [
    { id, label, kind, file_type, source, location? (file, line_start, line_end), metadata? }
  ]

  edges: [
    { source, target, relation, confidence: EXTRACTED | INFERRED | AMBIGUOUS }
  ]
}
```

A node is anything worth naming: a file, a function, a doc section, an HTTP route, a k8s Service, a Postgres table, a package dependency, a ticket. An edge is a typed relationship between two node ids: `contains`, `calls`, `imports`, `resolves_to`, `relates` — whatever the producer needs. The validator is fail-closed: missing id, missing kind, a confidence value outside the enum, a duplicate id — the whole batch is rejected, loudly, before anything lands. Nothing half-writes.

## One graph, several worlds of fact.

"Kind" is a free-form string per producer, not a fixed enum baked into the binary — that's what makes the adapter door open. In practice, a repo's store fills up with these families:

Every source file, and every function/method/class/struct tree-sitter parses out of it — signature, doc comment, and an exact `L#-L#` location.

Markdown headings become sections, nested by heading level, so a README and a wiki page are queryable the same way as code.

HTTP/RPC endpoints from FastAPI, Flask, Express, NestJS, Angular, React Router, Vue, OpenAPI specs, Ingress YAML — linked to the handler that binds them.

package.json / go.mod / pom.xml / csproj+sln / Gradle / Cargo entries, one `dep:` node per package federated across every build tool and module that imports it.

Deployments, Services, Ingress, ConfigMaps, Secrets (never their data) parsed straight from committed manifests — real cluster topology, not a guess.

Tables, collections, topics, buckets, operations — captured by a wire-protocol connector from a live Postgres/Mongo/Kafka/S3/OpenAPI system, logical shape only.

In a multi-module repo, each module gets its own store; the root navigator holds one summary node per module — path, node/edge counts, top hubs.

Anything your own script emits — tickets, custom logs, converted PDFs — through the same validated `--json` door, no fork required.

Edges follow the same open convention. `contains` nests a file → its declarations, or a doc → its sections. `calls` links a declaration to another it invokes (module-wide, resolved by unique name). `imports` ties a file to a module. `resolves_to` bridges a code import to the `dep:` node that actually ships it — so "what depends on lodash" answers from code, not just from the manifest. Adapters can declare any relation name that means something for their world.

## EXTRACTED vs INFERRED — every edge says how sure it is.

A knowledge graph that presents a guess with the same confidence as a fact is worse than no graph — an agent will cite it. Every edge in the store carries one of three confidence values, and the difference is not cosmetic:

| confidence | means | example |
|---|---|---|
| EXTRACTED | Parsed directly from source — the AST, the manifest, the live connector. Not a guess. | a function's `calls` edge from a resolved, unambiguous call site |
| INFERRED | Name-matched or heuristically linked — plausible, not certified. | a route matched to a handler by naming convention, not an explicit binding |
| AMBIGUOUS | Multiple candidates, no way to pick one confidently — the edge is **dropped**, never guessed into the graph. | two functions with the same name in different packages calling a third — the call edge is omitted rather than pointed at the wrong one |

This is the same discipline the [confidence note on the home page](/ctx-optimize/#ask) talks about, made concrete: ambiguous calls are dropped, never guessed. When you ask `change-plan` for a blast radius, the answer separates extracted evidence from inferred evidence in its confidence footer — so the agent (and you) know exactly how much to trust each line.

## Plain files, outside your repo, one per machine.

The graph itself never lives in your repo. It lives at `~/ctxoptimize/<repo-name>/` (the key is the repo's basename, or an explicit `name` in config for repos that collide) — sorted NDJSON, atomic writes, git-diffable by design even though it usually isn't committed.

```text
~/ctxoptimize/<repo>/
  nodes.ndjson        # one JSON object per line, sorted by id
  edges.ndjson        # same — diffs read like a real diff, not a blob rewrite
  manifest.json       # content-hash per producer — what changed since last gather
  sources.json        # per-source freshness stamps (native sources, 24h TTL)
  audit.ndjson        # append-only: every mutation, actor, before/after sha256
  wiki/               # deterministic markdown map, regenerated on every add

# the ONLY thing that lives in YOUR repo, and is meant to be committed:
.ctxoptimize/
  config.json         # name, remote {push,pull}, sources[], modules[] (monorepo)
  instructions.md     # the usage card agents read — versioned managed block
  adapters/           # drop-in scripts — the file existing IS the registration
```

The split is deliberate: `.ctxoptimize/` is small, text, and safe to commit — config, adapter scripts, no secrets (source URLs stay env-var **names**, never values). The actual graph is regenerated data, so it stays out of your repo's history the same way a `node_modules` or a build artifact would — except it's small enough (kilobytes to low megabytes for most repos) that sharing it is just a script your team declares ([see remote push/pull](/ctx-optimize/use-cases/)), not a registry to run.

## The store knows when it's lying to you.

A stale graph that answers confidently is the failure mode every graph tool has to solve for. ctx-optimize's answer is blunt: the store records the git commit it was gathered at, and every read path can check that against the repo's current HEAD before trusting an answer.

```text
$ ctx-optimize status
store:  ~/ctxoptimize/ctx-optimize
nodes:  3104
edges:  5939
remote: (none)
fresh:  ✗ STALE — store at 8a5057b, repo now at 03d0f49; run: ctx-optimize add .
served: 94 answers

$ ctx-optimize fresh; echo $?
# exit 0 = fresh · 1 = stale · 2 = unknown (no git HEAD to compare — e.g. not a git repo)
```

`fresh` is the scriptable gate — wire it into a pre-commit hook, a CI step, or an agent's own preflight (`ctx-optimize up && ctx-optimize fresh`) so nothing answers from a snapshot that predates the code it's describing. `up` is the one verb that always does the right thing here: fresh → no-op, stale → fast re-gather, missing → bootstrap or pull. Outside a git repo, freshness reports `unknown` rather than pretending certainty it doesn't have.

## Tree-sitter grammars, compiled to WASM, hosted in pure Go.

Code extraction doesn't shell out to a language server or a per-language toolchain. Every grammar is compiled ahead of time to WASI-target WebAssembly and embedded in the binary; a pure-Go WebAssembly runtime ([wazero](https://github.com/tetratelabs/wazero)) hosts it — one instance per worker goroutine, fanned across every core. `CGO_ENABLED=0`, one static binary, nothing to install.

12 languages ship embedded in the binary today — Go, Python, JavaScript, TypeScript/TSX, Java, C, C++, C#, Rust, Zig, SQL. Anything else is a **grammar pack**: a `<name>.wasm` plus a `<name>.json` node-type mapping, dropped into `~/ctxoptimize/grammars/` (machine-wide) or `.ctxoptimize/grammars/` (travels with the repo). `ctx-optimize languages add kotlin` builds one from any tree-sitter grammar, in pure Go — no toolchain to install; a Zig compiler is fetched once, sha256-verified against ziglang.org's index, and cached. Calls resolve module-wide by unique name; an ambiguous match is dropped rather than guessed (see [provenance](#provenance) above).

## One store per module. A navigator, not a merged mega-graph.

A single 300-module graph helps nobody — people work in one module at a time, and an agent loading the whole repo's graph pays for 299 modules it isn't asking about. Instead, each declared module gets its own store, and the root holds a small **navigator**: every module's path, node/edge counts, and top hub symbols.

```text
~/ctxoptimize/acme/
  services/api/       # full store: graph, wiki, cards — just this module
  services/worker/    # full store: its own scope, its own shrink-guard
  graph/              # the ROOT RESIDUAL — top-level files in no declared module
  navigator.md        # federation index: path, counts, hubs, README one-liner per module
```

Scope follows your shell's cwd: ask a question inside `services/api` and it answers from api's store, ranked against api's symbols only — not diluted by every other module's noise. A miss escalates repo-wide automatically and labels the result with the module it actually came from. At the root, the navigator ranks which modules likely hold the answer and federates across them (`--modules all|a,b`, or `--root` to force the residual only). `merge` stays opt-in for the rare case you want one combined artifact — it's derived, so re-derive it after a pull rather than syncing it directly. Full detail: [docs → multi-module](/ctx-optimize/cli/), and the [monorepo use case](/ctx-optimize/use-cases/).

## Lexical + graph, no embeddings, under a token budget.

There's no vector index and no model in this path. `query` ranks nodes with IDF-weighted lexical matching plus prefix and trigram tiers over labels, doc comments, and file paths, then returns complete hits — id, kind, file, line range, signature, doc — under a hard token budget, so an agent gets citeable facts instead of a pointer it still has to chase.

Pick the verb by intent, not habit: `query` to find something from words, `card` to inspect a known symbol without opening its file, `change-plan` for the one composed call an edit needs (signature + callers + blast radius + which tests to run), `affected` for blast radius alone, `path` to connect two symbols, `explain` for plain-language context, `hubs` to orient in an unfamiliar repo. The full breakdown, with real commands, is the [cookbook](/ctx-optimize/cookbook/).

**[](/ctx-optimize/)[](/ctx-optimize/cookbook/)[](/ctx-optimize/use-cases/)[](/ctx-optimize/cli/)[](https://github.com/muthuishere/ctx-optimize)[](https://github.com/muthuishere)
