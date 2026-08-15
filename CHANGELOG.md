# Changelog

All notable changes to ctx-optimize. Format loosely follows
[Keep a Changelog](https://keepachangelog.com/1.1.0/); versions are
[semver](https://semver.org/) and match the published npm package
(`@muthuishere/ctx-optimize`) and the GitHub release tags.

The contract never changes: **the binary is deterministic — no LLM, no DB, no
embeddings, no MCP, no network except your configured remote.**

## [Unreleased]

- **`serve` gets a second viewer, and a switcher to reach it: Flow — the
  architecture, DERIVED.** The dashboard's only picture was the force-directed
  graph, which on a real store is a hairball. The new **Flow** viewer draws the
  same store as a readable scene — numbered cards, labelled curves, a hub, and
  the outer world — and every mark on it is computed from the graph, never
  authored. `GET /api/scene` (read-only: no token, no audit, and it never
  creates a store dir) returns the derivation from `internal/scene`:
  - a **card is a DIRECTORY** (the package boundary the author chose), ranked by
    cross-directory edge weight;
  - an **arrow is N real `imports`/`calls` edges** lifted to those directories
    and summed, drawn with its relation and its count — `AMBIGUOUS` edges are
    excluded, so an arrow is a fact;
  - a card's **column is its longest-path depth in the lifted dependency DAG**,
    with Sugiyama barycentre row ordering, so position carries the direction
    dependencies actually point. This is the explicit answer to the
    `2026-08-13-serve-world` view that was killed by its own criterion —
    "position carried no information, it was the sort order… a map with no
    routes is a list in a costume";
  - the **hub is the most depended-upon directory** (in-degree weighted by
    in-share), so the floor of the dependency stack falls out of the graph:
    `internal/store` here, `src/database` on a real service;
  - the **outer world is the boundary lane's ports**, grouped by transport into
    dashed plates placed under the subsystems that open them — port NAMES only,
    sensitive ones flagged, never a value;
  - the scene prints what it is hiding: top N of M directories, N of M lifted
    relations drawn, and which test trees were excluded.
  The **view switcher** is a registry (`dashboard-ui/src/viewers.ts`): a third
  viewer is one entry, and the shell never names one. Canvas 2D, system fonts,
  zero external requests, and `prefers-reduced-motion` stops every travelling
  dot (verified headless: identical frames 1.4 s apart).

- **The lookup index no longer dies on a gather that changed nothing.** Its
  fail-safe header is keyed on the graph's CONTENT hash instead of the file's
  size+mtime, so a rewrite that produced identical bytes keeps the index alive —
  and any gather now also repairs an index that is missing or stale, which
  previously only `add --force` did. Measured on the linux kernel: `card
  bio_split` stays at **6 ms** across an incremental gather where it used to
  fall back to **1,629 ms**, silently, forever after. `status` now reports the
  index as current / stale / absent, because a 270x fallback that says nothing
  is how this shipped (ADR 2026-08-15-index-dies-on-a-noop-gather).

## [0.14.0] — 2026-08-15

The release the boundary lane was for. A repo's **external surface** — what it
calls, what it exposes, which config values are credentials — is now a first
class answer, and the machinery underneath got roughly **twice as fast** while
becoming **byte-for-byte reproducible** for the first time.

### Added

- **`boundaries` — one command for "what does this system talk to?"** Not a node
  dump: a C4-style system-context summary. CONSUMES and PROVIDES are split
  (different questions, previously interleaved), grouped by transport with
  counts, credentials flagged by **NAME** (never a value), and every line
  citable at `file:line` with a `(+N sites)` roll-up.

  ```
  boundaries: 6 modules, 339 ports

  CONSUMES (what this system calls out to)
    config.env    84 · 84 external · 22 SENSITIVE
        CONFIG_ENCRYPTION_KEY  INFERRED  SECRET  src/storage/secrets.go:L45
    network.http  46 · 46 external
        api.openai.com         INFERRED  src/ai/providers/openai.go:L14 (+2 sites)
            modules: apps/api, apps/ui
  UNRESOLVED  10 ports carry a dynamic identifier — the SITE is certain,
              the value is not
  ```

  Monorepo federation is the payoff: a host reached from two modules folds into
  **one** entry carrying both, rather than being counted twice. `--json` passes
  `otel.*` metadata through under its **OpenTelemetry semantic-convention**
  names (`otel.server.address`, `otel.http.route`), so a static boundary joins a
  runtime trace on the same key — no invented vocabulary. Output is budgeted
  like `query` and **always states how many entries were withheld**; silent
  truncation reads as "that is everything". `boundaries verify` is unchanged as
  its subcommand.

- **The boundary lane itself** — `port` nodes with
  `direction`/`transport`/`identifier`/`scope`/`sensitive`, produced by
  declarative rules (data, never code) merged repo > machine > embedded by rule
  ID, every edge citing its rule and `file:line`. Ships env, process, HTTP-URL,
  web-storage and websocket rules for Go/JS/Python plus server-route rules for
  express, fastapi/flask, net-http/gin/echo/chi, Spring and ASP.NET.

- **`services`** — a 30-vendor registry for SDK-mediated egress, where the
  *dependency is the boundary*: `firebase`, `stripe`, `openai` and friends
  produce a port from a manifest declaration even when no host literal exists
  anywhere in the source. `services add <file|url>` installs one validated
  registry file (the only command that touches the network).

- **`drift`** — reports where `provides`, `consumes` and *declared* disagree:
  dead contracts, env read but never declared. Findings require
  EXTRACTED-by-EXTRACTED evidence; anything weaker is demoted to an observation
  and **listed, never accused**. `--strict` is the CI gate.

- **`search`** — a cross-OS literal sweep over the extractor's own file set
  (same gitignore, same skip-dirs, same size cap), so a vendored tree the
  extractor never reads can no longer pollute a ground-truth count. `grep` does
  not exist on Windows, and Windows is a release target: the binary carries its
  own sweep.

- **Import specifiers resolve.** Relative and tsconfig-alias imports now rewrite
  to the real file node, and Go module imports (including local-path `replace`
  directives) gain `resolves_to` edges. On the Kubernetes corpus, traversable
  imports went **1.8% → 60.5%** of 73,386 edges.

### Changed

- **Gathers are roughly 2× faster than 0.13.0.** Linux **124s → 55s**,
  Kubernetes **14.6s → 7.3s**, java-spring **6.1s → 2.9s**. Most of it was not
  where anyone assumed: `store.Replace` ran **once per producer**, each a full
  read-sort-write of the entire graph; and the store's own cost turned out to be
  **sorting, not parsing** (1,375ms of 2,624ms on Kubernetes) — we were
  re-sorting a file we had written sorted ourselves. A one-file re-gather went
  from 98% of a cold build to **86%**.

- **Boundary rules moved from regex to the AST**, riding the code extractor's
  existing walk. The lane's own cost went from 5.29s to noise on Kubernetes and
  **12.43s → 0.06s** on the TypeScript compiler — which had been spending 12.4
  seconds of regex to produce 42 ports. Accuracy improved with it: a dynamic
  site like `exec.Command(binVar)` is now **visible as AMBIGUOUS** instead of
  invisible, so a repo that spawns processes stops reporting that it spawns
  none. Rules for unparsed languages (e.g. Kotlin without its grammar pack) are
  **declared** as raw-scan rather than silently producing nothing.

- **Memory on monorepos: 12.4GB → 3.42GB** on a 7-module repo, and `GOMAXPROCS`
  is now a **working memory cap** (0.73GB at `GOMAXPROCS=2`). Worker pools keyed
  on `runtime.NumCPU()`, which ignores cgroup quotas — a container limited to 2
  CPUs on a 64-core host spawned 63 wasm instances and OOMed despite its quota.
  Pools now key on `GOMAXPROCS` (container-aware since Go 1.25) and draw from a
  **process-wide** budget, so the bill no longer multiplies by module count.
  Full throttle on a laptop is unchanged.

  **Where the cap stops working, measured:** it bounds wasm instances, not the
  graph. On a 7-module monorepo it is a 5.3× lever (0.69GB at `GOMAXPROCS=2`
  vs 3.69GB at 18) and on kubernetes 2.4× — but on the **full linux kernel**
  (2.85M nodes / 5.54M edges) it is 14.36GB vs 14.65GB, i.e. **no cap at all**,
  because at that size the resident cost IS the graph. Treat ~14GB as a floor
  for kernel-scale trees: a 16GB machine is marginal and an 8GB machine cannot
  gather it. Peak RSS also varies run to run at that scale (14.65 → 16.76GB on
  identical input).

- **Markdown is parsed with a real CommonMark AST** (goldmark) instead of
  per-line regexes. See *Fixed* for what that corrected.

- **`card` cites line numbers on ambiguous candidates**, and a declaration now
  outranks a mention: `card Flask` returned a README heading while the class sat
  in `src/flask/app.py`. It still **refuses to guess** — adding locations is not
  picking a winner.

- **`query` ranks an exact match first.** Naming a node did not find it: the
  match was not merely un-boosted but actively **penalised**, because a
  dotted-label downrank written for child declarations fires on every hostname.

### Fixed

- **Two identical gathers now produce byte-identical stores.** They did not
  before: 360 node locations drifted on linux/block and 223 on Newtonsoft,
  because same-name declarations collapse to one id and the sort was unstable.
  Fixing only the sort would have frozen a **wrong** answer — `card bfq_queue`
  on the kernel cited a struct *member* instead of the type — so the comparator
  now prefers the **widest span**: a declaration spans many lines, a reference
  spans one. Citations got *better*, not merely stable (linux: 60 wider, **0
  narrower**).

- **`query` was never deterministic.** Map-iteration order decided which token's
  IDF counted, so the same query on the same store returned different results
  across runs (20 runs → two distinct outputs). Now one.

- **618 phantom `section` nodes removed** — headings *inside code fences* were
  being extracted as real, citable graph nodes. 9.4% of one repo's sections were
  fabricated from shell examples, and a `query` could return one with a real
  `file:line` that lands the reader inside a code sample.

- **Concurrent gathers no longer corrupt each other's store** — writers shared a
  fixed temp filename, so two `add` runs could eat each other's swap.

- **Markdown links resolve against the walk, not the disk**, so a link into a
  tree no producer reads (`.github/`, `dist/`) no longer mints an edge to a node
  that can never exist.

- **Invalid UTF-8 could reach the store** — doc and signature truncation sliced
  at byte offsets, cutting multi-byte runes in half. Exposed, not caused, by the
  store rewrite: the redundant passes had been healing it via JSON round-trip.

- **A `wasm` rebuild no longer drifts.** `scripts/wasm/build.sh` cloned grammars
  from their *default branch* and then overwrote `grammars.lock` with whatever
  it got, so the lock recorded history instead of pinning it.

- **A repeated `--where` silently kept only the last value.** `--where
  transport=network.http --where sensitive=true` threw the transport condition
  away and returned **1 node where the correct answer is 0** — a *plausible*
  wrong answer, which is worse than an error, and that spaced form is what our
  own skill router documents. `where` is now repeatable and comma-joined, which
  is the AND the filter already parses.

- **A URL's userinfo was reported as its host.** `https://user:pw@host/x`
  produced a port identified as `user`. The password was never captured — but a
  fabricated hostname is a lie, and the confidence tiers exist to prevent
  exactly that.

- **Terminal control bytes reached stdout unescaped**, so a hostile source file
  could rewrite our own output: a label containing `\r` renders as whatever
  follows it. Control bytes are now escaped as visible Go literals — disclosed,
  not silently dropped.

### Behaviour changes to expect

The **store format is compatible in both directions** (verified: 0.13.0 reads
0.14.0 stores and vice versa), no verb or flag was removed, and every flag
documented in 0.13.0 is still accepted. One change can fail a script that
worked before:

- **An unknown flag is now an error (exit 2) instead of being ignored.** It was
  silently dropped repo-wide at exit 0, so `boundaries --sensitve` printed the
  full unfiltered list — the failure always errs toward showing MORE than was
  asked for, worst on the flag whose job is to narrow output to credentials. A
  script passing a flag that never existed now fails loudly, with a
  did-you-mean. That is the intended trade.

Re-gathering an existing repo will also move some numbers, all deliberately:

- **Node counts drop slightly** where markdown fixture/example headings were
  being counted as real sections.
- **Some citations move to wider, more correct ranges** — a symbol that pointed
  at a 2-line prototype may now point at its 72-line body.
- **New `port` nodes appear**, which is the boundary lane.
- **`scope` on consumed ports is always `external` today.** The internal/external
  join compares hostnames against route paths and can never match; it is
  documented in `openspec/changes/2026-08-15-scope-join-broken/` rather than
  quietly left to look like a computation.

### Added — testing

- **The golden net gained gates that can actually fail.** The gather perf gate
  could not: it recorded its own measurement *before* checking against it, and
  shared a switch with the scoreboard, so the tight comparison never ran and the
  baseline silently drifted **up** twice under commits captioned "ratchets
  down". It now has its own switch and refuses to raise a baseline without an
  explicit override.
- **Byte-identity determinism is asserted on both tiers**, hermetic and corpus.
- **A hermetic boundary fixture** pins 11 ports and 12 edges by class, and every
  assertion was **proven red** by deliberately breaking what it guards —
  including by narrowing a *rule*, which proves it catches producer regressions
  and not merely rendering ones.
- **A multi-pass session benchmark** (`benchmarks/session/`) that models what an
  agent actually pays: build once, query, edit, re-index, query again. It
  includes a **staleness probe** missing from every benchmark surveyed — after a
  rename, the old name must vanish *and* the new one appear, so a tool that
  "re-indexes instantly" by doing nothing scores as wrong rather than fastest.
- **A Loc-Bench harness** (`benchmarks/locbench/`) — an external yardstick we do
  not control. We enter only its **retrieval** tier, because answering its
  issues end-to-end requires reasoning this binary refuses to do.

## [0.13.0] — 2026-08-05

### Added

- **A lookup index for the graph — `card` on the Linux kernel went 1.8s → under
  20ms** (ADR `openspec/changes/2026-08-05-query-at-scale/`). Every verb used to
  materialize the whole graph to answer about one symbol: reading all 2.1GB of a
  kernel store costs 0.12s, `json.Unmarshal` of it costs 3.19s, so ~97% of a
  lookup was deserialization paid *before the question was known*.

  `<store>/graph/index/` now holds plain sorted text — `labels.idx`, `ids.idx`,
  `edges-by-source.idx`, `edges-by-target.idx`. A lookup binary-searches with
  8KB `ReadAt` windows and parses only the matching records. **0.43GB, 20% of
  the graph** (CodeGraph's index is 54% of their DB), built in ~6s, ~5% added to
  a kernel gather and ~5% on small corpora.

  **The index is an optimization that fails safe.** Its header records the
  source file's size and modtime; on any mismatch — absent, stale, truncated,
  partial — the caller falls back to the full scan. It can make an answer fast;
  it cannot make one wrong. It is machine-local (byte offsets into this
  machine's graph), excluded from the manifest and never transported.

  Wired into `card` only, and only for exact-id/exact-label resolution. Fuzzy
  and ambiguous names still cost a full scan **deliberately** — they rank
  against every node, and refusing to guess is the point. `change-plan`,
  `affected`, `path` are follow-ups, each needing its own equivalence proof.

  Gated by a differential test that resolves the same symbols through both paths
  and compares whole result sets: **2002/2002 identical on the kernel store.**
  It caught four real defects during development — JSON escapes in index keys
  (11,798 kernel labels, 0.414%), a case-sensitivity divergence between index
  and fallback, a self-edge double-count, and a partial index reporting itself
  current.

### Changed

- **Benchmarks re-run and corrected; several published numbers were wrong.**
  The 2026-07-24 harness was a scratch script that was never committed, so its
  numbers could not be reproduced from the repo. It now lives at
  `benchmarks/bench_multi.py` with `benchmarks/suite/` (pinned, shallow clones).

  Corrections, all of which had flattered us or a competitor:
  - CodeGraph's kernel query published as **536ms** → **880ms**. The harness
    passed CodeGraph only the first word while every other tool answered the
    full phrase.
  - CodeGraph's flask query published as **416ms** → **102ms** (it resolves its
    store from cwd; the harness invoked it from the wrong directory). The whole
    2026-07-24 head-to-head was retired rather than patched, since the same bug
    touched every CodeGraph cell.
  - Linux gather **164.17s** → **118.18s**; query **3.91s** → **3.70s** median.
  - CodeGraph and GitNexus kernel rows filled in: 289.86s, and **GitNexus did
    not finish in 45 minutes** (137 CPU-minutes, 36GB heap, no index) — recorded
    as a non-finish, never as a win.

- **Positioning rewritten to lead with the losses.** The "fastest code graph"
  claim is **retracted**: CodeGraph answers kernel queries in 0.79s to our
  3.70s, and ripgrep in 1.59s. What replaced it is measured — on five kernel
  questions our top hit was useful **4 of 5** against CodeGraph's and graphify's
  **0 of 5**; on a graded 12-question run (gorilla/mux, 3 runs, n=36/arm)
  ctx-optimize scored **67%** against grep's 35% and graphify's 40%, and **79%
  vs 29%** on "what calls this / what breaks if I change this".

  **grep wins "where is X" (47% vs 42%)** and that is now stated on the site.

- **Site restructured to one marketing page + docs + GitHub**, nav cut from nine
  items to two. Every token-savings claim removed site-wide (`docs/CRITIQUE.md`
  S16 measured it dead: Claude Code −0.2%, Codex +3.0%).

### Added — proof

- `proof/agent/RESULTS-QUALITY.md`, `grade.mjs`, `run-quality.sh`,
  `questions-graded-mux.json` — the graded answer-quality harness. Scoring is
  deterministic against hand-verified facts; the grader never sees the arm. It
  also fixed a confound: all arms previously shared one clone, so the shell arm
  could read `.ctxoptimize/` and `graphify-out/`.

  It surfaced two live ctx-optimize defects, both disclosed rather than hidden:
  ambiguous method names collapse the call graph (`affected "Run"` returns only
  the containing file), and `query` ranking loses locate questions to grep.

## [0.12.0] — 2026-08-05

### Added

- **`fresh` exit code 3 = PARTIAL — a store missing a producer no longer reports
  fresh** (#13). Lane containment RECORDED which lanes failed but nothing READ
  the record, so `fresh` exited **0** for a store whose code lane had failed —
  defeating the one job it exists for, gating an agent or hook before it trusts
  an answer. A head-matching partial store reported `fresh` outright.

  `partial` is a distinct state, not a reuse of `stale`, because the two need
  different responses: stale means "old but complete", partial means "a producer
  is missing from this graph". It outranks every other state in the aggregate, so
  a sibling's staleness cannot mask it. `status`, the verdict line and
  `fresh --json` all name **which** lanes failed — "incomplete" alone leaves the
  reader unable to judge whether their question is affected. A hook gating on
  `!= 0` keeps working unchanged.

  Two holes found while wiring it:
  - **`up` on a partial store took the adapter-skipping fast path.** If an
    adapter was what failed, skipping it made the re-gather "succeed" with the
    adapter's data still missing — *clearing* the marker. `up` now retries a
    partial store in full, adapters included.
  - **A gather that skips a lane cleared that lane's prior failure.**
    `sync --no-adapters` reported complete after never retrying the adapter that
    broke. Untried lane failures are now carried forward, marked
    `(not retried: adapters skipped)`.

- **`"wiki"` key in `.ctxoptimize/config.json`** (#9). Onboarding chromium wrote
  **434,597 wiki pages / 1.7 GB** into a single directory — and `wiki.Generate`'s
  stale-page cleanup re-reads that directory at the end of **every** later
  gather (8 seconds just to list it), so the cost is paid forever for pages
  nobody opens.

  ```json
  { "name": "my-repo", "wiki": false }
  ```

  `false` skips generation during `add`/`up`; **`ctx-optimize wiki` still builds
  a COMPLETE wiki on demand**, so "off" never means "unavailable" — it just moves
  off the hot path. `init` scaffolds the key explicitly (a knob nobody can see is
  a knob nobody uses), and **absent means enabled**, so no existing repo silently
  loses its wiki.

  A page cap was prototyped and **rejected**: any cap is a number nobody can
  justify — 2,000 no better than 200 or 20,000 — and it yields a wiki that is
  both incomplete *and* still large. Whether a per-file wiki is wanted is the
  repo's call, not ours.

- **Declared resolutions: `.ctxoptimize/resolutions.json`** (ADR
  `openspec/changes/2026-07-26-declared-resolutions/`). The store abstains on
  call sites it cannot justify, which is correct and leaves the same question to
  be re-derived by every agent forever. Now a repo can write the answer down and
  commit it. First cut is one key, deliberately the one that **cannot make the
  graph wrong**:

  ```json
  { "external_methods": ["Error", "String", "Close"] }
  ```

  Bare method names whose receivers are never types you own. The store holds
  only YOUR declarations, so it can never tell `err.Error()` from a call to your
  own `Error` — it abstains and shows a shortlist. Listing the name retires that
  shortlist.

  The safety claim is structural, not a promise: the declaration is consulted
  **only on the abstention path**, so it can never delete a resolved edge
  (`MyErr.Error()`, which names its own receiver, still resolves) and there is no
  code path from a declaration to an emitted edge at all. It applies only to
  receiver-qualified calls — an unqualified `Error()` is a plain function call
  and may well be yours.

  Malformed is a **hard error, never a warning**: bad JSON, unknown key,
  qualified name, parens, empty entry. A silently ignored declaration is the
  worst outcome because the author believes it is in force; the unknown-key
  error names the keys that ARE supported, so a `receiver_types` line fails
  loudly today instead of doing nothing. A declared name matching no call site
  is reported on every gather — a file nobody prunes decays into
  confident-looking claims about code that moved on.

  Measured on this repo, same commit, one declared line: AMBIGUOUS
  `unresolved-receiver` **239 → 141** (98 maybes retired), INFERRED unchanged at
  2,455, `name-collision` untouched. `init`/`up` scaffold an inert
  `resolutions.json.sample`.

  **Not shipped:** `receiver_types` / `scoped` — the keys that *resolve* rather
  than retire. The binary cannot type-check a type claim, so a wrong line there
  becomes a confidently wrong edge; that trade gets its own ADR.

- **`--include-ambiguous` — a door out of the abstention** (ADR
  `openspec/changes/2026-07-26-include-ambiguous/`). Abstaining made the
  traversal verbs answer with facts only, which means a method's blast radius
  is a **FLOOR** — and there was no command to ask for the rest. Now
  `card`, `explain`, `affected`, `path`, `hubs` and `change-plan` all take
  `--include-ambiguous`.

  Off by default; no existing answer moves. When on, **every widened result is
  marked**: `affected` prefixes the row with `?` plus a footer count (the marker
  rides on the row because rows get copied one at a time), `card` and `explain`
  put the shortlist under its own `MAYBE …(AMBIGUOUS — verify before acting)`
  heading, `path` labels the hop. Two properties are pinned by tests: the
  default filter is what you get for FREE (verbs call `forTraversal`, so it
  cannot be lost by forgetting), and the fact fields — `called_by`, `incoming`
  — stay facts-only whatever flags are passed, so a consumer that never heard of
  the flag reads exactly what it read before.

  `report` is deliberately excluded: its structure is facts-only by design and
  it already has a dedicated section for what could not be resolved.

  On this repo: `affected Batch.Validate --depth 1` → 10 nodes,
  `--include-ambiguous` → 32, of which 22 marked `?`.

- **`store delete` — remove ONE store, from the CLI, safely** (ADR
  `openspec/changes/2026-07-26-store-delete/`). There was no CLI way to delete a
  store: `uninstall` explicitly leaves them, so the practical answer was
  `rm -rf ~/ctxoptimize/<name>` — aimed by hand at a root holding **every**
  repo's store plus `audit.ndjson`, unconfirmed and unaudited.

  `ctx-optimize store delete` resolves the key from cwd exactly as `add`/`status`
  do (no positional argument, so it cannot be aimed at an arbitrary directory),
  is **dry-run by default** (prints what goes, what survives, and that
  `.ctxoptimize/` is untouched), performs on `--yes`, and is audited.

  It **asks** `[y/N]` at a terminal after printing the blast radius. Off a
  terminal (pipe, CI, `< /dev/null`) nothing is asked and nothing is deleted —
  a missing answer must never read as consent, so `--yes` is the only
  non-interactive path.

  **Fixed a live bug while building it:** a store dir is not a leaf — a
  multi-module repo nests its module stores INSIDE the root store
  (`~/ctxoptimize/reqsume/` contains `reqsume/e2e/`). The dashboard's
  `os.RemoveAll` therefore **destroyed stores it never reported**. Deletion now
  goes through one guarded primitive that reports exactly what it touched.

  **It is always the whole repo**, whichever directory you run it from: the root
  store plus every module store, at any depth. Measured on chromium, the first
  version reported `deleted store "chromium"` and left **33 chromium module
  stores** on disk — a lie by omission. There is no per-module delete: module
  stores are the same repo's derived data and re-gathering takes seconds, so the
  flag was surface with no use case. What stays impossible is reaching a store
  the caller never named; a sibling repo is never in scope.

  Two bugs closed on the way there. The per-module path resolved its key with
  `ModuleKey`, producing `svcB` where the store actually lives at `dtest/svcB`,
  so `store delete` inside a module always missed — gone with the module path
  itself. And the nested-store scan stopped at the first store it found, so a
  repo declaring both `svcB` and `svcB/inner` reported **2 stores where there
  were 3**: the delete was right, but a confirmation prompt that under-states
  its blast radius is the one direction that must never happen.

  Also closed: `SanitizeKeyPath` drops a `..` segment, so the key `repo/..` was
  rewritten to `repo` — no traversal escape, but a delete of a store the caller
  never named. A destructive verb now requires the key to survive cleaning
  unchanged. Found by a test, not by reading.

  **Not shipped:** the delete-and-rebuild "resync". Whether a rebuild is a
  convenience or a correctness fix depends on whether a retired *adapter* leaves
  nodes in the store forever, which is unmeasured. That check comes first.

### Changed

- **BREAKING: the wiki is off by default — `add` no longer builds one.** On
  linux v6.9 (84,300 files) the wiki was **1,317.8s of a 1,475.4s cold gather —
  89.3%** — for a byte-identical graph, and no verb reads it: every query
  answers from `graph/`. Cold gather drops **1,475s → 132s** (re-measured after
  the change: 2,849,719 nodes, matching the pre-change count to the unit), which
  turns the graphify head-to-head (531.97s on the same tree) from a 2.8× loss
  into a **4.0× win**.

  Issue #9 made the wiki configurable and left the default alone. That was half
  a fix: linux and chromium have no `.ctxoptimize/config.json` at all, so a
  config-only lever never reached the repos paying the most. The cost is
  non-linear, which is why it went unnoticed — on every published benchmark
  corpus (≤754 files) the wiki is free.

  **"Off" never means "unavailable."** `ctx-optimize wiki` still builds a
  complete wiki on demand, and the new **`--wiki`** flag forces one for a single
  gather (it beats a committed `"wiki": false`). `"wiki": true` in
  `.ctxoptimize/config.json` is unchanged and still opts a repo in — repos
  scaffolded by `init`/`up` since #9 carry that key explicitly, so **they see no
  change at all**. `init` now scaffolds `"wiki": false`, reversing the
  2026-07-26 request to scaffold it on, which predates the measurement.

  What loses its auto-wiki: repos whose config predates the key, and repos with
  no `.ctxoptimize/` at all. For them the wiki stops refreshing and goes
  **stale** — which is strictly worse than no wiki, because it reads as current
  and cites lines that have moved. So staleness is now stated, not discovered:

  - **`status` says so** when a wiki on disk is older than the graph, and names
    both remedies. Silent otherwise — including for the empty `wiki/` directory
    that `store.New` pre-creates in every store.
  - **`ctx-optimize wiki --delete`** removes it. The graph is untouched and the
    verb rebuilds it. This exists so nobody reaches for `store delete`, which
    drops the whole graph (2.85M nodes on linux) plus a re-gather to reclaim an
    artifact they were not using. A stale wiki is not free either: the manifest
    walk does not skip `wiki/`, so every gather re-hashes it — ≈1.1s at linux's
    60k pages / 250MB.

  ADR `openspec/changes/2026-07-27-wiki-off-by-default/`; measurements in its
  `spikes.md`. Judged floors unmoved (linux-block 16.5, newtonsoft 13.0), as
  they must be — the wiki was never on the query path.

- **`scan` now honours `.gitignore`, and stopped hard-coding `out`** (#10). The
  code producer already respected `.gitignore` with git's own semantics; `scan`
  did not — so the two disagreed about what is even in the repo. Chromium's
  **`out/Default`** was proposed as a module while extraction correctly skipped it
  as gitignored build output (`chromium/.gitignore:252: /out*/`).

  `out` was briefly added to the built-in prune list, which patched the symptom
  with a name generic enough to break any repo that legitimately keeps source in
  `out/`. Removed. `.gitignore` handles it, correctly, for every repo rather than
  just Google-shaped ones — chromium still resolves to **21 modules**, now by
  principle instead of by coincidence.

  Precedence is now explicit, and the repo decides at every level: `.gitignore` →
  `scan.exclude`/`scan.markers` → **`scan.include`, which beats every automatic
  exclusion including `.gitignore`** (the escape hatch that makes honouring it
  safe) → the hand-editable `modules` list in `config.json` → and only then a
  short built-in list for trees that are **vendored yet checked in**, where
  `.gitignore` cannot help (`vendor`, `node_modules`, `third_party`, …).

  Also finished: a **marker file** must be tracked too, not just its directory. A
  repo that generates and gitignores its `package.json` / `Cargo.toml` is not
  declaring a project there — calling that directory a module was the same
  disagreement, one level down.

  Vendored code is still **indexed** — that is deliberate, so you can query into
  a dependency. What the prune decides is only whether a subtree gets its own
  store and its own line in the module list.

- **A `.txt` is plain text: `#` is a comment, not a heading** (#14, ADR
  `openspec/changes/2026-07-26-hash-is-a-comment-not-a-heading/`). The doc
  producer claimed both `.md` and `.txt`, so a shell-comment licence header —
  every line starting with `#` — became a wall of `section` nodes whose labels
  were mid-sentence prose.

  Measured across **30,289 real `.txt` files in 22 repositories**: 6,902 section
  nodes, **95.1% of them comment lines or prose fragments**. Linux's 1,695 `.txt`
  files yielded **zero** genuine headings. And they were not harmless — they
  ranked **first**, taking **26–30% of top-10 query slots** wherever they existed;
  one repo's 35 `.txt` files produced **16.1% of its entire store**.

  A `.txt` now yields exactly one node: the file, as a `document`. `.md` is
  unchanged. `internal/navigator` — a **second** code path applying the same
  `#`-is-a-heading rule to `README.txt` — was fixed with it, so the two
  subsystems cannot disagree about the same file.

  A threshold rule ("is `#` a comment character in *this* file?" — ≥4 consecutive
  `#` lines or >20% density) was designed, measured against all 30,289 files, and
  **rejected**: it needed two invented numbers, and a number nobody can justify is
  exactly what this project refuses elsewhere. 95% junk means the extraction is
  not worth having, not that it needs tuning.

  **Cost, stated rather than hidden:** a `.txt` that genuinely is markdown
  (`llms.txt`, LLM prompts kept as `.txt`, a manuscript) becomes reachable by
  filename instead of by content. Rename it `.md`, or grep it. A smaller,
  predictable loss beats a heuristic that misfires in ways nobody can enumerate.

  The per-file `document` node is kept **unconditionally**:
  `internal/extract/manifests` anchors `declares` edges on the file path and emits
  no node of its own, so it is the only node backing every python dependency edge
  — and `PartitionValidate` does not quarantine absent endpoints, so dropping it
  would dangle silently.

  Golden diff: `pydeps.txt` loses 4 section nodes + 4 edges (60→56 nodes), all
  pip-compile comment lines. `crlf_test.go`'s assertion that
  `# LICENCE / TRWYDDED` is "a real CRLF heading" is **inverted**, with the
  reversal explained in the test; the CR-stripping and empty-heading cases move to
  a `.md` fixture. Corpus counts and judged tiers unmoved (16.5 / 13.0).

- **The core promise is now written down**: *we do not invent structure that isn't
  there — if it cannot be parsed honestly, it is not indexed, and you are told to
  grep.* Every abstention in the tool is that one rule wearing different clothes:
  ambiguous callees, unresolved receivers, fuzzy ties, `[redacted]` values,
  partial gathers, and now `#` in a `.txt`. Stated in `docs/cli.md` with its
  measurement, and on the agent surface in `SKILL.md` — where routing a
  `.txt`-content question to grep is described as **the correct behaviour, not a
  fallback to apologise for**. Pinned by `TestCorePromiseIsOnTheAgentSurface`,
  including the requirement that the doc carry the measurement: an unmeasured
  promise is a slogan.

### Fixed

- **A single long task no longer looks hung** (#12). Progress ticks fired only on
  task COMPLETION, so on chromium the output went `[47/48]` and then silent for
  minutes while the 3.6M-node residual gathered. Two additions, both stderr-only
  (so `--json` and piped output are untouched) and both plain lines (so CI logs
  stay readable):

  - a line when a task **starts** (`→ third_party/androidx`) — you can see what is
    running instead of inferring it from what has not appeared yet;
  - a **heartbeat** naming the in-flight tasks and how long each has been going
    (`… still running (47/48 done): . (2m14s)`), so silence never means "no idea
    whether this is alive".

  The heartbeat stays silent until a task outlives its interval, so a normal repo
  prints exactly what it printed before — pinned by a test in both directions.

- **A failed npm publish no longer burns the tag** (#15). `run:` executes under
  `bash -e`, so the first platform package that failed aborted the loop: four
  good packages never attempted, wrapper step skipped. A re-run then hit
  `already_exists` on whatever *had* published and aborted again — which is how
  v0.10.2 was lost (a Sigstore 409 killed the run, the re-run failed on
  `already_exists`, v0.10.3 shipped instead).

  Now every platform is attempted, failures are collected and reported together,
  and a version that is already on the registry counts as success — so a re-run
  completes instead of aborting. That is what makes a tag retryable.

- **A big `add` was 4× slower than it needed to be: the dust-merge loop in
  community detection was O(n² log n)** (ADR
  `openspec/changes/2026-07-26-quadratic-dust-merge/`). Reported as "the progress
  bar sometimes takes too long"; the display was the symptom.

  The dust-merge phase needs one thing per iteration — the smallest non-isolated
  community — and it got it by **rebuilding and re-sorting the entire community
  list every iteration**. On a 12,000-file repo that is ~12,000 iterations of a
  12,000-entry sort (~1.7B comparisons): **12.8 seconds, 90% of the wiki's total
  time, to return ZERO communities**, since every component was disconnected dust
  that gets dropped. Replaced with a min-heap over `(size, id)` with lazy
  invalidation — same candidate, same tie-breaking, so clustering output is
  byte-identical (verified: 10 subsystems, same members, same order).

  Also: wiki file pages now render and write across `NumCPU` workers, worth 15%
  of a large gather. Output verified page-by-page — 0 of 12,021 pages differ.

  | | before | after |
  |---|---:|---:|
  | `add` on a 12k-file repo | 16.46s | **3.89s** (4.2×) |
  | `Communities` on 12k dust components | ~12.8s | **16ms** |

  The corpus tier is unchanged (linux 0.4s, Newtonsoft 1.0s) — this fixes graphs
  in the bad shape and does nothing for graphs that never were. `TestCommunities50kUnderASecond`
  passed at 29ms throughout: its graph is connected, so the dust loop barely ran.
  `TestCommunitiesDustMergeIsNotQuadratic` adds the missing shape.

  **Not fixed:** progress is still reported only on task completion, so a single
  long task still prints nothing while it runs.

- **One break no longer stops the whole** (ADR
  `openspec/changes/2026-07-26-failure-containment/`). Asked of the chromium run;
  the answer was that failure was contained at one level, not at two, and at a
  fourth level nothing was ever cleaned up at all.

  - **A producer lane no longer aborts the others.** `gatherInto` had seven early
    returns, and the worst shape was the adapter lane: it returned *after*
    code/docs/manifests were extracted but *before* the commit loop, so one
    broken adapter script **discarded a whole successful gather**. Every lane now
    runs, everything that worked is committed, and the failures are reported
    together and returned as one error. Same containment at commit time — one
    producer tripping the shrink guard no longer stops the rest from landing.
  - **A partial gather is recorded as partial.** Containment alone would be a
    downgrade, so `freshness.Source` gained `partial` (which lanes failed) and a
    partial gather **clears the tree signature**, so the next run cannot
    short-circuit as "unchanged" and freeze the gap in place.
  - **The navigator survives a failed module.** `if len(failed) > 0 { return }`
    fired before `writeNavigator`, so one broken module out of 48 denied
    root-level federation over the 47 that worked. The navigator is built from
    the full task plan, not from the successes, so writing it was always safe —
    the return was just in the wrong place.
  - **A retired producer's nodes are no longer immortal.** `Replace` is
    producer-scoped, so a producer that stops running is never replaced.
    Measured: an adapter emitting `custom://ghost`, then deleted, kept its node
    through `--force` and every later gather. Now reported on every gather, and
    pruned on a run with no skips and no failures. Reported rather than
    auto-pruned because absence means either "retired" or "did not run this time"
    (`--no-adapters`, unchanged HEAD, a failed lane), and deleting a lane's data
    because it did not run would be far worse than a stale node.

### Added

- **`add --rebuild`** — drop the store(s) this add will write, then gather into
  nothing: the guaranteed resync, for when you would rather not reason about
  producer scoping. Uses the same task plan as the gather, so it cannot drop a
  key the gather won't rewrite; nested module stores are kept (each is rebuilt by
  its own task); audited as `store.rebuild`.

### Fixed

- **Onboarding chromium: three defects, found by running it** (ADR
  `openspec/changes/2026-07-26-chromium-onboarding-defects/`). A full chromium
  checkout gathered without falling over (366,277 nodes in the root residual),
  and the output carried three real problems.

  - **`scan` reported 241 "modules"; 217 (90%) were vendored.** All under
    `third_party/`, plus `out/Default` — GN build output — and nested cases like
    `net/third_party/quiche/src/depstool`. `pruneDirs` already refused
    `vendor`/`dist`/`build`/`target` on exactly this reasoning and just did not
    know the names Google-style repos use. **241 → 21 modules on chromium.**
    Scan-only: the code producer still walks those trees, so nothing stops being
    indexed — a vendored subtree simply no longer gets its own store.
  - **CRLF files leaked a carriage return into every extracted value**, and a
    bare `# ` line in one became a heading titled `"\r"` — an empty slug and an
    empty label, which the schema correctly refused
    (`quarantined 18 invalid item(s) … label is required`, from
    `third_party/hunspell_dictionaries/README_*.txt`, shell-comment licence
    headers). Lines are now split with the CR stripped, and a heading with no
    text is skipped rather than emitted. Verified on the real file: 0 empty
    labels, no quarantine.
  - **Dangling symlinks were reported per-file at the same volume as real parse
    failures** (`third_party/nearby` vendors broken links). Now counted and
    summarized once — reporting each one trains the reader to ignore the channel
    that carries real skips.

  Investigated and **not** a defect: progress appearing to stop at `[47/48]`.
  Every task ticks from a `defer`; `[48/48]` is the root residual, the slowest
  task, which finishes after those messages. Recorded so nobody re-investigates.

  Recorded and deliberately **not** fixed: `.txt` files are parsed as markdown,
  so a `#`-prefixed prose line in a licence file becomes a `section` node. Valid
  but useless. Fixing it means dropping `.txt` or guessing "this isn't
  markdown" — own ADR.

- **A unique method name is no longer treated as evidence about the receiver**
  (ADR `openspec/changes/2026-07-25-method-call-resolution/`, the 0.11.0 known
  issue, now closed). `calleeName` returned the last name node and threw the
  receiver away, so `err.Error()` and `Error()` were indistinguishable — and
  when exactly one declaration in the repo bore the label `Error`, every
  `err.Error()` resolved to it **confidently**. Not ambiguity: one candidate, so
  the AMBIGUOUS shortlist never fired, and these edges DID flow into `affected`,
  `change-plan`, `hubs` and clustering.

  The receiver is now captured and a **method** candidate is attributed only on
  an actual tie: the receiver names the type (`Batch.Validate()`), the call is
  `self`/`this`/unqualified from inside the owner, or the owner type is written
  in the SAME declaration as the call (`e := &Engine{}; e.Charge()` — this is
  the tie that keeps "which tests cover X" answerable). Anything else is
  **abstained on**, not dropped: it becomes an AMBIGUOUS shortlist, filtered out
  of every traversal, reachable with `edges --confidence AMBIGUOUS`.

  Measured on this repo: **225 attributions reclassified from INFERRED to
  AMBIGUOUS, none lost** (2,626 → 2,401 INFERRED). `AmbiguousError.Error` went
  from 89 confident callers to 89 declared abstentions. **Judged tiers
  unchanged — linux-block 16.5/20, newtonsoft 13.0/20, byte-identical before
  and after** (re-run against stashed changes to be sure); hermetic + corpus
  golden green; gather time unmoved.

- **The card no longer explains an abstention with the wrong reason.** There are
  now two, they are settled by different greps, and the reason is stamped on the
  edge (`metadata.ambiguous_reason`: `name-collision` | `unresolved-receiver`).
  Saying "the name is defined more than once" about a name defined exactly once
  would be a false explanation, which is worse than none. `card` prints the
  matching line and grep; SKILL.md and `references/activation-routing.xml` carry
  the split plus the consequence an agent must not miss — **a blast radius for a
  method is a FLOOR, not the full set** — pinned by the doc-drift guard.

## [0.11.0] — 2026-07-25

### Added

- **`report` — one artifact for "explain this repo"** (ADR
  `openspec/changes/2026-07-25-report-verb/`). Store facts, subsystems, the seams
  BETWEEN subsystems, and a section no comparable tool prints: **what the graph
  could NOT resolve**, per symbol, with the grep that settles it. Structure is
  computed from facts only — AMBIGUOUS edges influence the gaps section and
  nothing else. Deterministic, including under reversed edge input order.
  Deliberately NOT graphify's "surprising connections": their scoring weights
  confidence `{AMBIGUOUS: 3, INFERRED: 2, EXTRACTED: 1}`, so the least reliable
  edge ranks as the most interesting and the headline finding is the one least
  likely to be true.
  Four rounds of measured de-noising, each from real output: import stubs
  excluded (every bridge was `X imports module://strings`), `contains` excluded
  (nesting is not dependency), an **allowlist** of dependency relations so a
  relation added later cannot silently pollute it, and one row per **subsystem
  pair** so a single over-attracting node cannot fill the table. Hubs needed it
  too — the first report ranked `strings` (142), `os` (111) and `fmt` (83) as this
  repo's top hubs; filtered inside `Report` so the standalone `hubs` verb keeps
  its shipped behaviour.

- **Ambiguous callees are shortlisted instead of silently dropped** (ADR
  `openspec/changes/2026-07-25-abstain-out-loud/`). A call site whose callee name
  is defined more than once used to be DISCARDED SILENTLY: nothing wrong entered
  the graph, but the graph looked COMPLETE, and an agent reading `called by` had
  no way to learn other call sites existed. Saying nothing is not saying no.
  Now the candidates are emitted as `AMBIGUOUS` — a **shortlist to grep**, never a
  claim — and **every traversal verb filters them out by default**
  (`analyze.WithoutAmbiguous`, applied INSIDE `Affected`/`Hubs`/`ShortestPath`/
  `Explain`/`Card`/`Communities`, not at call sites, so a verb added later cannot
  forget it). `card` reports the count it could not attribute plus the two commands
  that settle it; `called_by` itself stays exact. Reachable on purpose via
  `edges --relation calls --confidence AMBIGUOUS --to <id>`.
  Three outcomes are now distinguished where `pick` conflated two: >1 candidate
  shortlists; **0 candidates (stdlib/deps) still emits nothing**, because external
  is not ambiguity and must never inflate it; above the cap (4) nothing is
  shortlisted at all, since a name with 40 definitions is better served by grep
  than by 40 maybes.
  Measured across caps 0→100: INFERRED (2,561) and EXTRACTED (2,956) **identical at
  every value** — purely additive, no node created, no confident edge downgraded.

### Fixed

- **Community detection was clustering on maybes.** `Communities()` consumed every
  edge given to it — harmless until the shortlisting above landed, at which point
  the wiki's architecture summary started being decided by guesses. Measured on
  this repo (8,495 edges, 1,092 AMBIGUOUS): communities whose hub list repeats a
  single label went **7 → 0** (the `Run, Run, Run` artifact — a "subsystem" that is
  really one over-used name) and the top six subsystems were reshuffled outright.
  Exactly the god-node-by-name-collision failure `docs/VISION.md:284` measured,
  arriving through clustering rather than through `hubs`. Caught before release.
- **The 2026-07-14 community-detection ADR is flipped to IMPLEMENTED** — the wiki
  Subsystems section and the navigator `about` line had shipped as specified; a
  broken grep had led me to report it as unwired.

### Changed

- **`docs/VISION.md` refined on ambiguous edges.** "Emit … AMBIGUOUS and let the
  agent weigh them (graphify-parity behaviour)" was too loose: nothing stopped a
  maybe entering a blast radius, and an agent cannot weigh what it cannot see is a
  guess. AMBIGUOUS is now defined as a shortlist to grep, filtered from every
  traversal verb by default.
- **A doc-drift guard** (`internal/skills/docdrift_test.go`) pins the behaviour
  claims that would mislead an agent if they went stale — the ambiguous contract
  across code + docs + both agent surfaces, and the 0.9.2 redaction guidance. This
  repo states behaviour across 13 hand-maintained surfaces plus 6 site pages and
  40 ADRs, and had already shipped two stale claims in one day. It earned its keep
  immediately by catching a `code.go` comment that the same change had falsified.

### Known issue (not fixed)

- **Bare-name method resolution is imprecise** (ADR
  `openspec/changes/2026-07-25-method-call-resolution/`). `calleeName` returns the
  last name node and discards the receiver, so `err.Error()` and `Error()` are
  indistinguishable — and when exactly one declaration in the repo has the bare
  label `Error`, every `err.Error()` resolves to it **confidently**. This is not
  ambiguity: there is one candidate, so the AMBIGUOUS shortlist never fires. A
  unique name is being treated as evidence that a method call targets it, when the
  receiver's type is what decides. Root cause is a blind spot — the graph holds
  only OUR declarations, so a bare-name method match silently assumes the receiver
  is ours, which for `Error`/`String`/`Close` is usually false.
  Measured here: **331 of 2,596 INFERRED call edges (12%) target a method** and are
  only reachable by bare-name match, 215 of them cross-package. That is the suspect
  population, not the error count — `Batch.Validate` (31) is mostly right,
  `AmbiguousError.Error` (85) is almost all wrong. These are INFERRED, so unlike
  AMBIGUOUS edges they DO flow into `affected`, `change-plan`, `hubs` and
  clustering. Surfaced by the new `report` verb. Not fixed in 0.11.0: the
  candidate fix trades recall for precision and that had to be measured on the
  judged tiers first. **Fixed in [Unreleased]** — the measurement came back
  score-neutral.

## [0.10.4] — 2026-07-25

### Fixed

- **Question grammar no longer scores as signal** (ADR
  `openspec/changes/2026-07-25-doc-demote-ranking/`, follow-up section). After the
  doc demote, 3 of 10 code-intent questions were still wrong at rank 1. Diagnosing
  each instead of tuning further found only ONE was a ranker defect: "prune stale
  nodes on add" answered `install.go::OnPath` — it won on the word **"on"**,
  because IDF is computed over identifier tokens where `on` has df=49 of 3,963 →
  **idf 4.37, higher than `name` at 4.41**. English question grammar was acting as
  a rare, high-signal discriminator. (The other two misses were defensible answers
  and arguable expectations, so they are left as-is rather than reclassified.)
  A small stopword set is now dropped from the QUERY, never from node tokens, and
  kept narrow on purpose: `get`, `set`, `new`, `run`, `add`, `list`, `call`,
  `name`, `path`, `file` are explicitly NOT stopwords — they are real identifier
  prefixes and dropping them would break `query "get user"`
  (`TestStopwordsKeepIdentifierWords`). An all-stopword question still searches
  literally instead of returning nothing.
  **Measured on the independent corpus: newtonsoft judged 12.5 → 13.0**, floor
  ratcheted; N16 "Where do JSON converters get chosen FOR A type?" went 0.5 → 1.0.
  linux-block held at 16.5. Own-repo recall@1 did NOT move (7/10): the residual
  miss now returns `anyStale` (topically relevant) instead of `OnPath`
  (grammatically lucky) — the answer improved, the score did not, and both are
  reported.
  Remaining ceiling is **recall, not ranking**: `store.Replace` cannot win "prune
  stale nodes" by any reranking, because only `Label + Source` are tokenized and
  "prune" lives in its doc comment. Indexing doc/signature text is the next lever
  and was not attempted.

## [0.10.3] — 2026-07-25

Same contents as the v0.10.2 tag, republished. `npm publish` failed on v0.10.2
with a Sigstore `TLOG_CREATE_ENTRY_ERROR` (409, "an equivalent entry already
exists in the transparency log"), so **no npm package was ever published at
0.10.2** — the loop runs under `bash -e` and aborted on the first platform, which
at least kept npm consistent at 0.10.1. Re-running was not the fix: goreleaser
had already succeeded, so it hit `422 already_exists` re-uploading its own
assets, and the tlog conflict is keyed to the artifact+version, so an identical
tarball can keep colliding. A new version gets a new tarball and a fresh entry.
The v0.10.2 GitHub Release and its binaries are valid and stay published.

### Fixed

- **Prose no longer outranks code when the question is about code** (ADR
  `openspec/changes/2026-07-25-doc-demote-ranking/`). Dogfooded on this repo's
  own store with 10 real "where is X implemented" questions: the right symbol was
  the top hit **5/10**, and a `.md` node held #1 **4/10** — "prune stale nodes on
  add" and "budget query hits ranking" both answered `README.md`, and "match
  declaration by head symbol" returned the ADR three times while never returning
  `headDecl`. Cause measured, not guessed: doc nodes are **40% of the graph**
  (1,315 `section` + 278 `document` of 3,963) because `openspec/` is large, and
  prose repeats a question's words far more often than code does, so IDF-weighted
  overlap prefers the essay to the implementation. Any repo with real design docs
  has this shape.
  `intentAdjust` now scales `section`/`document` by 0.5 unless the question is
  about prose — the same mechanism that already demotes `module://` (0.25×) and
  test sources (0.5×), with the same escape hatch: `docIntent` (doc/docs/readme/
  changelog/adr/spec/proposal/design/guide/wiki/rationale/decision/openspec)
  turns it off entirely. A demote, not a filter: a doc still wins when it is
  genuinely the best answer, and `verify`/`explain`/wiki are untouched.
  After: recall@1 **5/10 → 7/10**, prose holding #1 **4/10 → 0/10**, recall@3
  8/10 → 9/10. 0.5 is the mildest value reaching every maximum — swept
  1.0/0.75/0.6/0.5/0.35/0.25 and kept runnable as
  `TestDocDemoteChosenByMeasurement`. Judged scoreboards unchanged (linux-block
  16.5/20, newtonsoft 12.5/20) and every golden snapshot passes, `queryTop`
  rankings included. Honest limits: recall@**3** barely moves — the failure was
  almost entirely at rank 1 — 3 of 10 still miss there, and the question set is
  10 questions on one repo written by the same person as the fix.

- **The skill was silent on redaction and stale on the pack format.** Audited the
  bundled skill against the whole CLI surface; every verb was covered, but since
  0.9.2 a credential-shaped line comes back `[redacted]` from `card` and
  `query --include-content` and the skill never said so — an agent hitting that
  would reasonably "fix" it by opening the file with Read, re-creating the exact
  leak the redaction closed. Now stated on the hydration row and on both the
  `query` and `card` notes, including that it applies WITHOUT the flag and that
  routing around it with a file read is not allowed. Separately,
  `references/extending.md` claimed `packConfig` "has only `name/exts/decls/
  names/calls/imports`, `langs.go:224-231`" — both halves went stale in 0.10.0
  (`decl_rules` exists; the struct moved to `langs.go:256`). Corrected, and the
  homoiconic case is now documented where a pack author hits it: the
  `list_lit → function` trap, the rule shape, `name_unwrap`, why `skip_inside` is
  structural, and that everything misses rather than guesses.

## [0.10.1] — 2026-07-25

### Added

- **`cljgo` is addable by name** — `ctx-optimize languages add cljgo`. cljgo is a
  Clojure dialect that ships `defroute`/`defroutes` and
  `defcommand`/`defcommands` as part of the LANGUAGE, not as a framework a
  project opts into, so it earns a registry entry rather than living in each
  project's pack. The entry carries clojure.core's 14 definers plus those 4, and
  is verified to cover core with identical kinds — a dialect must never
  reclassify `defn`.
  It claims **only `.cljgo`/`.cljg`**. `tree-sitter-cljgo`'s own pack also
  declares `.clj`/`.cljc`, which is right for a project written in cljgo and
  wrong for a shared registry: pack extensions beat the embedded set and the
  winner between two packs is order-dependent, so claiming `.clj` here would
  silently take every plain Clojure file from the `clojure` entry. Pinned by
  `TestDialectDoesNotClaimParentExts`.
  Measured: `languages add cljgo` into an empty grammars dir yields a loadable
  pack (2 rules, 20 heads) that gathers a cljgo tree carrying NO repo-local pack
  into 20 function, 22 variable and 7 module nodes — 0 named after a defining
  macro, 0 call sites.

### Fixed

- **Registry `DeclRules` seeds are now checked for validity.** They are
  hand-written JSON string constants spliced into a generated draft, so a stray
  comma shipped a grammar that builds and then cannot load — precisely the
  "pack ready / rejected one command later" failure G4 exists to prevent.
  `TestKnownDeclRulesAreValidJSON` parses every seed in the table.

## [0.10.0] — 2026-07-25

### Added

- **`decl_rules` — declarations matched by head symbol** (ADR
  `openspec/changes/2026-07-25-homoiconic-decl-rules/`). The pack format mapped
  *node type → kind*, which assumes a declaration HAS a node type of its own.
  Every Lisp breaks that: `(defn fetch-user [] …)` is an ordinary `list_lit`
  whose head symbol carries the meaning and whose second element is the name. A
  Clojure pack was therefore impossible by construction — mapping
  `list_lit → function` emitted nodes all named `defn` while the real names never
  appeared, and an empty-`decls` pack was hard-rejected at load. `decl_rules`
  matches ON the head (`head_type` + `head_match`) and reads the name from the
  next element (`name_type`, `name_unwrap` for `(in-ns 'app.core)`), with
  `skip_inside` structurally excluding forms inside a quote or syntax-quote —
  code a macro is CONSTRUCTING, not defining. A pack may use `decls`,
  `decl_rules`, or both. Zero cost to every existing language: the branch is
  guarded on `len(lang.DeclRules) > 0`, which is empty for all twelve embedded
  grammars.
  Literal-only, so every failure lands on the under-claim side: `s/def` aliases,
  a project's own `defsomething`, and metadata in the name slot all MISS rather
  than guess. Measured on a 658-file Clojure corpus: 1,372 definer-headed forms,
  1,362 resolved to a literal name, 5 excluded as syntax-quoted, **0 wrong facts**.
  `clojure` joins `KnownGrammars` (stock `clojure.core` definers only, over
  `.clj/.cljc/.cljs/.cljr/.edn/.bb`) so `languages add clojure` yields a
  *loadable* pack; the wasm is built on demand, not committed. Dialect and
  framework definers (Compojure, re-frame, cljgo) belong to the project and load
  from its repo-local `.ctxoptimize/grammars/`, which `LoadPacks` reads first.
  Only Clojure has been measured — Fennel, Janet, Elisp and Racket are the same
  shape and are deliberately NOT advertised until a corpus has been run.

### Fixed

- **`grammar build` no longer reports success for a pack that cannot load** (G4).
  It printed *"pack ready … next `ctx-optimize add` picks it up"* for an
  empty-`decls` pack that `add` rejects one command later. It now fails at build
  time and names the homoiconic case.
- **Pack extensions are no longer guessed from the grammar's name.**
  `tree-sitter-clojure` seeded `.clojure` — an extension nobody uses, silently
  matching nothing while looking configured.

## [0.9.2] — 2026-07-25

### Added

- **Docker + Compose recognizers** (ADR
  `openspec/changes/2026-07-25-docker-compose-recognizer/`). `Dockerfile`
  previously produced ONE `config` node and `compose.yaml` a bag of 17 flat
  `config_key` nodes — three of them labelled `image`, with the actual image
  refs (`ghcr.io/acme/api:1.2.3`, `postgres:16`) nowhere, and `depends_on`
  present as a key but never as an edge. Now: a `service` node per compose
  service, a `stage` node per Dockerfile build stage, `uses_image` edges to the
  shared `image:<ref>` node, `depends_on` edges between services (list AND map
  forms) and between stages (`COPY --from`), and a compose→Dockerfile edge when
  `build:` resolves to a file that exists. Reuses the k8s lane's exact image
  convention, so a repo with both k8s manifests and compose files converges on
  ONE node per image with edges from both lanes (pinned by golden assertion).
  Literal-only: `${VAR}` is never resolved, `extends`/`include`/profiles are
  never merged, and `environment:`/`env_file` are not read at all — neither
  keys nor values — so the credential surface is excluded by construction.

- **Python and Rust dependency extraction** (ADR
  `openspec/changes/2026-07-25-structured-formats/`, S7). `deps` used to print
  `(0 dependencies)` on a repo declaring 61 of them — a verb lying by omission
  about the second-largest ecosystem, while emitting 199 `config_key` nodes
  from the very `pyproject.toml` that held the answer. Now recognized: PEP 621
  `[project]` + `optional-dependencies`, poetry (incl. groups), PEP 735
  `[dependency-groups]`, `[build-system] requires`, `[tool.uv]`,
  `requirements*.txt`, and Cargo `[dependencies]`/`[dev-]`/`[build-]` including
  inline tables, `[dependencies.<name>]` sub-tables, `[workspace.…]` and
  `[target."cfg(…)".…]`. Namespaces `pypi` and `crates`, with PEP 503 name
  normalization so `flit_core`/`typing_extensions` don't split into duplicate
  nodes. New `internal/extract/tomlwalk` — a stdlib TOML **table** walker in the
  `yamlwalk` tradition (no TOML library; the validated prototype scored
  1,639/1,639 declarations across 103 real manifests, 100% precision and recall
  against `tomllib`).
- **pip-compile / uv lock output is skipped, not indexed.** 61% of pypi
  declarations in a real sample come from `requirements*.txt` and 12 of 40 of
  those are resolver output; emitting transitive pins as *declared* dependencies
  would be the same dishonesty in reverse. Detected by generated-by header (in
  the leading comment block only) or by the fully-`==`-pinned + `--hash=` shape.
  Matching table-anchored parsing, never a version-string scan — a naive scan
  invents ~12 phantom deps from flask's own `[tool.tox…] commands` array.

### Fixed

- **SECURITY: a secret's VALUE could reach agent context through a citation.**
  The store never held secret values — but `card` and `query --include-content`
  re-read the cited line off disk at answer time, so a `config_key` node
  anchored at `spring.datasource.password=…` printed the credential straight
  into the model's context. Reproduced on the shipped binary across
  `.properties`, `.ini`, `.toml` and compose YAML, and it needed **no**
  `--include-content` — plain `card` was enough. This violated the hard rule
  that a secret's value must never enter an agent's context window.
  Hydration now redacts at both choke points (`bodyHead`, `hydrateHits`):
  a line whose KEY names a credential (`password`/`secret`/`token`/`api_key`/
  `private_key`/`connection_string`/…) has its value withheld, and an embedded
  URL credential is masked to `scheme://user:***@host` — the same shape
  `internal/sources` already reports. The key and its exact line stay visible,
  so the citation is still useful; only the value is withheld. Over-redaction
  is the deliberate failure mode (a leaked value cannot be pulled back out of a
  model's context; an over-redacted one costs one file read). `verify`'s drift
  comparison still sees the real bytes.
- **Golden tests were not hermetic w.r.t. grammar packs.** They isolated the
  store but discovered packs from the machine-global `~/ctxoptimize/grammars`,
  so a developer's installed packs took part in every "hermetic" fixture
  gather — and one malformed pack failed three unrelated golden tests with an
  error about a language nobody was testing. The package now points
  `CTX_OPTIMIZE_GRAMMARS` at an empty temp dir for its whole run.

- **The Newtonsoft judged floor was a number nobody ever measured.** `min_score`
  16.5 was introduced in 7588bd8 by applying linux-block's figure to the second
  corpus, so the judged tier had never once been green and `golden.yml` was RED
  on every run from 2026-07-20. Corrected to the measured 12.5 — verified
  identical on a binary built from 0de40e3, i.e. no change regressed it — with a
  `_floor_note` recording that the 7.5-point gap (N17/N19/N20, usage-shaped "how
  do I serialize / create a serializer / run the tests") is the quality target,
  to be closed by fixing retrieval and never by editing the floor.

- **Config keys no longer depend on invisible whitespace** (S1). The
  nested-key guard in the config lane compared against a trailing-trimmed copy
  of the line, so an indented key was skipped only when it happened to carry
  trailing whitespace — the same file produced 9 nodes trimmed and 4 with
  trailing spaces. Keys are now indexed consistently at every depth, and the
  doc comment that misdescribed this (and hid it) is rewritten.
- **Config keys are no longer harvested from inside YAML block scalars** (S2).
  A `key: |` / `key: >` body is opaque data, not config structure. On real k8s
  manifests this removed 9 junk nodes parsed from an embedded
  `postgresql.conf`; on Newtonsoft.Json's `azure-pipelines.yml` it removed 11
  PowerShell lines (`$basePath`, `$keyPath`, `[System.IO.File]`, …). A `- key: |`
  list item now measures its indent including the `- ` marker (the `yamlwalk`
  rule) — without that, the item's sibling keys (`env`, `displayName`,
  `condition`) looked like block content; the corpus tier caught it.
- **Manifest-pack nodes carry a `Location`** (S3). Pack-emitted nodes had no
  file:line, so they could not be cited or passed to `verify` — the store's core
  contract. Real line numbers for yaml and xml; json falls back to the
  file-level `L1` (`encoding/json` discards positions) rather than inventing one.
- **Manifest-pack node ids are namespace-scoped** (S4). The id was
  `<file>::task:<name>` while the label was `<ns>:<name>`, so two rules yielding
  the same name for one file collided on id and one was silently dropped.
  Now `<file>::task:<ns>:<name>`. **This changes existing pack node ids** (no
  packs ship bundled, so only user packs are affected).
- **The documented manifest-pack example now works** (S5). `"target/@name"`
  matched nothing on a real Ant `build.xml` because selectors are root-anchored
  and exact-depth; it is `"project/target/@name"`. The package doc now states
  the selector's actual limits instead of implying a descendant match.
- **`grammar build` honors `CTX_OPTIMIZE_GRAMMARS`** (S6). The pack loader
  honored it and the builder did not, so a built pack could land where nothing
  would ever load it. Resolution now lives once in `store.GrammarsDir()`, shared
  by loader and builder — which also removes an inverted dependency in which the
  extractor imported the toolchain builder. A third site (`languages remove`)
  had the same bug.

### Changed

- **Docs tell the truth about sources and the extension doors** (ADR
  `openspec/changes/2026-07-25-structured-formats/`, S8 — coverage is the
  user's lane via adapters/packs, ours is that what we emit is honest). The
  skill + the committed usage card now document the **query** side of a
  captured source (the kind vocabulary `database`/`schema`/`table`/`view`/
  `column`/`collection`/`key_prefix`/`cluster`/`topic`/`consumer_group`/
  `server`/`stream`/`bucket`/`prefix`/`api`/`path`/`operation`/
  `securityScheme`, relations `contains`/`references`/`uses`, worked
  `nodes`/`edges`/`card` examples) and state plainly that **source subgraphs
  are ISLANDS**: a connector only ever sees a URL, so there is no
  code↔table/topic/config_key/endpoint edge and "which code writes this
  table" is not answerable. Spec routes and code routes are disclosed as
  separate, unjoined `route` nodes with identical labels (measured 0% join
  rate), as is the spec-format split (openapi connector: JSON only; the
  in-repo route lane: YAML only). `deps` ecosystem coverage is named
  (npm · go · maven · gradle · nuget · pypi · crates), with Ruby/PHP called
  out as an adapter case and pip-compile locks as deliberately skipped.
- **New skill reference `references/extending.md` — the traps in the doors we
  point users at.** A grammar pack whose declaration names duplicate existing
  ones silently deletes working `calls` edges (measured on real Apache beam:
  126 correct edges destroyed, 1.10% of 11,501, plus 1 invented wrong edge;
  66.6% repo-wide name-collision rate) — count `calls` edges before and after
  adding a pack; qualified labels do not fix it. Manifest-pack selectors are
  root-anchored and exact-depth, `*` matches exactly one level with no
  descendant operator, two attributes of one element cannot be paired, and
  `emit` is only `dependency|task` — anything else belongs in an adapter
  script.

## [0.9.1] — 2026-07-24

### Fixed

- **Definitions beat import stubs and tests** (ADR
  `openspec/changes/2026-07-24-answer-quality/`, F1/F2 — the measured `card`
  0.66 judge score). `card url_for` on flask returned the `module://url_for`
  import stub (no signature, no body, no file:line) because exact-label ties
  broke by smallest ID; `query "where is url_for defined"` ranked tests and
  stubs above the definition. Now: real declarations outrank `module://` stubs
  at every resolve tier, and query downranks stubs (×0.25) and test-file nodes
  (×0.5) unless the question itself asks about imports/tests. `card url_for` →
  `src/flask/helpers.py L200-L251`. No perf change (~10ms queries).
- Instructions marker no longer stamps `vv0.8.0-…` on dev builds.

### Changed

- Agent skills + committed usage card teach the 0.9.0 surface:
  `--include-content`, `sync --adapters/--all/--no-wiki`, autosync config,
  `node`/`impact` aliases.
- Golden net pins the 0-change resync short-circuit (message + ceiling) — the
  incremental-sync win may only move down.

## [0.9.0] — 2026-07-24

### Added

- **Fast incremental re-sync** (ADR `openspec/changes/2026-07-24-lazy-autosync/`,
  levers 1+2). A 0-change re-sync short-circuits on a stat-only tree signature
  BEFORE engine init (~0.73s → **~0.03s**); the 32 MB wasm engine compiles once
  into a disk-backed wazero cache (cold gather ~0.91s → **~0.57s**); git-history
  and wiki regen skip when provably unchanged. Byte-identical to a `--force`
  rebuild — pinned by six scenario tests (edit/add/delete/folder/ignored/module).
- **Lazy autosync on query + `sync` = the resync verb** (ADR
  `openspec/changes/2026-07-24-lazy-autosync/design.md`, lever 3). A read verb on
  a stale store can now bring itself current — config-gated, **code-only**, off by
  default:
  - `.ctxoptimize/config.json` `"autosync"`: `"off"` (default) | `"lazy"` |
    `"block"`; accepts a bool too (`true`→lazy, `false`→off). Global default in
    `~/ctxoptimize/config.json`; env override `CTX_OPTIMIZE_AUTOSYNC`.
  - **lazy** — a stale read spawns a detached child `sync` and answers NOW from
    the current store (0 ms added latency); the next read sees the refresh. One
    sync in flight, guarded by a PID lockfile in the store (no stampede). Detach
    is per-GOOS (`Setsid` on Unix, `DETACHED_PROCESS|NEW_PROCESS_GROUP` on
    Windows).
  - **block** — a stale read resyncs inline first, then answers (always fresh).
  - Staleness is lever 1's tree-signature, so it catches **uncommitted** edits
    (git-HEAD freshness would miss them). Scope is LOCKED to code: auto-sync never
    runs adapter scripts and never dials a native source.
- **`sync` is now the first-class resync verb**: code + local producers by
  default (no dial), incremental via levers 1+2. `sync --adapters` also re-runs
  adapter scripts; `sync --all` also refreshes native sources (dials).
- **Resync skips the wiki** (ADR `openspec/changes/2026-07-24-wiki-scale/`).
  Query/card/affected read the graph, never the wiki, so autosync (lazy child +
  block inline) refreshes the graph only; `--no-wiki` on `add`/`sync` is the
  explicit escape hatch. At Linux scale wiki regen was ~98% of a 24.7-min gather
  — a resync no longer pays it. Explicit `add`/`sync` still build the full wiki.
- **`--include-content` on `query`/`card`**: opt-in on-demand source hydration —
  the cited `file:line` range is read from disk at answer time and returned
  inline (`content_error` when unreadable, never a silent empty body). Nothing
  is stored; the pointer-only store stays 3–10× smaller than body-storing rivals.
- **Partition-and-quarantine gather**: one invalid node no longer discards the
  whole index — bad items are quarantined and reported, the rest commit. Proven
  on full Linux: 0 B before → 2.85M nodes / 4.6M edges, 9 items quarantined.
- **Field-standard aliases**: `node` (= `card`), `impact` (= `affected`).

## [0.8.0] — 2026-07-24

### Added

- **Native, portable, jq-free filter surface** (ADR
  `openspec/changes/2026-07-24-portable-export-consumption/`). No read verb
  ever needs `jq`/`python` again — one shared in-process predicate engine
  (`internal/graphfilter`), federated across all modules at a repo root.
  - **`nodes` / `edges` / `deps` verbs** — filter by kind, file-type,
    relation, confidence, id-prefix, label, from/to, producer, scope, and
    `--where k=v`/`k~v`; `--select` projects fields; table by default,
    `--json`/`--ndjson` for machines. `deps --importers` returns
    dependency → scope → importing-files in ONE command (retires the
    multi-line `export | jq` join). Measured ~4× faster than `export | jq`
    with 47–64× less memory on a 220k-edge store, and the only path that runs
    on stock Windows/Alpine.
  - **`export`** gains the same filter flags + `--ndjson`; bare `export` is
    byte-identical (non-breaking).
  - **`query`** pre-rank narrowing (`--kind`/`--where`/… ranks WITHIN the
    filter); **`affected --kind`** post-filters the blast set (e.g. tests);
    **`hubs --kind`** ranks within a kind. All gain `--ndjson`.
- **Top-level `scope` on dependency nodes** (issue #5 F1) — the field
  consumers reach for first is populated (`metadata.scopes` kept for
  back-compat).
- **Undeclared-dependency drift signal** (issue #5 F2) — a scoped npm import
  (`@scope/pkg`) with no declared dependency is flagged as a queryable
  `undeclared_dependency` node + file edges (`ctx-optimize nodes --kind
  undeclared_dependency`) — the "imported but never declared" signal for
  architecture-drift analysis.

## [0.7.0] — 2026-07-24

### Added

- **Code → dependency links + normalized scope** (ADR
  `openspec/changes/2026-07-23-code-dependency-edges/`, issue #5). A new
  `deplink` producer bridges the code lane's `module://<import>` targets to
  the manifest lane's `dep:<ns>/<name>` nodes with `resolves_to` edges, so
  the graph answers "which files use package X" and `affected dep:npm/react`
  crosses the dependency boundary to every importing file. Resolution is
  exact for npm (subpath-stripped) and go (longest-prefix, the repo's own
  go.mod module skipped), unambiguous-prefix-only for maven/nuget —
  ambiguous candidates dropped, never guessed; all links `INFERRED` +
  `synthesized_by`. Dependency scope is now filterable without a hardcoded
  framework ignore-list: each `declares` edge carries a normalized
  `scope_class` (`runtime|dev|peer|optional|test|build|indirect`) beside the
  raw section name, and each `dep:` node carries a `scopes` aggregate.
  Measured: +0.4–1.7% edges, ≤1.4 ms on a 220k-edge monorepo store
  (`spikes.md`).
- **Native sources — an env var holding a URL is the whole contract** (ADR
  `openspec/changes/2026-07-17-bundled-adapter-templates/`). Databases,
  buckets, queues, and external APIs enter the store with one declaration:
  the env var's value is a URL, the URL scheme picks the connector.
  **9 connectors**, all pure Go, wire-protocol-native (no pg_dump/atlas/tbls
  on any machine): postgres, mysql, mongodb, redis, kafka, nats, s3
  (stdlib SigV4 — minio-go banned for its 15ms init), mssql, openapi
  (http(s) or a spec file path).
  - **Verbs**: `add <ENV_NAME>` (resolve → dial → capture → merge → record
    in config `sources` on success only); `capture <ENV_NAME>` (one
    connector, Batch JSON on stdout, no store write — the composition/debug
    primitive, also the callback for adapter scripts doing tunnels/vaults);
    `adapters list` (recorded sources + schemes); `adapters help <scheme>`
    (setup card generated from the connector's own parameter table — never
    drifts from code). `up` re-captures recorded sources after the gather
    under a **24h TTL** (`--sources=always|never`), reports per-source
    outcomes — captured | skipped | failed (failed keeps prior nodes) —
    with staleness ages, and reconciles undeclared source producers
    (`--prune-sources`). Unset var = a clean one-line skip so teammates
    without credentials still `up`; `--strict` turns those into CI
    failures. A repo with no sources adds zero cost to the gather path.
  - **Secret hygiene by construction**: argv takes env-var NAMES only
    (`^[A-Z_][A-Z0-9_]*$`); a literal password in a committed entry is a
    hard error at load; values resolve process env → `.ctxoptimize/.env` →
    root `.env` (validated dotenv subset; origins reported name-only; a
    git-TRACKED `.env` warns loudly) in memory at dial time; stored ids go
    through a fail-closed textual sanitizer (never `net/url.Parse`, which
    echoes full URLs and chokes on real AWS secrets); every output —
    errors, summaries, panics — passes a value-scrub choke; a hermetic
    grep gate plants a fake password and greps the entire store tree plus
    all output (wrong-password and panicking-connector cases included).
    `.ctxoptimize/.gitignore` (scaffolded) covers `.env*` by construction.
  - **The logical-shape rule**: every connector captures what a developer
    reasons about, never physical/instance data — system schemas/dbs/topics
    skipped, a partitioned table is ONE node with `partitions: N`
    (chunks/children never enumerated), redis is a bounded prefix-pattern
    SCAN summary, s3 lists prefixes only (depth-capped), mongo fields from
    a capped sample; any truncating cap is reported in the summary line.
  - **Measured (postgres, 5-run medians, connect included, localhost)**:
    a 100-table / 3-schema / 1,307-column corpus captures in **31 ms** —
    vs pg_dump `--schema-only` 101 ms, atlas 248 ms, tbls 1,356 ms — and on
    the trap corpus (1 table with 100 partitions + 500 fake Timescale
    chunks = 706 raw tables) it emits **101 logical tables** with
    `partitions: 100` as a fact, where the others emit 606–716. Filtering
    is free (naive unfiltered: 33.8 ms).
  - **Companion binary**: drivers live in **`ctx-optimize-adapters`**
    (19.7 MB), shipped beside the main binary in every archive and npm
    package. The main binary keeps **zero driver imports** — 43.2 MB
    unchanged, query p50 within noise (compiling the drivers in breached
    the ≤+10% query gate at +13%) — and execs the sibling (names-only argv;
    the child re-resolves the same env ladder) only when a source dials.
    Companion missing → loud error naming the binary + install hint.
- **`.ctxoptimize/instructions.md` — the committed usage card** (same ADR,
  Scaffold additions). `init`/`up` scaffold and refresh a self-contained
  card — intent table, verify discipline, store-vs-grep ladder, sources
  flow, remote push/pull, `up` — inside a version-stamped managed block:
  refresh is **upgrade-only** (an older binary never rewrites a newer
  file's block) and never touches text outside the markers. Teammates'
  agents inherit full usage with zero installation; the CLAUDE.md/AGENTS.md
  pointer blocks shrink to the one-liner verb discipline plus a reference
  to the card.
- **Skill surfaces**: `references/sources.md` (the sources flow, env-var
  routes, skip semantics, companion note), the adapter-callback pattern in
  `references/adapters.md`, and `source-add`/`source-capture`/
  `adapters-catalog` routes in `activation-routing.xml`.

## [0.4.2] — 2026-07-17

### Added

- **`verify` — deterministic citation checking** (ADR
  `openspec/changes/2026-07-16-verify-verb/`; maintainer: "the model gets
  too hallucinated, need some way to get defensive"). Before a human acts
  on a claim: `ctx-optimize verify "<node-id | exact-label |
  file:L10-L20>" ...` — node exists (EXACT only, verify never fuzzes),
  file exists, line range in bounds, and drift vs the gather-time git HEAD
  the store already records. Verdicts `ok | drifted | missing-node |
  missing-file | out-of-range`; exit 0 only when ALL claims hold, so hooks
  and CI can enforce grounding. Untracked/non-git files report drift
  `unknown`, never a false clean.
- **Ambiguity-aware resolution — safe by default** (graphify audit: its
  `explain` silently answers about the nearest prefix match; that bug
  class is now refused). Fuzzy ties on card/explain/affected/path/
  change-plan return ranked "pick one" candidates instead of guessing;
  `--fuzzy` opts into the top candidate and the answer STAYS labeled.
  Every resolution reports `resolved_via: exact-id | exact-label |
  last-segment | fuzzy` (JSON + text banner). Fuzzy hits also need ≥half
  the asked tokens — a junk name can no longer resolve off one stray
  common token (caught by the new probe suite).
- **Grounding probe suite** (`internal/golden/grounding_test.go`) — the
  anti-hallucination tier: six adversarial probes where the RIGHT answer
  is a refusal, a labeled fuzzy match, or a failed verification. Runs in
  every `go test` / `task golden`.
- **Two-sided ladder in the skill** — replaces the absolutist gate: a
  tool-choice table (symbols/structure → store; literals/config
  values/comments → grep directly, say so), READ the cited range when
  behavior matters (explicitly not a violation), two misses = switch
  tools, verify before a human acts, abstain over padding. Hook context
  carries the one-line version.

  **Measured (2026-07-17, gates held):** ambiguity-refusal rate on both
  judged 20-question scoreboards: 0 (floors 16.5/20 unchanged); bench
  unchanged (subprocess query p50 19.2ms, 1597 tok/call — within the
  ≤+10%/≤+20% gates; gather within ≤+5%); `verify` ≈ card-class latency
  (store-load dominated, ~50ms cold for 3 claims on a 2.1k-node store).

## [0.4.1] — 2026-07-16

### Changed

- README: pkg.go.dev badge (#3). Docs-only release.

## [0.4.0] — 2026-07-16

**Breaking.** The remote is now YOUR script (ADR
`openspec/changes/2026-07-16-scripted-remote-transports/`): the binary
ships no transport of its own — `remote push` / `remote pull` run the
commands you declare in the committed config. The built-in `file://` +
`s3://` transports and `remote init` are gone.

### Added

- **`up` — THE command** (ADR `openspec/changes/2026-07-16-up-verb/`,
  amended: "the fundamental people should love"). One idempotent verb goes
  from ANY state to a store that answers: **no config → bootstraps it**
  (monorepos via scan `--yes`, curatable after) and gathers; empty store +
  declared `remote.pull` → run it (falls back to a local gather, loudly);
  empty store, no remote → gather; store stale vs git HEAD → fast
  re-gather (adapter scripts skipped); fresh → no-op. The whole
  getting-started story is `npm i -g @muthuishere/ctx-optimize &&
  ctx-optimize up`. `init` stays for authors wanting control and on a
  pull-declaring clone redirects to `up` instead of pulling itself. Every
  onboarding surface (pointer blocks, global rule, skill routes, docs)
  teaches `up`. CI gate: `ctx-optimize up && ctx-optimize fresh`.

### Changed

- **`remote push` / `remote pull` execute declared commands.**
  `.ctxoptimize/config.json` carries the transport:
  `{"remote": {"push": "node .ctxoptimize/push.js", "pull": "…"}}` — any
  shell line (js, py, sh, inline). The binary resolves scope, runs the
  command (cwd = repo root), and hands it `CTX_STORE_DIR`,
  `CTX_STORE_KEY`, `CTX_SCOPE_PREFIX` (module scope), `CTX_DIRECTION`.
  Non-zero exit fails the verb. Same trust model as adapters. `init`'s
  auto-pull-on-clone now runs the declared pull command.
- **`init` scaffolds an inert git-lane transport** —
  `.ctxoptimize/push.js.sample` + `pull.js.sample` (zero-dep node: a git
  repo hosts every store) and a rewritten `remote.example.md` (git / s3 /
  custom lanes). Arming = rename two files + add the config block.
- **The skill authors transports**: on "set up sharing" the agent arms the
  samples or writes the script, declares the commands, and commits — no
  chat-recipe retyping.

### Removed

- `internal/remote` (file:// + s3:// SigV4, tree sync, manifest-diff
  transfer), `remote init` (incl. `--local` and the store-local
  config.json it wrote), and the `${VAR}` credential resolver — secrets
  stay env-var NAMES that the shell expands at run time.

### Migration (v0.3 → v0.4)

| You had | Do this |
|---|---|
| `remote init file://…` (git-hosted folder) | arm the scaffolded `push.js.sample`/`pull.js.sample`, set `STORE_REPO_URL`, declare the config block |
| `remote init s3://…` | save the s3 lane script from `remote.example.md` (aws CLI), declare it for push + pull |
| `remote init --local` | move the commands into the committed config (per-machine remotes are gone) |
| legacy config (`"remote": "s3://…"` or `{type,url,credentials}`) | loads fine but is inert; push/pull print this migration pointer |

## [0.3.11] — 2026-07-16

### Changed

- **Skill: push/pull teaches both hosting lanes end-to-end.**
  `references/push-pull.md` now carries complete, executable setup recipes
  instead of an abstract `remote init <url>`: **Lane A** — a private GitHub
  repo as the store host (`gh repo create/clone` → `remote init file://…` →
  push + git publish → teammate clone + pull; store artifacts are sorted
  ndjson, so git diffs them cleanly); **Lane B** — an S3-compatible bucket
  (AWS/R2/MinIO/Hetzner) including the `${VAR}` credentials object for
  non-AWS endpoints. Both mirror the `.ctxoptimize/remote.example.md` that
  `init` scaffolds, and the skill tells agents to follow that file when
  present. The `remote-init` route and SKILL.md share row trigger on "set
  up sharing over github / a bucket".

## [0.3.10] — 2026-07-16

### Added

- **`update` now updates the binary too.** One command, whole tool: the
  binary lane runs first — npm-managed installs delegate to
  `npm install -g @muthuishere/ctx-optimize@latest` (wrapper +
  optionalDependencies stay in sync); goreleaser standalone binaries
  download the platform asset from GitHub Releases, verify sha256 against
  the release's `checksums.txt`, and swap atomically (any failure leaves
  the current binary untouched); dev builds and unrecognized installs are
  left alone with a note. Then skills + hooks + global rule refresh from
  the binary that is NOW current (via subprocess when it just changed, so
  the new bundle lands). `update --check` reports without touching
  anything. This is user-invoked network ONLY — same doctrine as `grammar
  build`'s zig download; the binary never checks for updates in the
  background. `CTX_OPTIMIZE_UPDATE_API` / `CTX_OPTIMIZE_UPDATE_DL`
  override the endpoints for tests and mirrors.

## [0.3.9] — 2026-07-16

### Added

- **`update` — refresh every installed surface after a binary upgrade.**
  `ctx-optimize update` re-runs the install lanes (skills + hooks + global
  rule, same platform/flag selection as `install`) and prints the npm
  one-liner for updating the binary itself — the CLI never phones a
  registry (deterministic contract: no network except your remote).

- **Skill installs are now an EXACT replace.** The bundle is staged in a
  temp sibling and swapped in, so files an older version shipped but the
  current one dropped are removed instead of lingering as stale orphans an
  agent might read. Local edits to installed skill files are restored to
  bundled truth.

### Changed

- **`uninstall` no longer requires `--skills`** (still accepted): plain
  `ctx-optimize uninstall` removes everything `install` wrote — skill dirs,
  hook entries (surgically: shared files like `~/.claude/settings.json`
  only lose our `UserPromptSubmit` entry), and the global rule. Stores and
  committed repo pointer blocks stay, and the report says so.

## [0.3.8] — 2026-07-16

### Added

- **`sync` — the fast lane.** `ctx-optimize sync` re-gathers the repo you're
  in (code, docs, manifests, git; prunes deleted, re-emits changed, refreshes
  wiki + navigator) but **skips adapter scripts**, which can be arbitrarily
  slow (DB dumps, doc converters). Skipping is safe: replace is
  producer-scoped, so adapter nodes stay put — `sync` prints how many were
  skipped. Takes no path by design (`add <path>` for another repo);
  `add . --no-adapters` is the same thing spelled long.

- **`adapters <list|run [name]>` — the slow lane, on demand.** Re-run every
  adapter script or just one by name when the external system changed (schema
  migrated, topics moved) — running one adapter never disturbs the code
  graph. Skill surfaces (SKILL.md routing, sync.md, adapters.md,
  activation-routing.xml) all route the two lanes.

- **`init` scaffolds `remote.example.md`** next to config.json — the
  push/pull setup as commented recipes (git-repo host and S3/R2 bucket),
  since JSON can't carry comments. `${NAME}` is baked in at scaffold time;
  every other `${VAR}` survives verbatim for env-time resolution. Scaffold
  templates now live as real files under `internal/project/templates/`
  (go:embed), not backtick-escaped Go strings.

- **`init --instructions CLAUDE|AGENTS|ALL|NONE`** picks which agent
  instruction files get the pointer block (persists to config; re-running
  `init` is idempotent — identical pointer content is never rewritten).

- **Agent pointers route BY INTENT.** Every surface (global block, per-repo
  pointer, SKILL.md frontmatter + hot-path table) now teaches the intent
  router — find→`query` · inspect→`card` · edit→`change-plan` ·
  impact→`affected` — instead of a flat verb list.

- **`change-plan` — the first composed one-call verb (A2 + A1 + tests-for).**
  `ctx-optimize change-plan "X"` answers "I'm about to change X" in ONE
  bounded call: signature, callers, blast radius, **which tests to run**
  (the derived tests-for view — affected filtered to test declarations, no
  persisted edge), historical co-changes, and a confidence footer separating
  extracted from inferred edges and co-change evidence. Output is capped per
  section with overflow summarized (`--json` for everything).

  **Measured on this repo: 229 tokens in 1 call vs 2,270 tokens across the
  query+card+affected chain it replaces — ~90% fewer answer tokens.** Against
  the bench's session finding (100 calls ≈ 150–190k tokens), routing
  change-intent questions here is the first real cut. Skill routing updated
  (SKILL.md hot-path row + activation-routing route); alias: `plan`.

- **Dev-env lane, first slice: task-runner facts.** Taskfile.yml (+ env
  variants), Makefile, and justfile targets become `task` nodes — same shape
  as npm scripts (`<file>::task:<name>`, label `task:`/`make:`/`just:`,
  command + desc metadata, line-anchored) — so "how do I build/test/run this
  repo" is answerable from the graph. Literal-or-silent: variables, pattern
  rules, `.PHONY`, assignments, and settings emit nothing. Landmarked in the
  golden fixture; all floors/scores/bench gates held (judge 16.5/16.5 — L19
  stays a gap: linux `block/Makefile` genuinely has no rule targets, only
  `obj-y` config keys, which L16 already answers). Known follow-up: the
  config lane also indexes these files as `config_key` nodes — overlapping
  facts to dedupe when the lane grows.

- **Golden acceptance suite** (`internal/golden/`) — the never-break net.
  Hermetic fixture repos (a multi-module config repo with a multi-path
  `src/`+`tests/` .NET module; a plain csproj/sln repo) are pinned as exact
  snapshots + query-ranking goldens in every `go test ./...`. Pinned real
  corpora run env-gated locally and via `.github/workflows/golden.yml`
  (shallow clones at fixed refs).

  **Baseline scores AND performance (measured locally 2026-07-16 before
  commit; both are enforced — extraction floors at the exact measured
  numbers, performance ceilings at ~10× measured wall so slow CI passes but
  an order-of-magnitude regression fails. Neither the score nor the speed may
  regress without a deliberate, reviewed spec change):**

  | Corpus | Nodes (floor) | Edges (floor) | Gather measured / ceiling | Probe query measured / ceiling |
  |---|---|---|---|---|
  | linux v6.9 `block/` | 8,163 | 12,007 | 0.6–1.1s / 12s | 8ms / 1500ms |
  | Newtonsoft.Json 13.0.3 (multi-path src+tests) | 10,131 | 19,194 | 1.3–2.6s / 25s | 33ms / 1500ms |
  | fixture: multimod config repo | exact snapshot (76 lines) | — | ~0.4s / 10s | ranking goldened |
  | fixture: csproj/sln repo | exact snapshot (23 lines) | — | ~0.4s / 10s | ranking goldened |

  Landmarks enforced alongside: `ll_back_merge_fn` / `blk_rq_merge_ok` /
  `elv_rqhash_add` + calls-into floors (linux); `JsonConvert` /
  `JsonSerializer` classes + **344** cross-split test→source calls floor
  (Newtonsoft); cross-split call edge, npm dep+task, go.mod dep, k8s image
  (fixtures). Query latency reference on this repo's live metrics: query avg
  7.0ms (n=92), card 0.6ms (n=91).

- **Judged Q&A scoreboard** — 20 agent-shaped questions per corpus, each
  routed through the same verb the skill teaches (query/card/affected/path)
  and marked deterministically (1 / 0.5 / 0). Gap-marked questions are
  deliberate zero-scorers documenting known weaknesses — the target list for
  the next feature. The floor is enforced: the score may only move UP, in a
  reviewed diff.

  **Marks (measured 2026-07-16, floors set at these values):**

  | Corpus | Score | Enforced floor | Known gaps (the next-feature target list) |
  |---|---|---|---|
  | linux-block | **16.5 / 20** | 16.5 | L17 gatekeeper ranks below top-5 lexically (0.5 — `trace` should fix); L18 `blk_rq_merge_ok` loses to wrappers (ranking); L19 Makefile *targets* not task nodes (dev-env lane); L20 tests-for has no in-tree tests to find |
  | Newtonsoft.Json | **16.5 / 20** | 16.5 | N17/N19 test files outrank source methods (ranking test-noise defect); N18 `PopulateObject` demoted (0.5); N20 no dotnet task facts (dev-env lane) |

  Notable passes: N14 "which tests exercise SerializeObject" — the derived
  tests-for view working end-to-end via `affected`; N15 NuGet deps of the
  test project; L16 the iocost build-config key (Makefile config lane already
  answers it).

- **Bench harness (`task bench-extract`, ADR step 0)** — cold gather p50/p95
  (10 runs), 5-file incremental refresh, query/card latency, peak RSS, store
  size, AND the agent-session cost model: subprocess spawn+answer per call
  and the 100-call session bill. Same-machine regression gates vs committed
  `proof/bench/baseline-*.json` (gather ≤+5%, query ≤+10% with a 5ms noise
  floor, RSS ≤+10%, output tokens ≤+20%).

  **Agent-cost baseline (2026-07-16): latency is NOT the cost — output
  volume is.** Subprocess query ≈ 19–29ms/call (100-call session ≈ 2–3s
  wall), but each query answer is ~1,500–1,900 tokens, so a 100-query
  session feeds the agent **~150–190k tokens** of answer text. This is the
  measured basis for choosing the next requirement: cut tokens-per-answer
  (terse mode / tighter default budget) and calls-per-session (composed
  verbs) — not shave milliseconds.

## [0.3.7] — 2026-07-16

### Fixed

- **Viewer crashed on mount for every store** ([#2]). The Viewer tab threw
  `Cannot access 'de' before initialization` and fell back to the error
  boundary; all other tabs worked. Root cause was a temporal dead zone in
  `ForceGraph`: the mount effect called `resize()` synchronously, which ran
  `requestDraw()` → `wake()` → `requestAnimationFrame(loop)` while `loop` was
  still an uninitialized `const` declared ~160 lines below (minified to `de`).
  Not store-specific and not a circular import. `loop` is now a hoisted
  function declaration.
- **Local builds always reported `0.0.0-dev (none, unknown)`.** Only goreleaser
  injected the version ldflags, so `task build` produced an unstamped binary and
  `task local-install` *copied* it — the copy then went stale silently. `build`
  now stamps `Version`/`Commit`/`Date` from git, and `local-install` symlinks
  onto `PATH` so it always tracks the last build.
- **Release notes leaked `docs:`/`chore:` noise.** goreleaser's changelog filters
  used bare `^docs:` regexes, which never matched this repo's scoped commits
  (`docs(skills):`). Filters now allow an optional scope, drop merge commits, and
  group Features/Fixes.

### Added

- **Dashboard UI tests + a CI job for them.** The UI ships as a committed
  `go:embed` dist, so `task ci` and `go install` stay node-free — which also
  meant no Go test could ever see a crash inside the bundle (exactly how the
  Viewer bug shipped). `ForceGraph.test.tsx` now mounts the component under
  jsdom and runs its effects, and CI gained a `dashboard` job (tsc + vitest).
- This CHANGELOG.

## [0.3.6] — 2026-07-15

### Added

- **The skill exposes the full CLI surface.** `references/activation-routing.xml`
  routes every verb as a `<route>` with its trigger, goal, and exact command —
  answer, build, customize, share, export, learn, and manage — plus the gate
  rules and disambiguation.
- **A global "knowledge graph before grep" rule.** `install` now writes a
  marker-fenced block into `~/.claude/CLAUDE.md` and `~/.codex/AGENTS.md`: use
  the store where a `.ctxoptimize/` exists, and offer to create one where it
  doesn't. `uninstall` strips it. Self-gates on `command -v ctx-optimize`, so
  it's inert if the binary isn't installed.
- **Per-build-system module-parsing assets.** Deriving `modules[]` from a build
  system is the agent's job, so it gets one asset each:
  `modules/dotnet-sln.md`, `gradle.md`, `maven.md`, `js-workspaces.md`,
  `naming-fallback.md`, plus `config-json.md` for the config contract itself.

### Fixed

- **Minified/generated bundles no longer pollute the graph.** Committed dist
  output and `*.min.js` sit under the size cap and aren't gitignored, so they
  were indexed — one minified line parses into thousands of junk symbols that
  dominated `hubs` and `query`. Files whose longest line exceeds 50KB are now
  skipped by shape (language-agnostic). Re-gathering this repo pruned 437 junk
  nodes.

## [0.3.5] — 2026-07-15

### Added

- **Modules across folders (multi-path modules).** A module is a name plus a
  *set* of paths: `{"name":"Billing","paths":["src/Billing","tests/Billing.Tests"]}`.
  Scattered source and tests gather into one store in a single pass, so
  test→source calls resolve across the split instead of breaking at the folder
  boundary.
- **One-step clones.** `init` detects a committed config with a `remote` and no
  local store, and pulls the prebuilt graph instead of rebuilding from source.

### Fixed

- **One bad node can no longer blank the whole viewer.** A malformed node is
  dropped or cleaned on its own and every healthy node still renders, with an
  error boundary as a last resort. Covered by unit tests.

### Changed

- The agent-instruction pointer block is XML-gated: it checks
  `command -v ctx-optimize` first, so a committed `CLAUDE.md`/`AGENTS.md` is
  inert on a machine without the binary.

## [0.3.4] — 2026-07-14

### Added

- Viewer node detail opens source — VS Code / file / GitHub blob links.

### Fixed

- Viewer force-graph settles and stops, plus a node cap — no more tab crash on
  large graphs.

## [0.3.3] — 2026-07-14

### Added

- Viewer producer filter (adapters / files / docs filterable alongside kinds).
- Global context/cost-saved stat on the Overview screen.

## [0.3.2] — 2026-07-14

### Added

- Dashboard: project-scoped settings, add packs from the UI, repos cache +
  reload, Overview landing screen; the viewer first-classes route/dependency/k8s
  kinds.
- The skill teaches the full v0.3 surface: `onboarding.md` + `dashboard.md`
  references, hardened `customize.md`, triggers for setup/onboard/serve/manage.

## [0.3.1] — 2026-07-14

### Added

- First-class customization helper: `references/customize.md` teaches agents to
  add framework routes, k8s, build-tool deps, and new languages via drop-in
  packs (`routes` / `manifests` / `languages add`).

### Changed

- Dashboard UI redesigned to match the site aesthetic — green accent, system
  fonts, responsive, across all screens.

## [0.3.0] and earlier

The v0.3 line established the current shape: tree-sitter code extraction
compiled to WASI (12 embedded languages + drop-in grammar packs), markdown docs,
framework routes, build-tool dependencies, Kubernetes topology, git co-change,
the local dashboard (`serve`), sync-only remotes (`file://` + `s3://`), the
agent skill, and the npm distribution. See the git history for detail.

[#2]: https://github.com/muthuishere/ctx-optimize/issues/2
[0.3.7]: https://github.com/muthuishere/ctx-optimize/releases/tag/v0.3.7
[0.3.6]: https://github.com/muthuishere/ctx-optimize/releases/tag/v0.3.6
[0.3.5]: https://github.com/muthuishere/ctx-optimize/releases/tag/v0.3.5
[0.3.4]: https://github.com/muthuishere/ctx-optimize/releases/tag/v0.3.4
[0.3.3]: https://github.com/muthuishere/ctx-optimize/releases/tag/v0.3.3
[0.3.2]: https://github.com/muthuishere/ctx-optimize/releases/tag/v0.3.2
[0.3.1]: https://github.com/muthuishere/ctx-optimize/releases/tag/v0.3.1
