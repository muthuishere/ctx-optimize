---
title: What it is
description: "Why query is not grep or embeddings, how the score is actually built, why each verb exists, and the schema — adapters, boundaries, confidence — underneath."
---

The store is already a graph — **nodes** (files, symbols, sections, tables, buckets) and **edges** (contains, calls, imports, the outer world). You do not search the repo. You ask a verb. That is the whole product.

## Why query is not grep — and not a vector store

Grep searches **lines in files**. You get a matching line. You still open the file, find the function, find who calls it, and stuff that into the window. A cheap model gets lost. A big model spends the session rediscovering the repo.

Embeddings search **meaning** in a vector index. That needs a model in the path, an index to build and keep fresh, and a similarity score that cannot tell a caller from a comment. We do not do that. There is no vector index and no model in this binary.

`query` searches **the graph that is already there** — the names of symbols and files, not the bytes of every source file. Then it hangs the neighbours on the hit so you leave with a citation, not a line to chase.

```text
ctx-optimize query "where is refund processing implemented"
```

### 1. Split like code, drop the English

The question becomes tokens the way identifiers are written.

```text
"where is refund processing implemented"
        │
        ▼  drop where / is   (question grammar — they are not symbols)
refund   processing   implemented

PaymentRefundProcessor.ProcessRefund
        │
        ▼  camelCase + acronyms
payment  refund  processor   process  refund
```

`HTTPServer` → `http` + `server`. Single letters are noise and go away. Stopwords are stripped from the **query only**. If we stripped them from nodes too, a function named `OnPath` would vanish; if we left `on` in the query, IDF would treat that rare word as a clue and hand you `install.go::OnPath`.

What is scored is `label + file path`. The signature and the doc comment ride on the *answer* so you can cite them. They are not extra search fields.

### 2. IDF — a rare word is the clue

Every token gets a weight from how few nodes contain it:

```text
weight(t)  =  0.1  +  ln( N / (1 + df(t)) )
```

`N` is how many nodes are in the store. `df(t)` is how many of them mention `t`. The `0.1` is a floor so a word that appears *everywhere* still matches, just barely.

Imagine:

```text
user      in 30,000 nodes   →  almost no evidence
refund    in    400 nodes   →  real evidence
stripe    in     18 nodes   →  this is probably what they meant
```

So `query "user refund stripe"` is not three equal votes. `stripe` decides. That is why this works on code: the interesting identifier is almost always the rare one.

### 3. Four ways a token can hit — strongest first

**Named.** You typed the node. `query "Store.Merge"` or `query "OPENAI_API_KEY"` matches id, label, or a port’s `identifier` exactly. That hit is **tier 0**. It sits above every scored row. We did not add `+1000` — a bonus has to outrun an unbounded IDF sum, and you cannot promise that. A sort key is correct by construction.

**Exact token.** `refund` is in the node’s set (`RefundProcessor`). Full IDF. Weight 1.0.

**Prefix.** `refund` vs `refunds`. Same idea, **0.7 × IDF**. If several tokens prefix-match, we take the *rarest* one — not whichever Go’s map yielded first. Same query, same store, same answer, every run.

**Trigram.** Typos and infixes. `refnud` and `refund` share character triples:

```text
refund  →  ref  efu  fun  und
refnud  →  ref  efn  fnu  nud
```

Overlap is the [Sørensen–Dice](https://en.wikipedia.org/wiki/S%C3%B8rensen%E2%80%93Dice_coefficient) coefficient: `2 × shared / (len(a) + len(b))`. We keep it only if Dice ≥ 0.5, and it is worth **0.4 × IDF**. Weakest on purpose: a typo can still find you, it cannot beat a real name.

```text
exact token     1.0 × IDF
prefix          0.7 × IDF
trigram         0.4 × IDF
named           not a weight — it sorts first
```

### 4. Then a tiny bit of intent — still no model

`where is url_for implemented` should not rank README, then the test, then the import stub, then the function.

```text
module:// stub     × 0.25     unless you said import / module
*_test.go          × 0.50     unless you said test
section / document × 0.50     unless you said doc / adr / spec
```

That is a rule, not a classifier. Docs used to steal the top slot: 39% of one graph, 15 of 30 top-3 hits on code questions, before this.

### 5. Then the graph — and then we stop

A winning node is not `refund_service.go:37`. It comes with its 1-hop neighbourhood (cap 12):

```text
ProcessRefund  [function]  refund_service.go L37-L80
    sig: func (s *Service) ProcessRefund(id string) error
    ← calls CheckoutHandler
    → calls PaymentGateway.Refund
```

Those arrows are **shown**. They do not become extra hits. A callee with none of the query words cannot win today. AMBIGUOUS edges stay out unless you pass `--include-ambiguous`.

Then the **budget**: each complete hit has a token cost (`chars/4`). We stop at ~2000 (or `--budget N`), and at 20 hits. The agent is supposed to stop reading. Quality here is useful facts per token, not Recall@100.

Grep still wins exact strings, comments, and config **values**. We do not replace it. We answer structure.

`card` is a different engine — a fail-safe label/id index, under 20 ms on the kernel. `query` still walks every node. That is honest, and it is why the next ADR is postings, not embeddings.

## Why every verb

An agent that only has `query` will query for everything, the way it greps for everything. Each verb is one job, so a small model can pick the right one and a big model does not refill the window.

| You want | Verb | Why it exists |
|---|---|---|
| Find something from **words** | `query` | You do not know the symbol yet. Ranked nodes + neighbors, under a budget. |
| Inspect a **known** symbol | `card` | Signature, doc, callers, callees, `file:line` — without opening the file. |
| **About to edit** | `change-plan` | The one composed call: card + callers + blast + which tests to run. |
| Blast radius only | `affected` | What breaks if this changes. Floor, not a guess — ambiguous callers stay a shortlist. |
| How A connects to B | `path` | A walk on real edges, not a story. |
| Plain-language context | `explain` | For a human (or an agent) that has the node and needs the sentence. |
| Orient in a new repo | `hubs` | What everything depends on. The load-bearing names. |
| What this **talks to** | `boundaries` | Hosts, env **names**, spawned binaries — never a secret value. |
| See it | `serve` | Same store, Flow picture. A card is a directory. An arrow is N real edges. |

Pick by intent, not habit. The [cookbook](/ctx-optimize/cookbook/) is the question you would ask a teammate mapped to the one command.

## Why extendable — why an adapter

Code is not the system. The system is code **plus** the database, the bucket, the queue, the ticket list, the thing only your company has.

Every producer emits the **same** shape: a `Batch` of nodes and edges, tagged with a producer name. Tree-sitter does that for Go and TypeScript. Goldmark does it for ADRs. A connector does it for Postgres and S3. Your script does it for everything else.

```text
.ctxoptimize/adapters/   # drop .js / .py / .sh — the file existing IS the registration
```

Print one JSON batch to stdout. The door validates it fail-closed and merges it. No fork. No MCP. No special “doc graph” versus “code graph.”

Native sources are the same door with less work: an env-var **name** whose value is a URL (`postgres://…`, `s3://…`). The scheme picks the connector. Credentials stay in the environment; the committed config holds names, never values.

That is why a repo like reqsume can show `public.applications`, Firebase Auth, and `reqsume-local` next to `Store.Merge`. Not because we hardcoded SaaS — because the store does not care who emitted the node.

## Why boundaries

A call graph that stops at your functions is a lie about the system. The thing that pages you is usually **outside**: `api.openai.com`, `POSTGRES_PASSWORD`, `s3://…`, a binary you spawn.

`boundaries` is that outer surface, extracted while the parser already walks the tree — direction (consumes / provides), transport (`network.http`, `config.env`, `process.exec`), identifier by **name**. A secret is flagged as a name. The value never enters the store.

Flow draws those as the dashed plates under the module cards. An architect sees what the service talks to. An agent running `change-plan` sees the same ports. One store, two doors.

Without this row, we are another pretty graph of functions. With it, the graph is the system.

## Two arrays. That is the schema.

Every producer — code, markdown, routes, manifests, k8s, a Postgres connector, your adapter — emits the same `Batch`. There is no special-cased “code graph” versus “doc graph.”

```text
Batch {
  producer: "code" | "markdown" | "firebase" | "source:BILLING_DB_URL" | …

  nodes: [
    { id, label, kind, file_type, source, location?, metadata? }
  ]

  edges: [
    { source, target, relation, confidence: EXTRACTED | INFERRED | AMBIGUOUS }
  ]
}
```

A node is anything worth naming: a file, a function, a heading, a route, a k8s Service, a Postgres table, a bucket prefix, a ticket. `kind` is a string, not a closed enum — that is what keeps the adapter door open. An edge is a typed link: `contains`, `calls`, `imports`, `resolves_to`, or whatever your producer needs.

The validator is fail-closed. Missing id, missing kind, a confidence outside the enum, a duplicate id — the whole batch is rejected. Nothing half-writes.

**What actually fills a repo**

| Family | What lands |
|---|---|
| Code | Every file, and every function / method / class / struct tree-sitter parses — signature, doc comment, `L#-L#`. |
| Markdown | Headings as sections (goldmark AST, not a line regex). Fences are not sections. A link is an edge only if the target resolved in the walk. |
| Routes | FastAPI / Flask / Express / Nest / React Router / Vue / OpenAPI / Ingress → the handler. |
| Manifests | package.json, go.mod, pom, csproj, Gradle, Cargo — one `dep:` node federated across tools. |
| k8s | Deployments, Services, Ingress, ConfigMaps; Secrets as nodes, **never their data**. |
| Live sources | Tables, topics, buckets, operations — logical shape from a URL in an env var. |
| Your adapter | Whatever you emit. Same door. |

`contains` nests a file → its declarations, or a doc → its sections. `calls` is module-wide, unique name. `imports` is file → module. `resolves_to` bridges an import to the `dep:` that ships it, so “what depends on lodash” is a graph answer, not a lockfile grep.

## EXTRACTED vs INFERRED — every edge says how sure it is

A graph that presents a guess as a fact is worse than no graph. An agent will cite it.

| confidence | means | example |
|---|---|---|
| **EXTRACTED** | Parsed from the AST, the manifest, or the live connector. Not a guess. | a `calls` edge from a resolved, unique call site |
| **INFERRED** | Name-matched or heuristic. Plausible, not certified. | a route matched to a handler by naming convention |
| **AMBIGUOUS** | Several candidates. **Kept, filtered out of every traversal by default.** | `Match` declared eight times — the call is a shortlist, not a caller |

`--include-ambiguous` on `card` / `affected` / `change-plan` / `path` / `hubs` widens the shortlist and marks those rows as candidates. A blast radius is a **floor**. `change-plan` prints a confidence footer so you know how much to trust each line.

## Where it lives

The graph is **not** in your repo. It is `~/ctxoptimize/<name>/` — basename, or `name` in config if two repos collide. Sorted ndjson, atomic rename, git-diffable even when you do not commit it.

```text
~/ctxoptimize/<repo>/
  graph/nodes.ndjson     # one object per line, sorted by id
  graph/edges.ndjson
  manifest.json          # content-hash per producer
  sources.json           # native-source freshness (24h TTL)
  audit.ndjson           # mutations: actor, action, before/after sha256
  wiki/                  # opt-in markdown map (`ctx-optimize wiki`)

# the ONLY committed thing:
.ctxoptimize/
  config.json            # name, modules[], sources[] (env-var NAMES), remote
  instructions.md        # usage card agents read
  adapters/              # drop-in scripts — existence is registration
```

Source URLs stay env-var **names**. Values resolve at dial time (process env → repo `.env` → `~/.config/ctx-optimize/.env`) and are never written or printed.

## The store knows when it is lying

It records the git commit it was gathered at. Every read can compare that to HEAD.

```text
$ ctx-optimize status
fresh:  ✗ STALE — store at 8a5057b, repo now at 03d0f49; run: ctx-optimize add .

$ ctx-optimize fresh; echo $?
# 0 = fresh · 1 = stale · 2 = unknown (no git HEAD)
```

`up` is the one verb that does the right thing: fresh → no-op, stale → incremental, missing → bootstrap or pull. Default autosync is **off**. After you edit, `ctx-optimize sync`. The agent may still grep.

## Languages, and one store per module

Code is tree-sitter compiled to WASI, hosted by wazero in pure Go. `CGO_ENABLED=0`, one static binary. Embedded: Go, Python, JS, TS/TSX, Java, C, C++, C#, Rust, Zig, SQL. Anything else is a **pack** (`<name>.wasm` + `<name>.json`) in `~/ctxoptimize/grammars/` or `.ctxoptimize/grammars/`. `languages add` builds one; Zig is downloaded once, sha256-checked. The gather machine, with numbers, is [how it works](/ctx-optimize/how-it-works/).

A 300-module mega-graph helps nobody. Each declared module gets its own store. Cwd picks the scope. A miss escalates repo-wide and labels where it came from. `--modules all|a,b` or `--root` if you need to say so.

```text
~/ctxoptimize/acme/
  services/api/       # full graph for that module
  services/worker/
  graph/              # residual: top-level files in no module
  navigator.md        # path, counts, hubs per module
```

[How to use it](/ctx-optimize/guide/) · [Cookbook](/ctx-optimize/cookbook/) · [Boundaries](/ctx-optimize/boundaries/) · [See the picture](/ctx-optimize/see/) · [How it works](/ctx-optimize/how-it-works/)
