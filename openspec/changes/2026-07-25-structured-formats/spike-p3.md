# Spike P3 — yaml/toml/properties dotted paths + the whitespace guard bug

Effectiveness spike, 2026-07-25. Zero product-code edits; all work in a scratch
dir. Every number below comes from a command recorded here. Anything not
measured is marked **not measured**.

## VERDICT

**Ship P3a (dotted paths) — but ONLY together with a query-side exemption, and
fix the guard bug as a separate, standalone commit that lands first.**

Ranked, from the numbers:

1. **Fix the guard bug now, on its own.** Its real-world blast radius is
   negligible (6 of 172,665 indented key lines across 3,998 real config files —
   **0.0035%**), so it is not corrupting indexes today. But it makes extraction
   **non-deterministic on invisible characters**: the same `application.yml`
   gathered with trailing spaces on nested lines produced **4 nodes instead of
   9** (measured, diff below). That violates the determinism contract outright
   and the fix is one line. It is independent of the P3a/P3b choice — the guard
   must express *some* intent unambiguously either way.
2. **P3a is nearly free in graph size.** 1,603 vs 1,557 config_key nodes over 41
   real config files = **1.03×**. The proposal's feared "10× config_key
   explosion" does not happen: today's code *already* indexes every depth, so
   P3a **relabels, it does not add**. Claim in the proposal — verified.
3. **P3a is the findability win.** Nodes whose label resolves to exactly one node
   go from **17.1% → 53.3%**; nodes carrying an ambiguous label drop
   **77.8% → 35.3%**. In this repo's own live store, **61 of 163 config_key
   nodes (37.4%)** carry a label that exists in another file (`name` in 3 files,
   `version` in 3, `env`/`build`/`cmds`/`run`/… in 2 each). Today a hit on
   `port` is un-actionable; `management.server.port` is.
4. **BLOCKER, and the real finding of this spike:** `internal/query/query.go:251`
   already downranks *any* dotted label of a non-callable kind by **×0.2**
   ("child-declaration downrank", proof D1), and `config_key` is **not** in
   `callableKind` (query.go:84-88). Today **0 of 163** config_key labels in this
   repo's store contain a dot, so the penalty never fires. P3a makes **every**
   nested key dotted, so it fires on all of them. Measured consequence: in a
   repo holding both config and code, the query `server port` today returns
   `config_key port` at **rank 2**; with dotted labels the config keys fall to
   **rank 5+, below three unrelated functions** (`LoggingLevel` 0.95 >
   `management.server.port` 0.54). **P3a without exempting `config_key` from the
   dotted downrank is a net retrieval regression.**
5. **P3b (true top-level-only) is rejected.** It would delete ~87% of today's
   config_key nodes (172,665 indented vs 39,677 top-level key lines in the
   sample) and with them every answer to "where is `maxAttempts` set" — a large
   destructive change bought for nothing but a smaller graph. The docstring is
   what is wrong, not the behaviour.
6. **No depth cap needed.** Max nesting observed in real files is **7**; the
   longest dotted label is **67 chars**. 98.6% of labels are under 60 chars.
   The hypothetical "9-level 180-char label" does not occur.

Golden cost is contained: **1 golden file, 36 of 104 lines**.

---

## 1. The guard bug

Confirmed verbatim at `internal/extract/markdown/markdown.go:158-160`:

```go
// top-level only: no leading whitespace
if line != t && strings.TrimLeft(line, " \t") != line {
    continue
}
```

`t` is `line` with **trailing** whitespace trimmed, so `line != t` is true only
when the line *has* trailing whitespace. A nested key is therefore skipped
**only if it also carries trailing whitespace** — the comment describes an
intent the code does not implement.

### Real-world reach

`python3 scan_ws.py` — walks `~/muthu/gitworkspace` to depth 5, `.yml/.yaml/
.toml/.properties/.ini`, excludes `node_modules`/`.git`/dot-dirs/vendor/dist,
capped at 4,000 files, applies the same secret-name refusal and 256 KiB cap as
`configFile`:

| metric | value |
|---|---|
| config files scanned | 3,998 |
| lines scanned | 471,684 |
| top-level key lines | 39,677 |
| **indented key lines** | **172,665** |
| indented key lines with trailing whitespace (= inconsistently skipped today) | **6** |
| **% of indented keys affected** | **0.0035%** |
| files with ≥1 skipped key | 4 (**0.1%** of files) |

The four affected files are `llvm-libc/src/include/signal.yaml`,
`beam/.../tour-of-beam/backend/docker-compose.yml`, and two
`linux/Documentation/devicetree/bindings/sound/*.yaml`. So: **latent curiosity
in volume, not an active corruption.**

### Observable symptom — determinism, not volume

Two directories, byte-identical YAML except that every nested line in `wsB`
carries two trailing spaces (what a non-trimming editor leaves). Gathered with
the installed HEAD binary into separate stores:

```
CTX_OPTIMIZE_STORE=$S/storeA ctx-optimize add $S/wsA
CTX_OPTIMIZE_STORE=$S/storeB ctx-optimize add $S/wsB
```

Diff of the node lists (`(kind, id, label, location)`):

```
A nodes: 9   B nodes: 4
--- A(trimmed)
+++ B(trailing-ws)
@@ -1,9 +1,4 @@
 ('config', 'application.yml', 'application.yml', 'L1')
-('config_key', 'application.yml#address', 'address', 'L3')
-('config_key', 'application.yml#level', 'level', 'L5')
 ('config_key', 'application.yml#logging', 'logging', 'L4')
-('config_key', 'application.yml#maxattempts', 'maxAttempts', 'L8')
 ('config_key', 'application.yml#orders', 'orders', 'L6')
-('config_key', 'application.yml#port', 'port', 'L2')
-('config_key', 'application.yml#retry', 'retry', 'L7')
 ('config_key', 'application.yml#server', 'server', 'L1')
```

**5 of 9 nodes vanish** because of whitespace no human can see. A whitespace-
trimming editor, a formatter, or a CI `sed` changes the graph. This is the
argument for fixing it, and it is independent of P3.

---

## 2. Graph-size cost of P3a

Prototype: `proto/main.go` — transcribes today's `extractConfig` key detection
verbatim, and builds dotted paths for `.yml/.yaml` from a **verbatim copy** of
`internal/extract/yamlwalk/yamlwalk.go` (copied, never edited; `Line.Indent` is
exactly the nesting signal the proposal claims) plus section-prefixing for
`.toml/.ini/.properties`. Run over 41 real files (docker-compose ×8, Spring
`application*.yml` ×6, `.github/workflows` ×9, Taskfile ×9, k8s manifests ×4,
`pyproject/Cargo/gradle.properties` ×5).

`$S/proto/spike < files.txt`

| corpus | today keys | P3a keys | ratio |
|---|---|---|---|
| 41 real config files | 1,557 | 1,603 | **1.03×** |
| ctx-optimize's own 10 config files | 282 | 338 | **1.20×** |

**The relabel claim holds.** The +3% is not new depth — it is yamlwalk correctly
seeing keys inside list items (`- name:` in workflows) that the line scanner's
`ContainsAny(t, ":=")`/quote filters drop.

Two files actually **shrank**, which is a precision *gain*: in
`k8s/arm-b-kubevirt/10-postgres-config.yaml` today's scanner emits 17 keys, 9 of
which are lines from inside a `postgresql.conf.append: |` **block scalar**
(`max_connections`, `shared_buffers`, `checkpoint_completion_target` …) — junk
config_key nodes for an embedded file. yamlwalk drops them (`=`, not `: `).
P3a: 8 keys.

### Findability (the actual win)

Label→node resolution over the same corpora:

| corpus | mode | nodes | distinct labels | nodes with an **ambiguous** label | labels resolving to **exactly one** node |
|---|---|---|---|---|---|
| ctx-optimize (10 files) | today | 282 | 105 | 175 (62.1%) | 48 (17.0%) |
| ctx-optimize (10 files) | **P3a** | 338 | 187 | 148 (43.8%) | 112 (**33.1%**) |
| 41 real files | today | 1,557 | 454 | 1,211 (77.8%) | 266 (17.1%) |
| 41 real files | **P3a** | 1,603 | 1,074 | 566 (35.3%) | 854 (**53.3%**) |

Within-file collisions (the `#port-2` slug-suffix cases, indistinguishable to a
reader) collapse **838 → 236** over the 41 files.

### Ambiguity in this repo's live store — the headline number

`~/ctxoptimize/ctx-optimize/graph/nodes.ndjson`, `kind == config_key`:

```
config_key nodes: 163      (4.67% of the store's 3,493 nodes)
distinct labels:  88
AMBIGUOUS labels (same label, >1 file): 15
nodes carrying an ambiguous label: 61  (37.4%)
already-dotted labels: 0
  name 3 files · version 3 · env 2 · build 2 · cmds 2 · desc 2 · lint 2
  tasks 2 · test 2 · run 2 · created 2 · maintainer 2 · schema 2 · status 2 · title 2
```

**37.4% of this repo's config_key nodes are currently un-addressable by label.**
That is the concrete findability win P3a buys. The `already-dotted: 0` line is
also the setup for the §3 blocker.

### Depth distribution — no cap needed

Over all 1,603 P3a labels from the 41 files:

| dotted depth | 1 | 2 | 3 | 4 | 5 | 6 | 7 |
|---|---|---|---|---|---|---|---|
| labels | 139 | 341 | 632 | 333 | 134 | 13 | 11 |

| label length (bucket) | 0–19 | 20–39 | 40–59 | 60–79 | 80+ |
|---|---|---|---|---|---|
| labels | 636 | 812 | 149 | 6 | **0** |

Longest observed:

```
67  spec.template.spec.volumes.cloudInitNoCloud.userData.package_update
66  on.workflow_dispatch.inputs.crabbox_keep_alive_minutes.description
56  services.browser-automation.environment.DEBUG_PORT_START
56  jobs.goreleaser-linux-windows.steps.with.go-version-file
55  spec.postgresql.parameters.checkpoint_completion_target
```

Max depth **7** (a KubeVirt VM manifest), max length **67 chars**, 98.6% under
60. Keeping the existing `len(key) > 80` filter as a `len(path) > 200` guard is
sufficient; a *depth* cap would only mutilate the k8s files that need the path
most.

---

## 3. Retrieval win/loss

Proxy method (no product code touched): the same Spring config expressed twice —
`qflat/application.yml` (nested YAML → today's leaf labels) and
`qdot/application.properties` (natively dotted → exactly what P3a would emit).
Both gathered with the installed HEAD binary; same queries against each.

`(cd $S/qflat && CTX_OPTIMIZE_STORE=… ctx-optimize query "<q>" --json)`

| query | today (flat) top-3 | P3a proxy (dotted) top-3 | verdict |
|---|---|---|---|
| `orders retry max attempts` | `maxAttempts` L29 4.88 · `maxAttempts` L34 4.88 · `orders` L27 2.99 | **`orders.retry.maxAttempts` L12** 1.30 · `payments.retry.maxAttempts` L15 0.99 · `orders.retry.backoff` 0.62 | **WIN** — flat's top-2 are the same label; one of them is *payments*. Unresolvable today. |
| `server port` | `port` L2 2.58 · `port` L22 2.58 · `server` L1 2.58 | `management.server.port` L10 0.63 · **`server.port` L1** 0.63 · `server.address` 0.26 | **WIN on identity, mild LOSS on order** — right answer is rank 2 behind a tie, see below |
| `logging level` | `level` L15 2.99 · `logging` L14 2.99 | `logging.level.org.hibernate` 0.68 · `logging.level.root` 0.68 · `logging.file.name` 0.31 | **WIN** — flat cannot say *which* level |
| `port` (single word) | `port` L2 2.58 · `port` L22 2.58 | `management.server.port` 0.37 · `server.port` 0.37 | **WIN** — both ambiguous flat; dotted names them |
| `management port` | `management` L20 2.99 · `port` L2 2.58 · `port` L22 2.58 | **`management.server.port` L10** 0.73 · `management.endpoints…` 0.37 · `server.port` 0.37 | **WIN** — flat's #1 is the parent key, not the setting |
| `hibernate ddl auto` | `ddl-auto` L13 5.98 · `hibernate` 2.58 · `org.hibernate` 0.52 | **`spring.jpa.hibernate.ddl-auto`** 1.26 · `logging.level.org.hibernate` 0.37 | WIN (flat also works here — leaf name is already unique) |
| `payments retry` | `payments` L32 2.99 · `retry` L28 2.58 · `retry` L33 2.58 | **`payments.retry.maxAttempts`** 0.76 · `orders.retry.backoff` 0.31 · `orders.retry.maxAttempts` 0.31 | **WIN** — flat's #2 is the *orders* retry |
| `max attempts` | `maxAttempts` L29 4.88 · `maxAttempts` L34 4.88 · `max` L6 2.30 | `orders.retry.maxAttempts` 0.68 · `payments.retry.maxAttempts` 0.68 | WIN on identity, tie on order |
| `datasource url` | `datasource` L8 2.99 · `url` L9 2.99 | **`spring.datasource.url`** 0.81 · `spring.datasource.username` 0.37 | **WIN** |

Proxy caveat: a `.properties` file carries only leaf keys, so the dotted column
has no intermediate-node rows. P3a *would* also emit `server`, `server.tomcat`,
etc. — that makes the dotted column slightly optimistic on ranking, and it is
the reason the size table in §2 is the authority on node count, not this table.

### LOSSES — reported, not hidden

**L1 — every dotted score is ~4× lower, and this is not IDF dilution. It is an
existing hard-coded penalty.** `internal/query/query.go:251`:

```go
if s > 0 && strings.ContainsRune(nodes[i].Label, '.') && !callableKind[nodes[i].Kind] {
    s *= 0.2
}
```

`callableKind` (query.go:84-88) = `function, method, class, interface, file,
module, table, document, section, topic`. **`config_key` is absent.** Today this
never bites (0 of 163 config_key labels in the live store contain a dot); P3a
makes it bite on 100% of nested keys.

**L2 — measured burial.** Same config plus a `server.go` holding `ServerPort()`,
`OrdersRetryMaxAttempts()`, `LoggingLevel()`, gathered into two stores:

```
query "server port"  —  FLAT:
  function     ServerPort                    L4-L4   4.25
  config_key   port                          L2      2.40   <- rank 2
  config_key   port                          L22     2.40
  config_key   server                        L1      1.84

query "server port"  —  DOTTED:
  function     ServerPort                    L4-L4   2.71
  file         server.go                     -       0.95
  function     LoggingLevel                  L10-L10 0.95   <- irrelevant
  function     OrdersRetryMaxAttempts        L7-L7   0.95   <- irrelevant
  config_key   management.server.port        L10     0.54   <- rank 5
```

The correct config answer drops from rank 2 to rank 5+, **below two functions
that have nothing to do with the query**. This is the regression P3a ships if
the query side is untouched.

**L3 — order within ties.** `server port` gives `management.server.port` and
`server.port` the *same* 0.63: the scorer has no label-length normalization, so
a longer path that happens to contain both query tokens ties the exact match.
Flat has the mirror problem (two identical `port` labels), so this is not a
regression — but it means P3a alone does not make ranking *right*, only
*legible*.

**Required companion change:** add `config_key` to `callableKind` (or split out
a `dottedLabelOK` set) in the same change as P3a. Whether that re-tunes proof
D1's original data-node case is **not measured** here — it needs the judged
20-question scoreboard against the pinned corpora, which this spike did not run.

---

## 4. Golden impact

```
grep -rl 'config_key' internal/golden/testdata     -> 1 file
grep -rn 'yml#|yaml#|toml#|properties#|ini#|Makefile#' internal/golden/testdata/golden/multimod.txt
```

| | |
|---|---|
| golden snapshot files total | 4 (`multimod.txt`, `multimod-queries.txt`, `dotnetsln.txt`, `dotnetsln-queries.txt`) |
| files containing config_key ids | **1** (`multimod.txt`) |
| lines that move | **36** of 104 (18 `N … config_key` + 18 `E … -contains-> …#slug`) |
| `dotnetsln.txt` | 0 config_key nodes — untouched |
| `*-queries.txt` | no config-key references — untouched |

Movement is entirely `Taskfile.yml#cmds` → `Taskfile.yml#tasks-build-cmds`-shaped
renames, plus the `contains` edges retargeting (and, if P3a also rewires
`contains` key→key as the proposal proposes, the 18 edge lines change *source*
too, not just target). `deploy/app.yaml#spec` / `#spec-2` — the exact collision
P3a fixes — become `spec` / `spec.template.spec`.

Two things NOT affected:

- `internal/golden/hermetic_test.go:42` asserts `Taskfile.yml::task:build` —
  that is the **manifests** producer's task node (`::`), not a markdown
  config_key (`#`). Unaffected.
- `testdata/questions/linux-block.json` L16 expects
  `Makefile#obj-config-blk-cgroup-iocost`. Makefiles are explicitly *not*
  dotted-path candidates (the docstring already leaves manifest lines whole and
  the prototype does not touch them), so this floor holds — **provided P3a's
  scope stays yaml/toml/ini/properties.** Whether the judged scoreboards move is
  **not measured** (needs `CTX_OPTIMIZE_GOLDEN_CORPORA` clones).

---

## Recommendation, in commit order

1. `fix(extract): make the config guard whitespace-independent` — one line,
   pins the determinism test from §1 (same file ± trailing whitespace ⇒ same
   graph). Golden-neutral in the sample (0.0035% reach); ships alone.
2. `feat(query): config_key labels are addresses, not child declarations` —
   exempt `config_key` from the `:251` dotted downrank, **with** the judged
   scoreboard run to prove proof D1's case did not regress.
3. `feat(extract): dotted config paths for yaml/toml/ini/properties` — P3a via
   `yamlwalk`, key→key `contains`, `len(path) > 200` guard, no depth cap, and
   the reviewed 36-line `multimod.txt` diff.

Step 2 must not land after step 3. Reversed, the release in between is a
measured retrieval regression.

---

## Commands run

```
# guard-bug reach
python3 scan_ws.py                                  # 3,998 files under ~/muthu/gitworkspace
# determinism proof
CTX_OPTIMIZE_STORE=$S/storeA ctx-optimize add $S/wsA   # trimmed        -> 9 nodes
CTX_OPTIMIZE_STORE=$S/storeB ctx-optimize add $S/wsB   # trailing ws    -> 4 nodes
# P3a prototype (verbatim yamlwalk copy, repo never edited)
GOFLAGS=-mod=mod go build -o proto/spike ./proto && proto/spike < files.txt
DUMP=1 proto/spike < repofiles.txt                  # label dumps for ambiguity math
# live-store ambiguity
python3 … ~/ctxoptimize/ctx-optimize/graph/nodes.ndjson   # kind == config_key
# retrieval
CTX_OPTIMIZE_STORE=$S/store_qflat  ctx-optimize add $S/qflat  ; ctx-optimize query …
CTX_OPTIMIZE_STORE=$S/store_qdot   ctx-optimize add $S/qdot   ; ctx-optimize query …
CTX_OPTIMIZE_STORE=$S/store_mix{flat,dot} ctx-optimize add …  # burial test, config + code
# golden
grep -rl 'config_key' internal/golden/testdata
grep -c 'yml#\|yaml#\|toml#\|properties#\|ini#\|Makefile#' internal/golden/testdata/golden/multimod.txt
```

Scratch dir (throwaway):
`/private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-ctx-optimize/3cb25356-4d1c-416f-be28-ea86609d3c63/scratchpad/spike-p3/`
