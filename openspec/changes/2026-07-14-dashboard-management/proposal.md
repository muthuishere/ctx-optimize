# ADR — dashboard FIRST-CLASS: React app, onboarding, viewer, management, logged

Status: DRAFT v2 — owner direction 2026-07-14 ("dashboard first class,
onboard add edit repo as well along with inbuilt viewer so create a react
app and embed"). Owner's ask upgrades Phase 2 mutations from ⚖️ to APPROVED
for: onboarding (scan/init/add), repo config edit, re-gather trigger.

## Decision 0 — first-class React app, embedded, zero runtime network

- `dashboard-ui/` at repo root: Vite + React + TypeScript. Runtime deps
  minimal (react, react-dom, one bundled graph-viz lib for the viewer —
  d3-force or cytoscape, VENDORED through the build, never a CDN).
- Built `dist/` is COMMITTED (the treesitter.wasm precedent: `go install`
  and CI must never need Node) and embedded via go:embed in
  internal/dashboard, served by the existing `serve` verb on 127.0.0.1:4747.
  The single-file UI it replaces is deleted.
- Contract unchanged at runtime: ZERO external requests — the built bundle
  is self-contained; CSP-style check in tests (no http(s):// asset refs in
  dist).
- Dev loop: `task dashboard-dev` (vite proxy → running serve),
  `task dashboard-build` (build + copy into embed dir) — dev-only targets,
  like scripts/wasm/build.sh.

## Decision 0b — onboarding + viewer are core screens

- **Onboard**: pick/enter a repo path → server runs the same code paths the
  CLI uses (scan → confirm module list → init → add with streamed
  progress). Edit repo = the config editing below + re-add.
- **Repos**: every store under the store root listed with freshness,
  counts, navigator link; add/edit/remove (remove = delete store dir,
  confirm-gated, audited).
- **Viewer**: interactive graph per module (nodes/edges from /api/graph,
  filter by kind/producer, neighborhood expand on click, community coloring
  once W2's Communities is exposed via API). Budgeted: server caps the
  payload (top-N by degree + on-demand neighborhoods), never ships 1.5M
  nodes to a browser.

## Context

`serve` today is deliberately read-only: embedded single-file UI +
`/api/modules|graph|query`, bound 127.0.0.1:4747, zero external requests,
read path never creates store dirs. Meanwhile the extension surface has
grown to four axes (grammar packs, route packs, manifest packs, adapters)
plus two config levels (global, project) — all plain files, editable today
only via CLI/editor. The owner wants the dashboard to be the place a human
SEES all of it and can CHANGE it, with changes logged.

## Decision — two phases, mutation stays localhost + audited

### Screens (owner 2026-07-14: "settings and all stuff within and they can
do all operations there as well") — Repos · Onboard · Viewer · Query ·
Settings · Changes. Settings = every config key (both levels), every pack
across all four axes (create/edit/delete with validation preview), adapters,
remote (init/push/pull triggers). All operations the CLI offers, same doors.

### Phase 1 — visibility (no writes)

New read endpoints + UI sections:

- `/api/setup` — effective config (global + project per module, with source
  level shown), discovered packs per axis (name, source path, rules/exts),
  adapters list, producers present in each store with node/edge counts and
  last-add provenance (source.json head + age, freshness verdict).
- UI: a "Setup" tab — config table (value + which level set it), pack cards
  per axis with their file contents rendered, per-producer contribution
  bars, freshness badge per module.
- Everything links to the owning FILE path — the file stays the source of
  truth.

### Phase 2 ⚖️ — guarded mutation (the "change and update anything")

Design constraints (non-negotiable, from the standing contract):

- Binds 127.0.0.1 ONLY — mutation endpoints refuse non-loopback remotes
  even if --host is widened (read may widen; write never does).
- Mutations are FILE EDITS through the same validated doors the CLI uses
  (project.Save, SaveGlobalConfig, pack validate-then-write) — the
  dashboard writes nothing the CLI couldn't; invalid input fails with the
  same loud errors.
- Scope of v1 mutations: config keys (both levels), pack files
  (create/edit/delete per axis with validation preview), trigger `add .`
  re-gather per module, remote push/pull trigger. NOT store surgery (no
  direct node/edge editing — the graph is derived truth; changing it by
  hand breaks the producer-scoped Replace contract).
- CSRF: same-origin check + a per-session token embedded in the served
  page (stdlib; no cookies needed for localhost single-user).

### The log ("logged so people can change")

`<store>/audit.ndjson` — append-only, one line per mutation from ANY door
(dashboard AND CLI verbs route through the same writer): ts, actor
(dashboard|cli), action, target file, before-hash → after-hash. Plain,
sorted-field, git-diffable like every store artifact. Surfaced in the UI as
a "Changes" feed; `ctx-optimize log` prints it (new tiny verb, read-only).
No secrets ever logged (values redacted by the same env-name discipline).

## Success checks

- Phase 1: every pack/config/adapter visible in one screen with its file
  path; freshness + producer counts accurate against `status --json`.
- Phase 2: editing a route pack in the UI → next `add` uses it; the edit
  appears in audit.ndjson with hashes; a malformed pack edit is rejected
  with the CLI's own error text; mutation over a non-loopback connection is
  refused (test with httptest on a non-loopback listener).
- Read-only guarantee of today (`serve` never creates store dirs) still
  holds for the read paths.

## Non-goals

- No auth system / multi-user — localhost single-operator only; team
  changes travel via git (committed packs/config), not via the dashboard.
- No graph editing.
- No remote/hosted dashboard (the wedge spec's Option C stays rejected).
