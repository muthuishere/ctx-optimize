# Changelog

All notable changes to ctx-optimize. Format loosely follows
[Keep a Changelog](https://keepachangelog.com/1.1.0/); versions are
[semver](https://semver.org/) and match the published npm package
(`@muthuishere/ctx-optimize`) and the GitHub release tags.

The contract never changes: **the binary is deterministic — no LLM, no DB, no
embeddings, no MCP, no network except your configured remote.**

## [Unreleased]

### Added

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
