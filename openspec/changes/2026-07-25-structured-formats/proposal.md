# ADR — structured non-code formats: properties / yaml / toml / xml / proto

Status: **SUPERSEDED by `design.md`** (ACCEPTED 2026-07-25) — kept for the option
menu, the refuted claims, and the reasoning trail. Drafted 2026-07-25. Owner asked: *"the graph we build is not that
efficient on properties / yml / proto / xml / markdown / yaml — can we ship some
tree-sitter so these connect better, or do we already have better?"*

## OWNER SCOPE DECISION (2026-07-25, mid-spike — governs everything below)

> *"we dont want to complete every — let them add adapters for what they want,
> we will take care of honest stuff"*

**Coverage is NOT the goal.** ctx-optimize does not chase every format. The
adapter script + manifest pack + grammar pack doors already exist and are the
user's lane for anything exotic. Our job is that **what we do emit is honest**:
deterministic, correctly located, not junk, and documented without overclaiming.

This turns the item list into a filter. An item ships only if it answers YES to
one of:

- **H1 — is today's output WRONG or nondeterministic?** (a bug: same input,
  different graph; a node with no file:line; silently dropped data)
- **H2 — do we CLAIM a capability we don't have?** (docs or a verb that implies
  coverage/joins that don't exist)
- **H3 — would shipping it make the graph LESS honest?** (then it must NOT ship,
  regardless of how much coverage it adds)

Everything that is merely *more coverage* is **out** — redirected to the adapter
/ pack door, and the docs must say so plainly.

Consequences, applied to the items below:

| item | fate under the filter | why |
|---|---|---|
| pack `Location` missing | **SHIP** | H1 — a node with no file:line can't be cited or verified |
| pack ID namespace-blind | **SHIP** | H1 — silently drops a matched fact |
| pack doc example wrong | **SHIP** | H2 — documented example yields 0 |
| P3-bug whitespace guard | **SHIP** | H1 — trailing whitespace changes the graph (measured: same file → 9 nodes vs 4) |
| block-scalar junk keys | **SHIP** | H1/H3 — keys parsed from inside a `foo.conf: \|` literal block are not config structure (measured: 9 junk nodes in real k8s files) |
| P6 skills query-side docs | **SHIP** | H2 — sources are ingestable but undocumented to query, and their island-ness is unstated |
| P4 proto grammar | **OUT** → user's pack door | coverage; and H3 — measured 126 correct edges destroyed + 1 invented, on real beam |
| `grammar build` ignores `CTX_OPTIMIZE_GRAMMARS` | **SHIP** | H1 — the loader honors the env var, the builder writes elsewhere |
| P5b spec↔code route join | **OUT** → P6 disclosure | refuted: 0% raw join rate on every real repo |
| P5a code→config_key | **OUT** (blocked) | needs P3a dotted paths, which needs the ranker change — chain too long for an honesty round |
| P1 XML packs | **OUT** → user's pack door | coverage; near-zero real prevalence |
| P1b `.xml` in configExts | **REJECTED** | H3 — measured 97.2% markup junk |
| P3a dotted paths | **PARKED** | H3 — measured retrieval regression (rank 2 → 5+); needs a ranker change first |
| P2 py/rust deps | **SHIP** | H2 — measured: `deps` prints `(0 dependencies)` on a repo declaring 61 |

### How the three judgment calls resolved once measured

**P2 (Python/Rust dependency extraction) — SHIP. Confirmed H2, not coverage.**
This is a verb lying by omission, and the spike measured exactly how:

- On `corpus-flask`, ctx-optimize emits **0 dependency nodes and 0 `declares`
  edges** while the manifests declare **61 deps (41 distinct)**. `deps` prints
  `(0 dependencies)` — indistinguishable from a repo with no dependencies.
- Worse, it emits **221 `config_key` nodes, 199 of them from `pyproject.toml`**:
  199 nodes describing the file that holds the answer, without holding the
  answer. That is the honesty failure stated precisely.
- Prevalence is not marginal: Python manifests in **14 of 52** top-level dirs
  under `~/muthu/gitworkspace`; Cargo in 4. Both `linux` and `chromium` carry
  `pyproject.toml`.

**And the stdlib-only crux is settled: YES.** A ~230-line TOML *table walker*
(vs yamlwalk's 107) extracted **1,639 / 1,639 declarations across 103 real
manifests — 100% precision, 100% recall, 0 version-spec mismatches**, judged
against Python's `tomllib` as an independent oracle, all 103 files in 0.009s. A
7-file hand-written torture corpus scored 44/44. So no TOML library is needed and
the no-third-party rule holds.

Two real failure shapes were found and fixed inside the spike (~25 lines):
multi-line `"""` strings weren't skipped (a fake dep from prose in a
`description`), and the inline dependency *table* form
`[tool.poetry] dependencies = { a = "^1" }` was missed. Multi-line inline tables
turned out to be **illegal TOML** (tomllib rejects the file), so they are a
non-issue.

Design requirements this pins down:
- **Namespaces `pypi` and `crates`**; PEP 503 name normalization is measurably
  needed (166 raw → 165 distinct; `flit_core`/`poetry_core`/`typing_extensions`
  collapse) — one real duplicate node avoided in a 28-file sample.
- **Table-anchored matching, never an array-of-version-strings heuristic.** The
  adversarial case is flask's own
  `[tool.tox.env.tests-min] commands = [["uv","pip","install","blinker==1.9.0",…]]`
  — a naive heuristic invents ~12 false deps; table-anchored gave exactly the
  hand-counted 31 with 0 extras.
- **Lock files must be marked or excluded**: 840/1,366 (61%) of pypi declarations
  come from `requirements*.txt` and **12 of 40 are pip-compile locks**. Emitting
  transitive pins as declared dependencies would be a NEW honesty failure —
  `isLockfile` only refuses `*.lock`, which pip-compile output is not.
- Scope-class gaps to fix: `scopeClasses` has `devDependencies` but not
  `dev-dependencies`, and its `HasPrefix("test")` rule misses
  `dependency-groups:tests`.
- **Scope P2a (Python + Rust), not P2b.** Ruby/PHP total 1 Gemfile + 3
  composer.json, 3 of the 4 vendored inside chromium — that genuinely is coverage,
  so it goes to the adapter door.

**P5b (spec route ↔ code route) — REFUTED. Do not ship; document instead.** I
called this "the cheapest, highest-confidence win, an exact label match". The
spike measured the join rate on real repos and it is **0%**:

| repo | raw-label join | + param normalization | + mount-suffix | ambiguous |
|---|---|---|---|---|
| review-app | **0 / 63** | 0 / 63 | 44 / 63 (69.8%) | 25.4% |
| iepapp | **0 / 47** | not reported | 12.8% | 27.7% |

My scratch repo joined only because I hand-wrote both halves to match. In real
repos nothing joins raw, param normalization alone adds **nothing**, and the
actual blocker is Express/NestJS **mount-prefix resolution** (a `deplink`-shaped
problem of its own). False-join risk is measured and severe: `GET /` appears in
**20 different route files** in one repo. Prevalence is also thin — only 1 repo
had both lanes recognized, and 4 of 5 specs found were JSON, which the openapi
file lane does not accept.

⇒ The duplicate twins are real, but joining them is a *project*, not a cheap win.
The honest move now is P6 disclosure: say that spec routes and code routes are
separate nodes that we do not join.

**P3a (dotted config paths) — NOT shipping, and the spike now proves it would
make answers WORSE, not merely be out of scope.** `internal/query/query.go:251`
already multiplies any dotted label of a non-callable kind by **×0.2**, and
`config_key` is not in `callableKind` (query.go:84-88). Today no config_key label
contains a dot, so that penalty never fires; P3a would make it fire on **all** of
them. Measured on a repo with config + code, `query "server port"` returns the
config key at **rank 2** today and at **rank 5+** with dotted labels — buried
below two unrelated functions (`LoggingLevel` 0.95 > `management.server.port`
0.54).

So P3a fails the H3 test outright: it would ship a retrieval **regression** while
moving node IDs and 36 golden lines. Its findability upside is real and large
(labels resolving to exactly one node 17.1% → 53.3%; ambiguous-label nodes 77.8%
→ 35.3%; 61 of 163 config_key nodes in this repo's own store, 37.4%, currently
carry a label that also exists in another file) and its size cost is nil (1.03×,
no depth cap needed — max real nesting 7, longest label 67 chars). **But it
cannot ship before the query-side dotted-label exemption, and that exemption is a
query-ranking change with its own judged-scoreboard risk.** Park the pair; only
the whitespace bug and the block-scalar junk fix ship now.

### What the docs must now say (H2 obligations)

Shipping the "adapters are your lane" doctrine honestly requires stating the
traps we measured, or we hand users a door with a hidden hole:

- **Grammar packs**: a pack's `names` mapping yields the BARE identifier, and call
  resolution drops ambiguous names module-wide — so adding a pack whose decl
  names duplicate existing ones **silently deletes working `calls` edges**
  (reproduced: 1 → 0). Anyone building a proto/other pack must be warned.
- **Manifest packs**: selectors are root-anchored exact-depth (no descendant
  operator), cannot pair two attributes of one element, and `emit` is only
  `{dependency, task}` — so many facts have no honest kind. Say it, and point at
  adapter scripts for the rest.
- **Native sources**: captured subgraphs are **islands** — no edges to code. Say
  it, so no agent promises "which code writes this table".

Spikes (effectiveness per item) go in `spikes.md`. The finalized decision goes in
`design.md`. **No code until the owner signs off** (repo change-flow).

Deterministic, no LLM, stdlib-only — unchanged doctrine.

## The short answer to the question asked

**Tree-sitter is the right tool for exactly one of these formats (proto) and the
wrong tool for the rest.** The evidence is that where we wrote a *semantic
recognizer*, these formats already connect well; where we didn't, they don't —
and parsing was never the missing piece.

Measured on a probe repo (`.properties`, `.yml`, `.proto`, `.xml`, `.toml`, `.md`,
one Go file):

| format | today's result |
|---|---|
| `openapi.yml` | **2 route nodes** (`GET /orders/{id}` L6) — `yamlwalk` + `yamlroutes.go`, no grammar |
| k8s yaml, `pom.xml`, `.csproj`, `Taskfile.yml` | recognized, real nodes + edges |
| `.properties` | `config` + 4 flat `config_key` nodes |
| `.yml` (generic) | `config` + `config_key`, **hierarchy discarded** (`maxAttempts`, not `orders.retry.maxAttempts`) |
| `pyproject.toml` | `config_key` nodes only — **no `dep:` nodes at all** |
| `.proto` | **zero nodes** — not even a file node; `query "LineItem"` → no matches |
| `.xml` (generic, e.g. `logback.xml`) | **zero nodes**; `query "logback appender"` → no matches |
| `.md` | `document` + `section` nodes — already good, leave alone |

So YAML is not weak. *Unrecognized* YAML is weak, and two formats are entirely
absent.

## The load-bearing distinction: findable vs connected

The owner's word was **connect**. Verified against this repo's own store — every
`config_key` node has exactly **one** edge, `contains`, from its own file:

```
163  config  contains    config_key
 44  config  declares    dependency
 22  config  contains    task
  4  config  depends_on  config
  1  config  contains    resource
```

There is **no `code --reads--> config_key` relation in the schema at all.**

Consequence, stated plainly so it isn't oversold later: P1–P4 buy
**findability** (nodes exist, labels are unambiguous). They do **not** make
`affected orders.retry.maxAttempts` list the code that reads the key — that
needs a new edge type, which is P5. Shipping P1–P4 alone answers "where is this
configured?" but still not "what breaks if I change it?".

## P1 — XML config via bundled manifest packs (not a grammar)

Generic `.xml` produces zero nodes: `configExts`
(`internal/extract/markdown/markdown.go:112`) has `.properties/.yaml/.yml/.toml/.ini`
but not `.xml`, and only `pom.xml` matches by basename.

Tree-sitter XML would yield a parse tree with no semantics. We **already have the
better mechanism**: declarative manifest packs (`internal/extract/manifests/packs.go`)
already support `"format": "xml"` with element+attribute selectors
(`"path": "target/@name"`). Nobody ships packs for the common files.

Options:
- **P1a** — ship bundled XML packs for logback, spring beans, `web.xml`, Ant
  `build.xml`, ivy. Zero new mechanism; pure data.
- **P1b** — add `.xml` to `configExts` so *any* XML at least becomes a
  searchable `config` document with keys.
- **P1c** — both (P1b as the floor, P1a for the ones worth semantics).
- Rejected: an XML tree-sitter grammar — more mechanism, less meaning.

Open: does the tiny selector language cover spring `<bean class=…>` and
`<property name=… ref=…>`? Spike must confirm, since "if it can't express it, the
answer is an adapter, not a bigger language" is existing doctrine.

## P2 — Python / Rust / Ruby / PHP dependency extraction

`manifestKind` (`manifests.go:245`) handles `package.json`, `go.mod`, `pom.xml`,
gradle, `.csproj`, `.sln`, Taskfile, Makefile, justfile — and **not**
`pyproject.toml`, `Cargo.toml`, `requirements.txt`, `Gemfile`, `composer.json`.

Measured: `query "fastapi dependency"` returns the `dependencies` *key* node; no
`dep:pypi/fastapi` node exists. So `deps` is blind on every Python repo.

This is the one item the owner did **not** ask about, and likely the highest
user-visible impact — ctx-optimize claims dependency awareness and silently has
none for the second-most-common language ecosystem.

Options:
- **P2a** — `pyproject.toml` (PEP 621 `project.dependencies` + poetry
  `tool.poetry.dependencies`) + `requirements.txt` + `Cargo.toml`.
- **P2b** — P2a plus `Gemfile` / `composer.json`.
- Open: TOML has no stdlib parser in Go. Does the existing `yamlwalk`-style line
  walker suffice for these narrow shapes, or does this need a small dedicated
  TOML table walker? **Stdlib-only rule forbids a TOML library.** Spike must
  answer this — it is the main implementation risk in P2.

## P3 — YAML/TOML/properties dotted paths + the whitespace guard bug

`extractConfig` (`markdown.go:~146`) is a line scanner. Two defects:

**P3-bug — the "top-level only" guard is whitespace-dependent.** The documented
intent is top-level keys only; the condition is
`line != t && strings.TrimLeft(line, " \t") != line` where `t` is `line` with
*trailing* whitespace trimmed. So a nested line is skipped **only if it happens
to carry trailing whitespace**:

```
'  nested_no_trailing: 1'    → indexed
'  nested_with_trailing: 2 ' → skipped
```

Whether a nested key enters the graph depends on an invisible character. Either
the comment or the code is wrong; today's behavior (index every depth, flatten
it) is the accidental one.

**P3-flat — hierarchy is discarded.** Nested keys emit as bare leaf labels
(`port`, `level`, `retry`, `maxAttempts`) with `contains` wired file→key rather
than key→key. In a monorepo, `name`/`port`/`enabled` collide across every file.

Options:
- **P3a** — route `.yml/.yaml` through the existing `yamlwalk` (which **already
  returns `Indent` per line** — exactly the nesting info needed), emit dotted
  paths (`orders.retry.maxAttempts`) and key→key `contains` edges. Same
  treatment for TOML sections and `.properties` (already dotted natively).
- **P3b** — honor the documented intent instead: fix the guard to truly
  top-level-only, keeping the graph small.
- Rejected: a YAML tree-sitter grammar. It yields `block_mapping_pair` nodes and
  you must still write the path composition; worse, the grammar-pack mapping
  format (`decls`/`names`/`calls`/`imports`) **cannot express** "join ancestor
  keys", so it needs Go code either way.

**P3a changes existing node IDs** → golden snapshots move. That diff is a
reviewed diff, not a rubber stamp. Spike must report the node-count delta on a
real config-heavy repo — a 10× config_key explosion would be a reason to prefer
a depth cap.

## P4 — proto via tree-sitter grammar, WITH a collision guard

The genuine tree-sitter case. `tree-sitter-proto` node types (`message`, `enum`,
`service`, `rpc`, `import`) map cleanly onto the existing pack vocabulary
(`grammars/kotlin.json` shape), so `grammar build <url>` produces a working pack
**with no binary change**. Proto `import` edges are real cross-service structure.

**But it carries a measured regression risk.** Call resolution
(`internal/extract/code/code.go:331`) prefers a same-file unique match, else
**module-wide unique**; ambiguity is dropped, never guessed. Adding a second
declaration of an existing name silently deletes working edges. Reproduced,
cross-file, one commit apart:

```
BEFORE:  b.go::Caller --calls--> a.go::PlaceOrder   (1 edges)
AFTER (gen.py adds `def PlaceOrder`):              (0 edges)
```

A correct edge vanished. `protoc` generates Go/Java/Python types named *exactly*
after the messages and services, so in any repo that vendors generated code this
collision is the **normal case, not bad luck**: we would add proto nodes and lose
call edges among the generated code in the same pass.

### CORRECTION — my proposed mitigation was WRONG (spike-p45, measured)

I wrote above that qualified labels would prevent the collision because "the
resolver keys on `d.label`". **That is false against HEAD**, and the spike proved
it two ways:

1. **A pack cannot emit a qualified label at all.** `packConfig` carries only
   `name/exts/decls/names/calls/imports` (`langs.go:224-231`); the `qual` label is
   built in Go at `code.go:562-580` and emitted at `code.go:591`. Verified
   empirically with a real built proto pack: it emits `Order`, never
   `acme.orders.v1.Order`.
2. **Even a qualified label would not help**, because resolution never uses the
   label — `declRef{label: name}` stores the **bare** name (`code.go:599`, see the
   comment at `code.go:78`) and that is what `byName` is keyed on
   (`code.go:328`). So qualification changes the display label and nothing about
   collision.

⇒ Proto needs a **Go-side** change regardless of pack-vs-embedded. The mitigation
I proposed does not exist. This is why the item is parked rather than
"shipped with a guard".

### Measured damage (real repo, real grammar pack)

Built `grammar build https://github.com/mitchellh/tree-sitter-proto` (1.7 MB
wasm) and ran it against Apache **beam**:

- **Collision rate: 66.6% repo-wide** (373 of 560 proto names already exist as
  code decl names); **83.3%** in `sdks/go` where proto and generated Go co-live.
- **Edge loss: 126 correct `calls` edges destroyed = 1.10%** of an 11,501-edge
  baseline — python-simulation and the real pack run agree exactly.
- **Plus 1 invented WRONG edge**, with zero restructuring:
  `serialize.go::encodeType --calls--> v1.proto::Type.ChanDir`.
- The loss lands on **hand-written** API, not generated code: 60 edges in
  `trigger.go`, 41 in `teststream.go`, 22 in `*.pb.go`.
- The grammar's auto-suggested mapping was unusable anyway (`decls` = `{enum}`
  only — no message/service/rpc).

Destroying 126 correct edges and inventing a false one to gain proto nodes fails
H3 outright.

Options:
- **P4a** — ship proto as a downloadable/committed **pack** in `grammars/`
  (like kotlin/swift/dart). No binary change, no wasm size cost.
- **P4b** — promote proto to an **embedded** grammar (wasm bundle grows).
- **P4c** — do not ship proto at all until qualified labels are expressible.
- Requirement for P4a/P4b: a golden fixture pinning a **proto-alongside-
  generated-code** repo, so an edge-loss regression fails the golden net.

Open: can a pack mapping produce a qualified label, or does qualification need
Go-side support? If it can't, P4a is unsafe and P4 becomes P4b-or-nothing.

## The island problem — P5 is one instance of a general gap

Owner's follow-up: *"what about the postgres specs and all stuff — will that be
mentioned in skills on how to query effectively the specs, code and all stuff?"*

Investigating that surfaced the **same** structural gap in three more places, and
it reframes P5. Two facts, both verified in code:

1. **A connector structurally cannot link to code.** `Connector.Capture(ctx, url)`
   (`internal/sources/registry.go:24-29`) receives *only* a URL. It never sees the
   code batch, so it can only emit edges inside its own subgraph. Measured
   connector output: kinds `database/schema/table(r)/column/collection/topic/
   stream/cluster/bucket/prefix/key_prefix/consumer_group/api/server/path/
   operation`, relations `contains` (14), `references` (3 — FK column→table,
   `postgres.go:299`), `uses` (1).
2. **`deplink` is the ONLY cross-lane linker** (`internal/app/multimodule.go:729`),
   and it bridges exactly one pair: code `module://` imports → `dep:` nodes.
   Native-source batches are merged by `internal/sources/run` and passed to **no**
   linker at all.

⇒ An ingested postgres schema, kafka topic list, or OpenAPI spec is an
**island**: internally well-structured, with **zero edges to the code**. So
*"which code writes to the `orders` table?"* is unanswerable today, for the same
reason *"what reads this config key?"* is.

The sharpest demonstration — an OpenAPI spec and the Flask code implementing it
produce **two `route` nodes with the identical label and no edge between them**:

```
GET /orders  [route]  app.py::route:GET /orders       L4-L6
GET /orders  [route]  openapi.yml::route:GET /orders  L6

edges touching them:
  app.py::route:GET /orders      -handles-> app.py::list_orders
  openapi.yml::route:GET /orders -handles-> listOrders
  (no edge between the two routes)
```

Duplicate, unlinked twins. An agent asking "is the spec implemented?" or "which
handler serves this?" gets two competing answers and no way to join them — even
though the labels are byte-identical, which is the easiest join imaginable.

So P5 is better scoped as **one cross-domain linker producer** (a `deplink`
sibling — its own producer, its own Replace lifecycle, `INFERRED` +
`synthesized_by`, ambiguous dropped) with independent instances:

| instance | join key | value |
|---|---|---|
| **P5a** code → `config_key` | config key literal in source | "what breaks if I change this property" |
| **P5b** spec `route` ↔ code `route` | identical label (`GET /orders`) | "is the spec implemented / who serves it" — **cheapest, highest confidence** |
| **P5c** code → `table`/`column` | table name in SQL strings / ORM model names | "which code touches this table" |
| **P5d** code → `topic`/`bucket` | topic/bucket literal in source | "who publishes to this topic" |

P5b is the standout: an exact label match, no heuristic, and it collapses a
visible duplicate. It is also the only one that needs **no** new string-literal
indexing. Ranked ordering to be set by the spike, but the hypothesis is
**P5b first, cheap and safe; P5a next (depends on P3's dotted paths for
precision); P5c/P5d after.**

## P6 — the skills never teach the QUERY side of sources

Also from the owner's follow-up, and confirmed: the bundled skill documents
*ingestion* thoroughly (`references/sources.md`, 104 lines — schemes, env-var
rule, TTL, skip semantics, logical-shape promise) and the **query side not at
all**.

Grepped `internal/skills/bundled/ctx-optimize/SKILL.md`: none of the source node
kinds (`table`, `column`, `schema`, `topic`, `bucket`, `operation`, …) appear
anywhere. The only `--kind` example in the whole skill is `nodes --kind service`
(SKILL.md:172). So an agent that has just ingested a postgres schema has been
told how to *capture* it and nothing about how to *interrogate* it — it does not
know `nodes --kind table` is the move, that FK structure is `--relation
references`, or that `card <table>` works on a table node.

This is a pure documentation gap: cheap, zero risk, and it multiplies the value
of work already shipped. It should ship **regardless** of what happens to P1–P5.

Options:
- **P6a** — add a "querying ingested sources" section to `references/sources.md`
  plus one hot-path row in `SKILL.md` and a matching `<route>` in
  `activation-routing.xml`; mirror into `internal/project/templates/instructions.md`
  (the committed usage card).
- **P6b** — P6a plus a worked cross-domain example per source type.
- Must state honestly in the docs that source subgraphs are **islands today**
  (no code↔table edges) so an agent doesn't promise a join the graph can't do —
  and this text is what P5 would later get to delete.

## P5 — `reads_config` and friends: the edges that make sources *connected*

Not on the owner's original list; it is what "connect better" actually requires.
A `reads_config` edge from a code decl to a `config_key` node, resolved by
matching config-key literal strings appearing in source against `config_key`
labels. See "The island problem" above for the generalized framing (P5a–P5d).

This is what makes *"what breaks if I change `orders.retry.maxAttempts`?"*
answerable via `affected`, and it is what makes P3's dotted paths worth more than
cosmetics — the dotted path is the string that actually appears in
`@Value("${orders.retry.max-attempts}")` / `os.getenv` / `viper.GetString(...)`.

Options:
- **P5a** — literal-string match: index string literals from code, match against
  `config_key` labels, emit `reads_config` at `INFERRED` confidence.
- **P5b** — framework-aware recognizers only (`@Value`, `@ConfigurationProperties`,
  `viper.Get*`, `os.environ[...]`) — fewer, higher-confidence edges.
- **P5c** — defer entirely.

Risks: the code extractor does **not** currently index string literals (the
UserPromptSubmit hook doctrine even says literal strings are grep's job), so P5a
may mean new node/collection work and false positives across a monorepo where
`name` or `port` appears in prose. Confidence labelling must stay honest
(`INFERRED`, ambiguous dropped) — same discipline as `calls`.

## Ranking to be settled by spikes, not by argument

Hypothesis going in (to be confirmed or killed in `spikes.md`):

| item | expected value | risk | can it drop existing edges? |
|---|---|---|---|
| P6 skills query-side docs | high (unlocks shipped work) | **none** | no |
| P5b spec↔code route link | high | low (exact label match) | no (new relation) |
| P2 py/rust deps | high | low | no |
| P1 XML packs | medium | low | no |
| P3 yaml paths | medium | medium (IDs move) | no |
| P5a code→config_key | **highest for "connect"** | high (precision) | no (new relation) |
| P5c/P5d code→table/topic | high | high (precision) | no (new relation) |
| P4 proto | medium | **high** | **yes** |

Docs first (P6, free); exact-match linking next (P5b); safe-and-additive after
(P1, P2); ID-moving next (P3); heuristic linking (P5a/c/d) judged on measured
precision; risky last (P4).

## Questions for the owner

1. **P6** — ship the skills query-side docs this round? (Recommended yes: zero
   risk, and it makes the postgres/kafka/OpenAPI work already shipped actually
   usable by an agent.)
2. **P5b** (spec route ↔ code route, exact label join) — ship now? It is the
   cheapest real "connect" win and removes a visible duplicate-node wart.
3. Is the rest of **P5** (a/c/d — heuristic literal matching) in scope, or a
   later round? It delivers the most and is the only lane that can emit *wrong*
   edges.
4. **P4**: pack (P4a) or embedded (P4b)? And is proto worth the collision-guard
   work at all, or park it (P4c)?
5. **P3**: full dotted paths (P3a, bigger graph, IDs move) or honor the
   documented top-level-only intent (P3b, small graph)?
6. **P2** scope: Python+Rust only, or Ruby+PHP too?
