# ADR — core language coverage: shell + ps1 in the box, at zero runtime cost

Status: **DRAFT** — 2026-07-25. Needs owner sign-off before any code.

Owner's two requirements, stated together:

> *"the core should always be there — java, js, python, c#, go, rust, c, cpp and
> the related make files, shell scripts, ps1 all should be there"*
> *"i dont want performance to be affected any way"*

They collide today. This ADR is about the option that satisfies both.

## Measured coverage audit (probe repo, one file per language, current binary)

| file | result |
|---|---|
| `.go` `.py` `.js` `.jsx` `.ts` `.java` `.cs` `.rs` `.c` `.cpp` `.hpp` | **OK** — file + decl nodes |
| `Makefile` | **OK** — `config` + `config_key` + `task:make:build` nodes |
| `.h` | **file node only — NO decls** (see G3) |
| `build.sh` | **NO NODES** |
| `script.bash` | **NO NODES** |
| `deploy.ps1` | **NO NODES** |
| `stub.pyi` | **NO NODES** |

So of the owner's core list, **shell and ps1 are the real holes**; make and every
named language is already covered. `zig` and `sql` are also embedded.

## Why adding them to the bundle violates the perf constraint

`internal/extract/code/wasm.go:22` — **one** `//go:embed treesitter.wasm`, a
single monolithic module containing all 12 grammars, compiled as a unit. Per the
lever-2 ADR comment at `wasm.go:48-53`: *"compiling the 32MB tree-sitter module
is ~225ms and dominated every gather/re-sync"*, now paid once per machine per
binary version via the disk-backed wazero cache.

Consequence: grammar compilation is **not** per-language lazy. Growing the
monolith raises that one-time compile for **every user on every binary upgrade**,
including those who never open a shell script — plus binary and npm download
size. That is a real, measurable regression for people who gain nothing.

## Options

**A — add bash + powershell to the embedded monolith.** Simplest; gives "always
there". Costs everyone a bigger first-gather compile and a bigger download.
**Rejected against the perf constraint** unless a spike shows the delta is
negligible.

**B — ship them as packs in `grammars/`** (the kotlin/swift/dart precedent).
Zero runtime cost — a pack's wasm is separate and only compiled if present in the
grammars dir. But it needs a copy-in step, so it is *shipped*, not *always
there*. Fails the requirement as stated.

**C — separate embedded modules, compiled lazily per language (RECOMMENDED).**
Give bash and powershell their own `//go:embed` wasm modules, compiled on first
sight of a matching file extension and cached like today. Then:
- they are **always there** (in the binary, no copy step) — requirement 1 ✓
- a repo with no `.sh`/`.ps1` pays **nothing**: the module is never compiled —
  requirement 2 ✓
- only binary/download size grows.

C also opens a follow-on win: the same mechanism could later **split the existing
monolith**, so a Go-only repo stops paying to compile 11 grammars it never uses —
turning today's 225ms into a fraction of it. That would make this a net perf
*improvement*, not a cost.

## Spike required before building (perf claim must be measured, not assumed)

1. Build the bundle with bash + powershell added; report the wasm size delta, the
   binary size delta, and the monolith compile time before/after (option A's true
   cost — it may be small enough to prefer A's simplicity).
2. Prototype option C's lazy per-module path; confirm a repo with **no**
   `.sh`/`.ps1` shows **zero** added time vs today (the load-bearing claim).
3. Measure the follow-on: compile time for a Go-only repo if the monolith were
   split. If that is a large win, this ADR should grow to include it.
4. Confirm the shim ABI: language IDs are `shim.c` order (`langs.go` comment), so
   adding grammars must not renumber existing IDs — a renumber would silently
   mis-parse every language. Pin it with a test.

## Grammar viability — verified per language, do NOT assume from a registry entry

Checked each candidate's `node-types.json` for declaration-shaped nodes:

| language | decl node types | verdict |
|---|---|---|
| bash | `function_definition` | **viable** |
| powershell (`airbus-cert/tree-sitter-powershell`) | `function_statement`, `class_method_definition`, `class_property_definition` | **viable** |
| ruby | `method`, `class`, `module`, `singleton_method` | viable (registry) |
| php | `function_definition`, `method_declaration` | viable (registry) |
| **elixir** | **only `call`, `do_block`, `stab_clause`** | **NOT viable — see G1** |
| perl | `node-types.json` not found at the expected path | unverified |

## Gaps found alongside, each needing its own call

**G1 — `elixir` in our registry is a door to junk (honesty defect, already
shipped).** Elixir is macro-based: `def`/`defmodule` are `call` nodes, so there
is no declaration node type — the same wall that killed the Clojure/cljgo idea
(mapping `call → function` makes every function call a declaration; measured on
cljgo: `(defn fetch-user …)` emitted a function named **`defn`**, and
`fetch-user` never appeared at all). `KnownGrammars` currently advertises
`elixir` as a one-command add. Either drop it, or flag it in `languages list`
with what it can and cannot produce. **Recommend: drop it** — an entry that
yields wrong data is worse than absence.

**G2 — `.pyi` yields nothing.** Python stub files are ignored because `.pyi` is
not in the python grammar's `Exts` (`langs.go:56`). One-line fix using the
grammar already embedded: **zero bundle cost, zero cost for repos with no
`.pyi`**. The only effect is parsing files that exist and were previously
skipped.

**G3 — C/C++ headers produce no declarations.** `int header_decl(void);` in a
`.h` yields a file node and nothing else, because the mapping recognizes
`function_definition` but not `declaration`. For C and C++ the public API *lives*
in headers, so this is a substantive hole in two languages the owner lists as
core. Fixing it is a mapping change (no new grammar, no size cost) but it **adds
nodes**, so golden snapshots and the judged scoreboard must be re-measured.

**G4 — `grammar build` reports success for a pack that cannot load.** When the
auto-suggested `decls` comes out empty, it prints "pack ready … next
`ctx-optimize add` picks it up", and `add` then hard-errors with
`name, exts and decls are required`. Reproduced on both tree-sitter-clojure and
tree-sitter-cljgo. It should fail loudly at build time. Also: built from a URL it
seeds `exts` from the repo name (`.cljgo`, `.clojure`) rather than real
extensions.

## What ships regardless — the README/skill coverage table

Owner: *"if you have mentioned in readme thats okay."* Independent of A/B/C, the
docs must state coverage honestly in one table: the embedded set, the shipped
packs (kotlin/swift/dart), the one-command registry names, the
explicit-URL cases (powershell, perl), and **the languages a grammar pack cannot
serve at all** (the homoiconic family: Clojure, EDN, Elixir) with the reason. That
last row is the one that saves a user from building a pack that emits garbage.

## NEW REQUIREMENT — "ensure any enterprise can adapt WITHOUT adapters"

Owner, 2026-07-25: adapters stay the escape hatch, but a normal enterprise stack
must work **out of the box**. That is a real narrowing of the earlier
"let them add adapters for what they want" call, and it **changes one verdict I
already recorded today** (see E4).

Measured on a probe repo of enterprise-typical files, current binary:

| file | result |
|---|---|
| `helm/values.yaml`, `helm/Chart.yaml`, `playbook.yml` (Ansible) | OK — `config` + `config_key` |
| `main.tf` (Terraform) | **NO NODES** |
| `Jenkinsfile` | **NO NODES** |
| `app.groovy` | **NO NODES** |
| `web.config` (.NET) | **NO NODES** |
| `openapi.json` | **NO NODES** |
| `helm/templates/deploy.yaml` | config keys only — **k8s lane did NOT recognize it** |

### E1 — Terraform / HCL: the biggest enterprise hole

Infrastructure-as-code is near-universal in enterprise and we emit **nothing**.
The right shape is probably a native recognizer (like the k8s lane) emitting
`resource` / `module` / `variable` / `output` nodes with provider metadata, not a
grammar pack — HCL is config-shaped, and `resource "aws_s3_bucket" "orders"`
carries a two-part identity a decl mapping cannot express. Worth its own ADR.

### E2 — In-repo OpenAPI JSON specs

`openapi.json` yields nothing: the in-repo route lane is YAML-only, and the
JSON-only path is the *connector*, which requires `add <ENV_NAME>`. So a team
that commits `openapi.json` (the majority — 4 of 5 real specs found in a spike)
gets no routes without configuring a source. Extending the route lane to accept
JSON specs is small and squarely "enterprise without adapters".

### E3 — Helm templates defeat the k8s lane

`helm/templates/deploy.yaml` has `kind: Deployment`, yet produced only generic
config keys and **no k8s `resource` node** — the templated
`name: {{ include "orders.fullname" . }}` breaks recognition. Helm is how
enterprises actually ship k8s, so the k8s lane effectively doesn't apply to their
deployment manifests. Options: recognize `kind:` even when the name is templated
(emit the resource with a templated-name marker), or skip `templates/` and read
`Chart.yaml`/`values.yaml` as the chart's identity. Needs a decision, not a guess.

### E4 — enterprise XML: RAISED, then DECLINED by the owner ("remove xml")

I flagged that the XML rejection in `2026-07-25-structured-formats` rested on
**workspace-local** prevalence (2 `logback*.xml`, 8 of 9 `build.xml` vendored in
chromium, zero Spring beans files) and that this does not generalize to
enterprise Java/.NET estates, where Spring XML, `web.config`, `logback.xml` and
`persistence.xml` are ordinary.

**Owner decision 2026-07-25: "remove xml" — do not pursue it.** XML config is
out of scope for the enterprise push. The earlier rejection therefore stands as
final, on the owner's call rather than on the prevalence argument.

Explicitly UNAFFECTED — existing XML handling stays, because removing it would
break Maven and .NET dependency extraction outright:
- `pom.xml` (`maven.go`) and `.csproj`/`.sln` (`dotnet.go`, stdlib
  `encoding/xml` → `dep:nuget/*` + `depends_on` project edges)
- the manifest-pack `"format": "xml"` selector lane
- `.xml` remains ABSENT from `configExts` (P1b stays dead: 25,091 nodes at
  97.2% markup junk).

### E5 — Groovy / Jenkinsfile

`.groovy` and `Jenkinsfile` yield nothing. A grammar pack could serve `.groovy`
classes; Jenkinsfile `stage('x')` blocks are more naturally *tasks* (the devenv
lane's shape). Lower priority than E1–E3 but on the list.

### What already serves enterprise well (do not re-litigate)

Maven + Gradle + `.sln`/`.csproj`/NuGet + npm + pip/poetry/uv + Cargo + go.mod;
plain k8s manifests; Dockerfile/compose; Taskfile/Makefile/justfile; postgres /
mysql / mssql / mongo / redis / kafka / nats / s3 / OpenAPI as native sources.
Those cover a large majority of an enterprise repo's *build and dependency*
surface with no adapter.

## OWNER REQUIREMENT (2026-07-25) — scripts and build files by default

> *"also need shell script ps1 cmd bat files by default"*
> *"also taskfiles makefile build.gradle and other building files"*

### S — shell / ps1 / cmd / bat, in the box

`bash` and `powershell` grammars are viable (verified decl node types:
`function_definition`; `function_statement` + `class_method_definition`), so they
go in via **option C** (own embedded module, compiled lazily on first matching
file) — the only route that is "by default" AND costs a shell-free repo nothing.

**`.cmd` / `.bat` are different: there is no established tree-sitter grammar.**
They also have almost no declarative structure — no functions, just `:label`
blocks, `CALL`, and `GOTO`. So they need a tiny **native scanner**, not a
grammar: emit the file node, a node per `:label`, and `CALL other.bat` /
`CALL :label` edges. Honest expectation: this is thin value compared to shell —
worth saying so rather than implying parity.

### B — build files: measured coverage, and the real gaps

| file | today |
|---|---|
| `Taskfile.yml` | **OK** — `config` + `config_key` + `task` |
| `Makefile` | **OK** — `config` + `config_key` + `task` |
| `justfile` | **OK** — `task` |
| `build.gradle` | **PARTIAL** — deps yes, see B1/B2 |
| `settings.gradle` | config keys only (no module linkage) |
| `Dockerfile` | `config` only — no stages/targets |
| `CMakeLists.txt` | `document` only — no targets |
| `build.sbt` | **NO NODES** |
| `Rakefile` | **NO NODES** |
| `BUILD.bazel` | **NO NODES** |
| `meson.build` | **NO NODES** |

**B1 — gradle map-notation dependencies are missed.** String notation works
(`implementation 'org.slf4j:slf4j-api:2.0.9'` → `dep:maven/org.slf4j:slf4j-api`),
but the map form
`api group: 'com.google.guava', name: 'guava', version: '32.1.3-jre'`
produced **no node**. Map notation is common in enterprise Gradle, so this is a
silent dependency hole in a build file the owner named explicitly.

**B2 — gradle tasks are not extracted at all.** `task hello { … }` yields
nothing, while Make, Taskfile, justfile and npm all emit `task` nodes. That is an
inconsistency in exactly the "what can I run in this repo" question the task
lane exists to answer.

**B3 — Docker / Compose: owner confirmed in scope ("yes docker file docker
compose file and all stuff"). Measured today, and it is worse than "shallow".**

`Dockerfile` → **one `config` node**, nothing else. No stages
(`FROM golang:1.22 AS builder`, `AS runtime`), no base images, no exposed ports.

`compose.yaml` → a `config` node plus **17 flat `config_key` nodes**, which is a
bag, not a model:

```
config_key  api        config_key  image     ← three separate nodes
config_key  db         config_key  image       all labelled "image"
config_key  cache      config_key  image
config_key  services   config_key  volumes   ← two labelled "volumes"
config_key  depends_on config_key  volumes
config_key  ports      config_key  networks  …
```

Three consequences, all measured:
1. **Services are not services.** `api`, `db`, `cache` are indistinguishable from
   `ports` or `environment` — same kind, same shape.
2. **The images are LOST.** Only the *key* `image` is captured three times; the
   values `ghcr.io/acme/api:1.2.3`, `postgres:16`, `redis:7` are nowhere. So
   "what images do we run" is unanswerable from compose — while the **k8s lane
   already answers it** (`uses_image` → `image:` nodes). Same fact, two lanes,
   one blind.
3. **`depends_on` exists as a key but not as an edge**, so the service dependency
   graph — the entire point of compose — is not in the graph.

Good news for the design: **no new vocabulary is needed.** `service` and `image`
kinds and `depends_on` / `uses_image` relations already exist for k8s. Compose
recognition is a natural extension of `k8s.go`'s shape:
`service` node per entry under `services:`, `uses_image` → the shared `image:`
node, `depends_on` edges between services, ports as metadata. Dockerfile: a node
per build stage with its base image, `uses_image` edges, and — the join worth
having — a compose `build: ./api` edge to the Dockerfile it builds.

Secret discipline note: `environment: DB_URL: postgres://db:5432/app` is today
captured as the KEY only, value not stored — correct, and the compose recognizer
must keep it that way (identity only, never env VALUES).

**B4 — CMake / sbt / Rake / Bazel / meson**: zero. Priority call needed; CMake
matters most for the C/C++ core the owner listed, Bazel for large enterprise
monorepos. All four are line/expression-shaped — native recognizers, not
grammars.

## GOVERNING RULE (owner, 2026-07-25) — literal only

> *"try only whatever possible — if there is 1% chance wrong, no need."*

**Emit a fact only if it is read LITERALLY out of the file. If producing it needs
a guess, skip it.** Under-claiming is always the correct failure mode. This
supersedes any "nice to have" reasoning below and it is a stricter bar than
`INFERRED`-with-confidence: an edge we cannot be sure of simply does not exist.

Applying it to everything currently on the table:

**PASSES — pure literal reads, zero inference**

| item | why it is certain |
|---|---|
| compose `service` nodes, image values, `depends_on` edges, ports | every one is a literal string in the file |
| Dockerfile stages (`AS name`) + base images (`FROM`) | literal |
| gradle map-notation deps (B1) | literal strings |
| gradle `task <name>` (B2) | literal declaration; dynamically-created tasks are simply missed (under-claim) |
| Spring bean `id`+`class`, appender `name`+`class`, servlet `name`+`class`, `url-pattern`, Ant `target`/`depends` | literal attributes / element text |
| shell + ps1 function definitions | deterministic tree-sitter parse of a real decl node |
| SQL `column` nodes + FK `references` edges | literal DDL |
| `.pyi` (G2) | parsed by the already-embedded grammar |
| compose `build: ./api` → Dockerfile | filesystem path resolution, deterministic |
| Terraform `resource "type" "name"` (E1) | literal |

**FAILS — needs a guess, therefore OUT (and this rule now confirms every
rejection already recorded today)**

| item | the guess, measured |
|---|---|
| `reads_config` code→config_key (P5a) | **1.9% precision** on today's flat labels |
| spec↔code route join (P5b) | **0%** raw join rate on real repos |
| proto grammar (P4) | destroys 126 correct `calls` edges, invents 1 |
| homoiconic packs (Clojure/EDN/Elixir) | labels every decl after the macro; real name never appears |
| `.xml` in `configExts` (P1b) | 97.2% markup junk |
| **Helm templated names (E3)** | `name: {{ include "orders.fullname" . }}` — the real name is NOT in the file. Emit only the literal `kind:`/chart facts; **never invent the resolved name.** |

**Needs a spike to decide (do not assume):** the XML→code link
(`bean class="com.acme.OrderService"` → the Java class node). It is a
**fully-qualified** name match, and FQN uniqueness is a language guarantee — far
stronger than the bare-name `calls` resolver. Likely passes the bar, but it must
be proven: if a spike finds ANY ambiguous match, drop the link and keep bean
recognition only. `.cmd`/`.bat` labels pass the bar but carry thin value; ship
only if cheap.

## Questions for the owner

1. **A, B or C?** Recommend **C** — the only one meeting both requirements, and
   it sets up a real perf win by splitting the monolith.
2. Should the monolith split be in scope now, or a follow-up?
3. ~~**G1**: drop `elixir`?~~ **DONE** — owner said "no need elixir"; removed from
   `KnownGrammars`, from `docs/languages-packs.md`, and `suggest.go` now documents
   why the homoiconic family (Clojure/EDN/Elixir) is excluded on purpose. The
   owner will add a grammar by URL himself if he ever wants one.
6. ~~Enterprise priority order?~~ **DECIDED by the owner 2026-07-25**
   (*"yes need openapi json and all those will be priority than"*):

   1. **E2 — in-repo OpenAPI JSON specs** (highest priority, smallest change)
   2. **E1 — Terraform / HCL** (biggest gap; own ADR)
   3. **E3 — Helm templates vs the k8s lane**
   4. **E5 — Groovy / Jenkinsfile**
   5. ~~E4 — enterprise XML~~ **DROPPED** ("remove xml")
4. **G3**: fix C/C++ header declarations? It is real core coverage but moves the
   golden baseline.
5. **G2** (`.pyi`) and **G4** (false "pack ready") — trivial and safe; take them?
