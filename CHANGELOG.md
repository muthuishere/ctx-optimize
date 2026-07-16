# Changelog

All notable changes to ctx-optimize. Format loosely follows
[Keep a Changelog](https://keepachangelog.com/1.1.0/); versions are
[semver](https://semver.org/) and match the published npm package
(`@muthuishere/ctx-optimize`) and the GitHub release tags.

The contract never changes: **the binary is deterministic — no LLM, no DB, no
embeddings, no MCP, no network except your configured remote.**

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
