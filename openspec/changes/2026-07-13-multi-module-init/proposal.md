# ADR — multi-module init: deep scan, per-module stores, top-level navigator

Status: DRAFT v2 — after owner discussion 2026-07-13. Decisions marked ⚖️ are
still open; everything else is decided.

## Context

ctx-optimize today is strictly single-module:

- `init` (`internal/app/app.go` cmdInit) scaffolds ONE `.ctxoptimize/` in one
  directory and one store `~/ctxoptimize/<basename|config-name>/`.
- `add .` at a monorepo root walks the whole tree into that ONE store — a
  20-package monorepo becomes one undifferentiated giant graph. One graph for
  a very big repo is too big to be useful: people work in ONE module at a
  time; the giant graph costs build time, sync bytes, and query noise.
- `merge <module|path>... --into <name>` already exists (cmdMerge) and is the
  right primitive: per-module stores stay canonical, the merged store is
  derived and re-derivable, provenance preserved. It stays **opt-in**.
- Extraction is parallel *within* one add (wazero instance per worker
  goroutine) but there is no orchestration *across* modules.
- The skill layer told agents to "init && add ." — with no module awareness,
  so on monorepos it silently built the too-big single graph (owner-observed
  failure that triggered this ADR).

### Prior art — graphify (read from `~/muthu/gitworkspace/graphifyread`)

graphify's multi-module story, and where it stops:

1. **Per-directory builds, manual.** You `cd services/api && graphify update`;
   each directory built gets its own `graphify-out/`. The central store
   (`docs/graph-store.md`) keys module data by repo-relative path
   (`~/graphify-store/<repo>/services/api/graphify-out`) — the mirrored
   layout is proven. But there is **no auto-discovery of module boundaries**
   (detect.py is file-type detection only) and **no parallel multi-module
   build** — the user is the orchestrator.
2. **`merge-graphs g1.json g2.json --out merged.json`** — ad-hoc merge with
   repo-tag prefixing for id collisions (#1729). Output is a dead file.
3. **Global graph** (`graphify/global_graph.py`): `~/.graphify/global-graph.json`
   + `global add/remove/list` — their cross-project merge; a growing global
   singleton, exactly the "one giant graph" failure at a bigger scale.
4. Founding-spec gap G6: "how do I combine graphs?" is a top open ask.
5. **No navigator.** Nothing at the top level tells an agent *which* module
   graph answers a question — you must already know where to look.

## Decision (shape)

Four moves, each independently shippable:

1. **Module scan is an OPTION, not the default** — `ctx-optimize scan`
   (read-only discovery) + `init --scan`; plain `init` stays single-module.
   The skill offers the scan as a choice too.
2. **Store tree mirrors the repo tree** — one store per module, not one giant
   graph. (In the user's HOME store, never in the repo — see assumption.)
3. **A top-level NAVIGATOR** is the root artifact — a small module index the
   agent reads first to route into the right module store. Root queries
   federate through it.
4. **Merge stays opt-in** — `merge --into` when someone actually wants one
   graph; never automatic.
5. **The embedded skill ships updated in this change** — SKILL.md
   (internal/skills, fanned to claude+codex by `install --skills`) gets the
   multi-module flow: detect monorepo signals → `scan --json` → show the
   user the full list → on okay `init --scan` → fan-out `add` → route
   queries via the navigator. The skill update is in scope, not a follow-up.

**Assumption (stated because "graph.json on each project" is ambiguous):**
per-module graphs live in the central store `~/ctxoptimize/…`, mirroring the
repo's folder structure — NOT as graph.json files inside the user's repo. The
house rule stands: `.ctxoptimize/` is the ONLY thing we put in a repo.

### 1. Discovery UX: SEE → CONFIRM → WRITE, as three separable verbs

The flag-soup version of this (`init --scan --depth --modules --single --yes`)
was rejected as UX — the better way is to separate *seeing* from *writing*:

```
ctx-optimize scan                # READ-ONLY. Finds ALL projects (default depth 5,
                                 #   --depth N), prints the tree AND the exact
                                 #   config.json it would write. Changes nothing.
ctx-optimize scan --json         # machine form for the skill / scripting

ctx-optimize init                # unchanged today: single module, this dir
ctx-optimize init --scan         # scan + confirm + write modules[] + scaffold
                                 #   (interactive on TTY; --yes for automation;
                                 #    --modules "globs" to skip detection)

ctx-optimize config              # show the CURRENT state: config.json contents,
                                 #   available modules (declared vs discovered diff),
                                 #   which have stores, per-module freshness
```

- `scan` is the preview: its output IS the proposed `config.json` — you see
  exactly what init would commit before anything is written.
- `init --scan` is `scan` + a confirmation + the write. Nothing is scaffolded
  until the shown list is accepted; on accept, EVERY found project is written
  to `modules[]` — the full list, and the user prunes the committed file if
  they want fewer.
- `config` is the standing "what do I have?" verb — config.json + the
  discovered-vs-declared diff (a module added since last scan shows as
  `+ services/newthing (not in config)`), so drift is always visible.
- **Skill flow:** `scan --json` → present the FULL list to the user →
  adjust depth/globs if something is missing → on okay, `init --scan --yes`
  with the accepted list → fan-out `add`. The skill offers multi-module as
  an option; it never scans-and-writes in one silent step, and never builds
  the single giant graph on a monorepo without being told to.

- Scan is a bounded walk (default max depth **5**, `--depth N` to change),
  pruned at `node_modules/.git/vendor/target/dist/build/.venv` etc., looking
  for **build-file markers**: `go.mod`/`go.work`, `package.json` (+
  workspaces/pnpm-workspace), `settings.gradle(.kts)`, `pom.xml <modules>`,
  `Cargo.toml [workspace]`, `pyproject.toml`, `.gitmodules`, and existing
  child `.ctxoptimize/`. Workspace manifests are read for declared members;
  bare markers found by walking count as module roots too (that covers repos
  with no workspace file — the case the skill got wrong).
- Nested markers: a marker under an already-found module root still registers
  (multi-level, e.g. `apps/mobile/android`); depth is from the scanned root.
- **Exhaustive, not sampled:** the scan finds ALL module roots within the
  depth bound — it never stops at the first few. If the depth bound clipped
  the walk (markers seen right at the boundary), the summary says so and
  suggests `--depth N+`.
- **Confirm before writing.** `init` prints the discovered tree and the plan
  (N modules → N stores + navigator) and asks for confirmation on a TTY;
  `--yes` skips for automation. Nothing is scaffolded or gathered until the
  list is accepted. Re-running `init` re-scans and shows a diff against the
  committed list (add/remove suggestions, never silent).
- **Expandable detection.** Detectors live in a registry (adding an ecosystem
  is add-only, like grammar packs), and the config can extend/override the
  built-ins without a new binary release:

  ```json
  "scan": { "depth": 8,
            "markers": ["build.zig", "Makefile"],
            "include": ["plugins/*"], "exclude": ["examples/**"] }
  ```

- 0 or 1 module found → behaves exactly like today (zero migration).

**Skill contract (internal/skills SKILL.md):** for a repo that looks
multi-project, the agent runs the scan, shows the user the FULL found list
(all projects, not a sample), waits for the okay (adjusting depth/globs if
the user says something's missing), and only then confirms init + runs the
fan-out add. The skill never silently picks a subset and never builds the
single giant graph on a monorepo.

Config field (root, committable):

```json
{
  "name": "acme",
  "modules": [
    { "path": "services/api" },
    { "path": "services/worker", "name": "worker" },
    { "path": "apps/mobile/android" }
  ]
}
```

### 2. Store layout mirrors the repo

```
~/ctxoptimize/acme/                    ← root store: navigator + root-residual graph
├── navigator.md / modules.json        ← §3
├── graph/ manifest.json wiki/         ← root tree MINUS module subtrees
├── services/api/                      ← full store (graph/ manifest wiki/) for that module
├── services/worker/
└── apps/mobile/android/
```

- Store key becomes `<root-key>/<repo-relative-path>` — mirrored, obvious,
  collision-free by construction (same property graphify's central store
  proved). `SanitizeKey` learns to keep `/` for module sub-paths.
- Each module store is complete and standalone: own graph, manifest, wiki.
  `query --path services/api` (or running from inside that dir) hits exactly
  that store — people on very big projects look at only one.
- A module subtree is EXCLUDED from the parent's walk — nothing extracted
  twice, file→module attribution unambiguous.
- Remote sync syncs the tree as-is; a teammate can pull one module's prefix.

### 3. The top-level navigator (the new root artifact)

The root store's first-class artifact is not a giant graph — it is a small
**module index**, regenerated on every add:

- `modules.json` (machine): per module — name, repo path, store path,
  languages, node/edge counts, top-N hub symbols, exported/public surface
  count, one-line summary (first README heading/line if present), manifest
  hash + last-gathered time (staleness visible).
- `navigator.md` (agent/human): the same as a wiki front page — a table of
  modules + hubs + "go here for X" lines. The agent-pointer block in
  CLAUDE.md/AGENTS.md points here first.
- **Federated root query:** `query` at the root consults the navigator, ranks
  modules by index match (names, hubs, summaries), fans the query out to the
  top-K module stores (K default 3, `--modules all|a,b`), and interleaves
  ranked hits under the existing budget — cited with module prefix. No merged
  store needed for the common "where is X across the repo?" question.
  `card`/`affected`/`path` at root resolve through the navigator to the
  owning module store; cross-module edges exist only in a merged store, and
  the output says so when it detects the boundary.
- `serve` reads the navigator for a module switcher; per-module dashboards
  come free (stores are just stores).
- **ONE wiki, many module graphs (owner call 2026-07-13).** Graphs are "just
  modules" — but the wiki is unified: `~/ctxoptimize/<root>/wiki/` is a
  single browsable tree whose front page IS the navigator and whose module
  sections relative-link into each module store's own wiki (they live under
  the same mirrored root, so links are plain relative paths — no copying,
  no duplication). A module gathered standalone still has its complete own
  wiki; the root wiki is just the one place a human or agent starts reading
  the whole repo.

### 3b. Resolution: WHERE you ask decides the scope (git-style upward walk)

Today there is no upward walk: `query` from `internal/store/` resolves store
key `store` (the subfolder's basename), finds an empty store, and answers
"no matches" — silently wrong (verified 2026-07-13). New rule, every read
verb (`query`/`card`/`affected`/`path`/`explain`/`serve`/`status`):

1. **Walk up** from cwd to the NEAREST `.ctxoptimize/config.json` — exactly
   how git finds `.git`. No config anywhere up the tree → today's error
   path ("no store — run init"), never a silent basename guess.
2. **The nearest config says what it is, and that decides the scope:**
   - Config declares itself a MODULE (`"module_of": "<root-key>"` —
     OPTIONAL, see resolved ⚖️1: modules normally have NO config of their
     own) → search THAT module's store first, full stop. The module's own
     config, when present, is the authority for "I am a module, my graph
     is here". Escalation (step 4) uses `module_of` to find the root.
   - Config is a ROOT (has `modules[]`) → locate cwd relative to
     `modules[]`: inside a declared module (one that predates child
     scaffolds or lost its config) → that module's store; at root / in the
     residual tree → navigator-routed federation (§3): rank modules, fan
     out to top-K, interleave within budget.
3. Explicit overrides always win: `--path <dir>` (any module),
   `--modules all|a,b` (widen), `--root` (force root federation from inside
   a module). `add` follows the same walk: run inside a module → re-gather
   just that module + refresh its navigator entry; run at root → full
   fan-out.
4. **Escalation ladder (scope resolution, innermost first).** From
   `beam/sdks/java/transform-service` the answer comes from THAT module's
   store first — the config being at `beam/` only tells the walk where the
   map is, not where the answer is. Escalation outward is automatic but has
   CRISP triggers, never a vague "seems thin":
   - `query`: module store returns **zero hits** → escalate one scope out —
     the enclosing module if nesting declares one (`sdks/java` when it is
     itself a module), else the root navigator federation. Each hop's
     results are labeled with the scope that answered
     (`[transform-service]`, `[root: 3 modules]`) so the reader always
     knows how far the answer traveled.
   - `card`/`affected`/`path`: **symbol not in the module store** → don't
     fail, don't full-federate — ask the navigator which module OWNS the
     symbol and answer from that store directly, labeled
     (`found in sdks/python`). Navigator-as-symbol-directory beats blind
     fan-out: one lookup, one store, precise.
   - Hits found locally → NO escalation; `--root`/`--modules` widen
     explicitly when the user wants repo-wide results despite local hits.
   - **Fast matching lives in the STORE, not the repo:** `add` writes the
     navigator's `modules.json` with globs PRE-EXPANDED to concrete paths,
     so cwd→module resolution is an O(1) prefix match after the walk.
     (Considered and REJECTED: a committed `.ctx-optimize-module.json`
     marker per module — the walk is already a handful of stats, and the
     marker re-creates duplicated identity + N generated files across the
     repo. Owner concurred 2026-07-13.)
5. Boundary honesty: a module-scoped `affected`/`path` whose blast radius
   crosses into another module says so in the output ("crosses into
   services/worker — federate with --root, or build a merged store for
   cross-module edges") instead of silently truncating.

### 4. `add` orchestrates: parallel fan-out, NO auto-merge

`add` at a root whose config has `modules[]`:

1. Expand `modules[]` (recursing into child configs — multi-level, each dir
   gathered at most once, first declaration wins).
2. Gather modules **concurrently** — worker pool (default `runtime.NumCPU()`
   capped at 8, `--jobs N`, `--jobs 1` = serial); each worker runs the
   existing single-module pipeline into that module's mirrored store. Output
   buffered per module, printed in declared order — byte-identical regardless
   of scheduling (CI: `--jobs 1` vs `--jobs N` byte-equality test).
3. Gather the root residual tree into the root store.
4. Regenerate the **navigator** (cheap — reads manifests + hubs per module).
5. A module failure fails the add loudly (fail-closed); `--force` per module.

**No automatic merged store.** `merge services/api services/worker --into
acme-all` stays the explicit opt-in for whoever wants one graph; `merge
--refresh <name>` (new) re-derives a previously saved merge (source list
recorded in the target store's config). Cross-PROJECT (multiple repos) is the
same verb — no graphify-style global singleton.

### 6. Sync + adapters in the multi-module world

- **Store root stays the home folder** (`~/ctxoptimize/`); the mirrored
  module tree under `<root-key>/` is what syncs. `remote push` at the repo
  root pushes the whole tree (all module stores + navigator + unified wiki);
  from inside a module it pushes just that module's prefix — same upward
  walk, same scope rule as queries. `pull` mirrors: a teammate can pull one
  module's prefix onto a fresh clone and work; pulling at root fans in
  everything. Existing `file://` + `s3://` remotes (stdlib SigV4) carry this
  unchanged — the mirrored layout means a module IS a key prefix, so
  partial sync needs no new protocol. Merged stores stay local-only
  (re-derivable, never synced).
- **DB / messaging / NoSQL / queue enter via adapters, and adapter CREATION
  is a SKILL job — the CLI only executes.** The binary keeps its vow: no
  DB drivers, no LLM, ever. The skill gains an explicit "create an adapter"
  flow: asked for postgres/kafka/mongo/rabbit/etc., the AGENT authors the
  adapter script into `.ctxoptimize/adapters/` (root-level for shared
  infra; inside a module's opt-in `.ctxoptimize/adapters/` when the source
  belongs to one module), reading connection values from env-var NAMES per
  the house secrets rule. `add` then just runs it through the validated
  `--json` door like any producer — deterministic CLI, intelligent skill,
  which is the founding architecture (CLAUDE.md) extended to multi-module.

## Counter-proposal under discussion — the omakase design (DHH lens)

Owner asked for a Rails-style rethink (2026-07-13). Applying *convention over
configuration* to everything above:

**The heresy in v2:** `modules[]` in config.json is a SECOND copy of truth the
repo already declares. `go.work`, `pnpm-workspace.yaml`, `settings.gradle`,
`Cargo.toml [workspace]`, `.gitmodules` — these ARE the committed, reviewed
module list. Writing them into our config duplicates them, and duplicated
truth drifts (hence the "declared vs discovered diff" machinery — machinery
that exists only to manage a problem we created). Same for the confirm step:
Rails never asks "are you sure?" — it does the conventional thing and shows
you what it did; you read the output like you read a migration.

**Omakase golden path (three words, no flags):**

```
ctx-optimize init          # once. done.
ctx-optimize add .         # reads the repo's OWN manifests at add time →
                           #   N module stores + navigator, in parallel.
                           #   prints the module tree it built.
ctx-optimize query "x"     # navigator-routed federation, from anywhere.
```

- Module boundaries are **derived fresh at add time** from the committed
  workspace manifests — deterministic (same repo state → same modules), zero
  drift, zero config. A new package in pnpm-workspace.yaml is a new module
  store on the next add, automatically.
- `config.json` holds **deviations only**: `exclude`, `name` overrides, extra
  module globs for repos with NO manifest (the only case that needs config),
  `depth`. Empty config = fully conventional repo = nothing to commit beyond
  what init scaffolds today.
- `scan` survives as the **preview/debug verb** (the `rails routes` analog):
  "what will add treat as a module, and which marker says so." Read-only,
  also `--json` for the skill.
- No confirm gate in the golden path. `add` on a fresh monorepo prints
  `12 modules (go.work) → 12 stores + navigator` — reading that IS the
  confirmation; if it's wrong you add one exclude line and re-add. The
  skill's interactive confirm becomes: run `scan`, show the user, okay,
  `add` — a skill-level courtesy, not a CLI gate.
- Dies with this: `init --scan`, `--yes`, the `config` verb (it's `cat
  .ctxoptimize/config.json`), the declared-vs-discovered diff machinery,
  `modules[]` as primary mechanism.

**The trade** (what the explicit v2 design bought that this gives up):
a frozen, committed module list that can't change under you at add time vs
conventions that track the repo automatically. Rails answer: the manifest
change was itself committed and reviewed — tracking it is correct, freezing
is the bug. Escape hatch stays: a repo that hates its manifests writes
explicit module globs in config and convention defers to it.

**Resolution (owner, 2026-07-13): the Rails-GENERATOR pattern, not runtime
inference.** The repo-level `config.json` stays and holds ALL the modules —
but as *generated, owned code*: `scan`/`init --scan` writes the full found
list (like `rails generate` writes files), and from then on the user owns it
— hand-add a module, rename, exclude, "do stuff". `add` reads config only,
never scans. What we take from omakase is the UX, not the inference:

- Golden path stays three words per repo class — single repo: `init` →
  `add .`; monorepo: `scan` → `init --scan` → `add .`.
- No confirm-flag soup: `scan`'s printed config IS the preview; drift shows
  up as a diff the next time someone runs `scan` (generator re-run), never
  as hidden machinery inside `add`.
- Manifests (go.work, workspaces, gradle, …) are detection EVIDENCE for the
  generator, not a runtime source of truth — so a repo with no manifests, or
  hand-added module entries pointing anywhere, works identically.

## What this is NOT (scope guards)

- No watching, no daemon — derived artifacts refresh on explicit verbs only.
- No import-graph inference of module boundaries — markers, manifests, globs;
  the user edits the committed list; the binary obeys.
- No change to the emit schema or the `--json` door.
- Navigator is deterministic text derived from stores — never an LLM summary.
- Store artifacts stay sorted/atomic/diffable.

## Consequences

- The skill's monorepo flow becomes correct: `scan` shows everything, the
  user okays, `init --scan` writes, `add` fans out, the agent lands on the
  navigator. Fixes the observed skill failure (silent single giant graph)
  while keeping plain `init` untouched for single repos.
- Query cost scales with the module you're in, not the repo: big-project
  users load one module store; the navigator (KBs, not MBs) is the only
  always-loaded artifact.
- Directly out-executes graphify G6 + their missing discovery/orchestration/
  navigator, while keeping their proven mirrored-store layout.
- New surface to test: scan determinism, exclusion correctness (no double
  extraction), federation ranking, navigator staleness flags.
- `status` must show the module tree + per-module freshness.

## Open questions ⚖️

1. ~~Scaffold child `.ctxoptimize/` in each module, or root-only?~~
   **RESOLVED (owner, 2026-07-13): root-only by default** — `init --scan`
   writes ONE `.ctxoptimize/` at the root; modules get no config dir (no
   tool-spray across N packages, one source of truth). A child config is
   OPT-IN and honored when present (module needs own adapters/remote, or
   standalone use): the upward walk prefers it (§3b.2). Running plain
   `init` inside a dir an ancestor root already declares as a module
   ADOPTS it — writes the minimal child config (`module_of`) and reuses
   the mirrored store — never mints an independent basename-keyed store
   that shadows it.
2. Agent-pointer blocks per module dir, or root only? (Rec: root only — the
   navigator routes; module-level CLAUDE.md pointers = N files to maintain.)
3. Federated query default K (3?) and how hits are interleaved across modules
   within one budget — per-module quota vs global rank?
4. `--jobs` default cap (NumCPU vs 8 — wasm instance ≈32MB each).
5. Does the root residual store earn its keep, or should root-level stray
   files fold into the navigator only? (Rec: keep residual — root docs/
   READMEs are exactly what onboarding queries hit.)

## Traceability

- Owner direction (2026-07-13): `scan` is its own verb, separate from `init`
  (scan = see, init = write) and opt-in; depth ≥5 and configurable; single
  giant graph.json rejected; per-project graphs mirroring folder structure;
  merge strictly optional; top-level navigator wanted; scan must find ALL
  projects (expandable detectors), confirm with the user, and EVERY found
  project goes into config.json `modules[]` — the full list, not a subset.
- graphify evidence: `docs/graph-store.md` (mirrored central store, manual
  per-dir builds), `graphify/global_graph.py` (global-graph singleton — the
  anti-pattern), `graphify/cli.py` L1404+ (merge-graphs + #1729), detect.py
  (no module discovery), founding spec G6
  (`openspec/changes/2026-07-11-graphify-gaps/proposal.md`).
- ctx-optimize ground truth: cmdInit `internal/app/app.go:243`, cmdAdd
  `internal/app/app.go:277`, cmdMerge `internal/app/app.go:866`,
  `internal/project/project.go` Config/Scaffold, store.ModuleKey
  `internal/store/store.go:57`.
