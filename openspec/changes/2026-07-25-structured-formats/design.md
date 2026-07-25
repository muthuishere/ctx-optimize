# ADR — structured formats: ship HONESTY, redirect coverage to adapters

Status: **ACCEPTED** — 2026-07-25 (owner-directed scope). Finalizes
`proposal.md`; all four spike files (`spike-p1.md`, `spike-p2.md`, `spike-p3.md`,
`spike-p45.md`) hold the measurements. Deterministic, no LLM, stdlib only.

## The decision, up front

The owner's scope call governs: **"we dont want to complete every — let them add
adapters for what they want, we will take care of honest stuff."**

Coverage is not the goal. The adapter / manifest-pack / grammar-pack doors are the
user's lane for anything exotic. **Our job is that what we emit is honest:**
deterministic, correctly located, not junk, and documented without overclaiming.

Every item was filtered on: **H1** today's output is wrong or nondeterministic ·
**H2** we claim a capability we don't have · **H3** shipping it would make the
graph *less* honest (⇒ must not ship).

### SHIPS (7)

| # | item | test | evidence |
|---|---|---|---|
| S1 | YAML nested-key guard is whitespace-dependent | H1 | same file → **9 nodes trimmed vs 4 with trailing spaces** |
| S2 | config keys emitted from inside block scalars | H1/H3 | **9 junk nodes** from inside `postgresql.conf: \|` in real k8s files |
| S3 | manifest-pack nodes carry no `Location` | H1 | verified in raw ndjson; uncitable, unverifiable |
| S4 | manifest-pack node IDs are namespace-blind | H1 | multi-rule pack silently dedups: `appender-ref/@ref` → 0 nodes co-shipped, 1 in isolation |
| S5 | documented pack example yields 0 | H2 | `packs.go:17-18` `"target/@name"` → 0 on real Ant; `project/target/@name` → 19 |
| S6 | `grammar build` ignores `CTX_OPTIMIZE_GRAMMARS` | H1 | loader honors it, builder writes elsewhere |
| S7 | Python/Rust dependency extraction (P2a) | H2 | `deps` prints `(0 dependencies)` where **61 are declared** |
| S8 | skills/docs: query side of sources + the traps (P6) | H2 | 0 source node kinds documented; island-ness and pack traps unstated |

(S8 is listed 8th; "SHIPS (7)" counts code changes — S8 is docs.)

### DOES NOT SHIP — recorded with the number that killed it

| item | verdict | the number |
|---|---|---|
| **P4 proto grammar** | **PARKED** | **126 correct `calls` edges destroyed (1.10%) + 1 invented wrong edge** on real Apache beam; 66.6% name-collision repo-wide, 83.3% in `sdks/go` |
| **P1a XML packs** | **OUT → user's pack door** | prevalence: 2 `logback*.xml` (one project), 8 of 9 `build.xml` vendored in chromium, 3 `web.xml` all chromium, **0 spring beans anywhere** |
| **P1b `.xml` in configExts** | **REJECTED (H3)** | 506 real XML files → **25,091 config_key nodes, 97.2% markup junk** (`<a` 9,435, `xmlns` 952) |
| **P3a dotted config paths** | **PARKED as a pair** | `query.go:251` ×0.2 penalty on dotted non-callable labels ⇒ `query "server port"` falls **rank 2 → rank 5+**, below unrelated functions |
| **P5b spec↔code route join** | **REFUTED** | raw-label join **0/63 and 0/47** on the only real repos; `GET /` in 20 route files |
| **P5a code→config_key** | **BLOCKED** | 100% precision on dotted labels but **1.9% on today's bare leaves**; needs P3a, which needs the ranker change |
| **P2b Ruby/PHP deps** | **OUT → adapter door** | 1 Gemfile + 3 composer.json, 3 of 4 vendored in chromium |

### Two claims in `proposal.md` that the spikes REFUTED — corrected, not quietly dropped

1. **"Qualified labels prevent the proto collision" — FALSE.** A pack cannot emit
   a qualified label (`packConfig` has only `name/exts/decls/names/calls/imports`,
   `langs.go:224-231`; `qual` is built in Go at `code.go:562-580`), **and it would
   not matter anyway** — resolution keys on the **bare** name
   (`declRef{label: name}` at `code.go:599`, keyed at `code.go:328`). Proto needs a
   Go-side change either way. The proposed mitigation does not exist.
2. **"Spec↔code route join is the cheapest high-confidence win" — FALSE.** 0% raw
   join rate on every real repo; my scratch repo joined only because I hand-wrote
   both halves. The real prerequisite is Express/NestJS mount-prefix resolution.

## What changes in code

### S1 — whitespace-dependent nested-key guard

`internal/extract/markdown/markdown.go` (~L157-159). Condition today:

```go
if line != t && strings.TrimLeft(line, " \t") != line { continue }
```

`t` is `line` with **trailing** whitespace trimmed, so a nested line is skipped
only when it happens to carry trailing whitespace.

There are exactly two *consistent* fixes: **(a)** index every depth (delete the
broken guard, correct the stale comment) or **(b)** honor the comment and index
only top level.

**Decision: (a) — index every depth, consistently. Fix the COMMENT, not the
behavior.**

```go
// (a): drop the guard entirely; keys at every depth are indexed.
```

Reasoning, from `spike-p3.md`:

- The defect affects **6 of 172,665** indented key lines (0.0035%). Option (a)
  changes the graph for those 6 lines **only** — the minimum edit that removes the
  nondeterminism, with essentially zero golden churn and no retrieval risk.
- Option (b) is a large **removal**: on a representative `application.yml`, 6 of 9
  config keys are nested, so ~⅔ of config nodes would disappear. Most keys in real
  YAML are nested, and with P3a parked there is nothing to replace them — that
  trades a 0.0035% determinism bug for a large findability loss, and would very
  likely drop the judged scoreboard.
- Honesty is satisfied either way (both are deterministic). Between two honest
  options, take the one that destroys less.

⚠️ The stale comment ("Deliberately shallow … top-level KEY sections") must be
rewritten to describe what the code actually does — a comment that misdescribes
behavior is its own H2 defect, and it is what hid this bug.

The remaining flat-label ambiguity (bare `port`, `name`) is **not** fixed here;
it is P3a, parked behind the query-ranker change, and the spike quantified it
(37.4% of this repo's config_key nodes carry a label that also exists in another
file). Do not paper over it in the comment.

### S2 — block scalars are opaque text, not config structure

Same file. A `key: |` / `key: >` (with any indicator: `|-`, `>+`, `|2`) opens a
literal block whose body is **data**, not YAML structure. Track the block's
indentation and skip every line more-indented than the opening key until the block
closes. Nine junk nodes in real k8s files today come from inside an embedded
`postgresql.conf`.

Note S1 and S2 interact, and with S1 resolved as option (a) — index every depth —
**S2 is load-bearing, not cosmetic**: every line inside a block scalar is now
reliably indexed unless S2 skips it. S1 alone would make the junk *consistent*
instead of removing it. Implement S2 in the same change as S1.

### S3 — pack nodes must carry `Location`

`internal/extract/manifests/packs.go:216-220` emits the `task` node with no
`Location`, unlike every other producer. A node without file:line cannot be cited
or passed to `verify` — the store's core contract.

Requires the selectors to report a line number. `xmlSelect` uses
`encoding/xml`, whose `Decoder.InputOffset()` gives a byte offset convertible to a
line; `yamlSelect` already goes through `yamlwalk`, which carries `Num`. If a
format genuinely cannot yield a line, emit `L1` (the file-level truth) rather than
nothing — never an invented line.

### S4 — pack node IDs must be namespace-scoped

Same file: `id := rel + "::task:" + h.name` while the label is
`br.namespace + ":" + h.name`. The label is namespaced, the ID is not, so two
rules yielding the same name in one file collide and one is silently lost.

**Fix: `id := rel + "::task:" + br.namespace + ":" + h.name`** — ID and label
agree. Changes existing pack node IDs (no bundled packs ship, so blast radius is
user packs only); note it in CHANGELOG.

### S5 — fix the documented example

`packs.go:17-18`: `"path": "target/@name"` → `"project/target/@name"`, and state
explicitly in the package doc that xml selectors are **root-anchored and
exact-depth** (`matches()` requires `len(stack) == len(segs)`, `packs.go:405`) with
**no descendant operator**, and that `*` matches exactly one level.

### S6 — `grammar build` must honor `CTX_OPTIMIZE_GRAMMARS`

The loader honors the env var; the builder writes elsewhere, so a built pack can
land where nothing will load it. Make the builder resolve the same destination the
loader reads. (Also what made the spike's grammar work escape into the user's real
`~/ctxoptimize/grammars` — a real hazard, not theoretical.)

### S7 — Python/Rust dependency extraction (P2a)

New recognizers in `internal/extract/manifests`, plus a **~230-line stdlib TOML
table walker** as a sibling of `internal/extract/yamlwalk` (the spike's prototype
scored **1,639/1,639 declarations across 103 real manifests, 100% precision and
recall vs `tomllib`**, 0.009s).

Requirements, each from a measurement:

1. **Table-anchored matching only** — never an array-of-version-strings
   heuristic. Flask's `[tool.tox.env.tests-min] commands = [["uv","pip","install","blinker==1.9.0",…]]`
   makes a naive scan invent ~12 phantom deps; table-anchored gives the
   hand-counted 31 with 0 extras.
2. **Sources**: PEP 621 `[project] dependencies` + `optional-dependencies`,
   poetry `[tool.poetry.dependencies]` (+ groups), PEP 735 `[dependency-groups]`,
   `requirements*.txt`, Cargo `[dependencies]`/`[dev-dependencies]`/
   `[build-dependencies]` incl. inline-table and `[target."cfg(…)".dependencies]`
   forms.
3. **Namespaces `pypi` / `crates`**, with **PEP 503 normalization**
   (`_`/`.`→`-`, lowercase) — measured to collapse `flit_core`, `poetry_core`,
   `typing_extensions` (166 raw → 165 distinct; one real duplicate avoided in 28
   files).
4. **Lock output must not masquerade as declared deps** (H2, would be a NEW
   honesty failure): 61% of pypi declarations come from `requirements*.txt` and
   **12 of 40 are pip-compile locks**. `isLockfile` only refuses `*.lock`. Detect
   the pip-compile header / fully-pinned-with-hashes shape and either skip or mark
   the nodes so `deps` can distinguish declared from transitive. **Skipping is the
   default** — silence beats a wrong claim.
5. **Scope classes**: add `dev-dependencies` (currently missing beside
   `devDependencies`) and fix the `HasPrefix("test")` rule to catch
   `dependency-groups:tests` (`scopeclass.go`).
6. Multi-line `"""` strings must be skipped (spike found a phantom dep from prose
   in a `description`); the inline dependency-table form
   `dependencies = { a = "^1" }` must parse. Multi-line inline tables are illegal
   TOML — do not chase them.

### S8 — docs: the query side, and the traps in the door we point people at

Because coverage is now explicitly the user's job, the docs must not hand them a
door with a hidden hole. Touch
`internal/skills/bundled/ctx-optimize/references/sources.md` (add a querying
section), `SKILL.md` (one hot-path row), `references/activation-routing.xml` (a
`<route>`), a new `references/extending.md` for the trap list, and mirror the
essentials into `internal/project/templates/instructions.md`.

Must state, all measured:

- **How to query an ingested source.** The kind vocabulary appears nowhere today:
  `database` · `schema` · `table` · **`view`** · `column` · `collection` · `topic` ·
  `stream` · `cluster` · `bucket` · `prefix` · `key_prefix` · `consumer_group` ·
  `api` · `server` · `path` · `operation` · **`securityScheme`**. Relations:
  `contains`, `references` (FK column→table, `postgres.go:299`), `uses`.

  **Three corrections found while writing this (my list above was wrong):**
  1. `view` was missing — emitted by `postgres.go:207-213`, `mysql.go:477`,
     `mssql.go:430` (materialized views carry `materialized: true`). Kinds are
     EXACT: `--kind table` does **not** return views.
  2. `securityScheme` (and a second `schema` kind, for openapi *component*
     schemas) were missing.
  3. **`nodes --kind table --where schema=public` does NOT work** — table nodes
     carry no `schema` metadata key (`postgres.go:206-241`), and `--where`
     resolves top-level fields then metadata (`graphfilter.go:134-166`). The
     correct form is `--where label~public.` since labels are `schema.table`.

  Kinds must be documented **per connector**, not as one flat list: mysql emits no
  `schema` node (database == schema), `consumer_group` is kafka-only,
  `stream`/`server` are nats-only. Also: gradle deps land in the **`maven`**
  namespace (`gradle.go:41`), and `deplink` emits specifically `resolves_to`.
- **Source subgraphs are ISLANDS.** `Connector.Capture(ctx, url)`
  (`registry.go:24-29`) receives only a URL and never sees the code batch;
  `deplink` (`multimodule.go:729`) is the only cross-lane linker and bridges only
  code imports → `dep:` nodes. So there is **no** code↔table, code↔config_key, or
  code↔topic edge. Say it plainly so no agent promises "which code writes this
  table".
- **Spec routes and code routes are separate, unjoined nodes** with identical
  labels (measured 0% join rate) — an agent must not treat one as the other.
  Both sides here are kind `route` from the in-repo **gather** YAML lane
  (`internal/extract/markdown/yamlroutes.go`). The openapi **connector** is a
  different thing entirely: it emits `operation` nodes with different ids.

- **CORRECTION (verified during S8 — the ADR had this inverted).** Earlier drafts
  said "the openapi file lane accepts YAML; JSON specs are rejected". The truth is
  the opposite, and it is split across two lanes:
  - the openapi **connector** (`add <ENV_NAME>` / no-scheme spec path,
    `internal/sources/connectors/openapi.go:6-11,152-158`) is **JSON only** — a
    YAML spec is a loud error naming the adapter lane;
  - the in-repo **route lane** (`extractYAMLRoutes`) is **YAML only**.

  So the "4 of 5 real specs found were JSON" number stands, but the gap it names
  is the **YAML-only route lane**, not a JSON-rejecting connector.
- **Grammar-pack trap**: a pack's `names` yields the BARE identifier, and call
  resolution drops module-wide-ambiguous names, so a pack whose decl names
  duplicate existing ones **silently deletes working `calls` edges** — measured
  **126 lost + 1 invented on beam**. Anyone adding a pack must be told.
- **Manifest-pack trap**: selectors root-anchored, exact-depth, no descendant
  operator; cannot pair two attributes of one element; `emit` is only
  `{dependency, task}`, so many facts have no honest kind → use an adapter script.
- **`deps` ecosystem coverage**: after S7, npm/go/maven/nuget/gradle/pypi/crates.
  Name them, so silence never reads as "no dependencies" again.

## Golden-net requirements

- S1 as decided (option a) should move golden **barely or not at all** — it changes
  behavior only for indented keys carrying trailing whitespace. S2 removes
  block-scalar junk where a fixture has one. Any larger movement than that means
  the implementation drifted from the decision — investigate, don't re-bless.
- S7 needs a **new golden fixture**: a repo with `pyproject.toml` (PEP 621 +
  poetry group), `requirements.txt`, a **pip-compile lock**, and `Cargo.toml`
  (incl. inline-table + `[target.…]`), pinning dep nodes, namespaces, scopes, and
  that the lock contributes nothing.
- S3/S4 need pack-node assertions for `Location` presence and namespace-scoped IDs.
- `task ci` + `task golden` green. Score and speed may only move UP; **if the
  judged scoreboard drops because S1 removes nested keys, escalate — do not lower
  a floor.**

## ESCALATION — the judged scoreboard floor for Newtonsoft was never met

Design instruction above: *"if the judged scoreboard drops, escalate — do not
lower a floor."* It did not drop, but running it surfaced something worse.

| corpus | score | floor | verdict |
|---|---|---|---|
| linux-block | 16.5 / 20 | 16.5 | passes, exactly at floor |
| newtonsoft | **12.5 / 20** | 16.5 | **FAILS by 4.0** |

**This change did not cause it.** Measured the same judged set against a binary
built from HEAD (`0de40e3`, pre-change) in a separate worktree: **also 12.5 / 20,
identical.** Zero movement, so S1/S2/S7 are clean on the scoreboard.

**The floor was never achievable.** It was introduced in `7588bd8`
("floors enforced at 16.5/20") with `min_score: 16.5` for *both* question
sets — the linux-block number applied to Newtonsoft without being measured
against it. Consequence: **`golden.yml` has been RED on every run since
2026-07-20**, and the latest CI log fails on exactly this line
(`SCORE newtonsoft: 12.5 / 20 (floor 16.5)` → `fell below the floor 16.5`).

That is an honesty defect in our own quality gate, and a serious one: the ratchet
that exists to catch regressions has been failing for unrelated reasons long
enough that a real regression would have hidden in the noise. A red gate nobody
can act on is equivalent to no gate.

The three zero-scoring questions are usage-shaped: `N17` "How do I serialize an
object to a JSON string?", `N19` "How do I create a JsonSerializer with
settings?", `N20` "How do I run this project's tests?".

**NOT fixed here, and deliberately NOT papered over by lowering the floor**
(doctrine: nothing may lower it). It needs an owner decision between two
different repairs:

1. **The floor is wrong** — 16.5 was never measured for this corpus. Correct it
   to the measured 12.5 as an explicitly-labelled *correction of a mis-recorded
   number* (not a lowering), restoring a green, trustworthy gate, and file the
   4-point gap as the quality target it was always meant to name.
2. **The product is wrong** — these three questions represent a real retrieval
   gap for "how do I use this library" questions, and the fix is ranking work
   with the floor left red until it passes.

Either way the current state — a permanently red gate — is the one option that
must not continue.

## Found during implementation — recorded, NOT fixed here

1. **Store-level edge dedup silently drops scope-differing `declares` edges.**
   `internal/store/store.go:262` keys edges on `(source, target, relation)` only,
   so a manifest declaring one package in two scopes yields **one** stored edge.
   Measured on flask: the batch carries 40 `declares`, the store shows **35**
   (asgiref 3→1, python-dotenv 3→1, pytest 2→1). **Pre-existing and equally true
   of npm** (`dependencies` + `devDependencies` in one manifest) — not introduced
   by S7, and the information is not lost (`applyScopeAggregates` runs on the
   batch, so the node still reads `asgiref [dev,optional,test]`). Neither the ADR
   nor the spike noticed it, because the spike measured the prototype and never
   went through the store. Adding `scope` to the edge key is an ADR-level
   decision about edge identity — deliberately out of scope.
2. **List-item keys are never indexed by the config lane.** The key parsed from
   `- powershell: |` is `"- powershell"`, which the long-standing
   `ContainsAny(key, "{}\"' ")` guard rejects for containing a space. So keys
   introduced by a list item have never entered the graph. Fixing it would ADD
   nodes — coverage, not honesty — so it is out of scope under the owner's scope
   call. Pinned in a test as current truth so it reads as known, not overlooked.
3. **`corpus-flask` cannot be pinned at "61 declares / 41 distinct".** ADR
   requirements 2 and 4 conflict arithmetically: skipping pip-compile locks
   (requirement 4) removes 21 of those 61. The honest post-change numbers are
   **40 declares / 28 distinct**, and 40+21=61 / 28+13=41 reconciles exactly to
   the skipped lock. A fresh `pydeps/` fixture pins the contract instead.
4. **The block-scalar skip is format-blind.** `extractConfig` receives no file
   extension, so the `key: |` rule applies to any config format. The
   `t[cut] == ':'` requirement confines it to YAML-shaped lines in practice; the
   residual exposure is a `.properties` file using `:` as separator with a bare
   `|` value. Vanishingly rare; plumbing the extension through would widen the
   diff. Flagged rather than papered over.

## Explicitly NOT in this change

Proto (needs a Go-side resolver change), XML packs, dotted config paths + the
query-ranker exemption they require, all `reads_config`/table/topic linking,
Express mount-prefix resolution, Ruby/PHP deps, and **YAML spec support in the
openapi connector / JSON spec support in the in-repo route lane** — each lane
currently accepts exactly one format and rejects the other (see the S8 correction
above). A real gap, separately recorded, not fixed here.
