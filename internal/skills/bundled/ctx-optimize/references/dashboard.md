# Dashboard — the visual management surface (`serve`)

`ctx-optimize serve` (alias `dashboard`) starts a first-class local UI and
prints a `http://127.0.0.1:4747` link — give it to the user. It's an embedded
single-file app: ZERO external requests (no CDN ever), read-safe, and every
mutation is loopback-only. Offer it whenever the user wants to SEE the store,
manage packs/config visually, or onboard/re-gather repos interactively —
instead of you narrating CLI output.

## The tabs

- **Overview** — global + per-repo stats: store count, node/edge totals,
  languages, token-usage rollup. The at-a-glance "what's indexed" screen.
- **Repos** — onboard a new repo (runs the same scan → confirm flow as the
  CLI), re-gather (`add .`) an existing one, or remove a store. The
  interactive alternative to `./references/onboarding.md`.
- **Query** — the lexical query box (same engine as `query`), pick a module,
  read ranked cited hits without leaving the browser.
- **Viewer** — the force graph. Expand from a center node, and filter by kind
  — code decls, routes, dependencies, k8s resources, config — to see just the
  subsystem you care about.
- **Settings** — config at BOTH levels (repo `.ctxoptimize/` + machine
  `~/ctxoptimize/`) and every pack across all four axes (routes / manifests /
  languages / adapters), each with its file path. Add packs from the UI here.
- **Changes** — the audit feed: who changed what, when, before/after hashes.

## What the agent should know

- **It's local and safe.** Binds 127.0.0.1; reads never create store dirs.
  Mutations (onboard, re-gather, remove, config set, remote push/pull) are
  refused off-loopback even if `--host` widened the bind, and each carries a
  per-process token.
- **Every mutation is audited.** Anything changed in the UI lands in the same
  append-only feed as CLI `config` changes. Read it back with
  `ctx-optimize log --json` (or the Changes tab) — dashboard edits show
  `actor: dashboard`, CLI edits show `actor: cli`.
- **It is a surface, not a second brain.** The UI routes through the SAME
  command functions the CLI dispatches — nothing happens in the dashboard
  that a CLI verb couldn't do, so you can always fall back to the terminal.
- Prefer `serve` for exploration, pack/config management, and interactive
  onboarding; prefer the CLI for scripted or single-shot answers.
