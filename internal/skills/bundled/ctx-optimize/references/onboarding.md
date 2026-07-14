# Onboarding a repo — set it up (scan → confirm → init → add)

This is the SETUP side. Querying a store that already exists is the routing
table; building one for the first time is here. `.ctxoptimize/` is the ONLY
thing written into the user's repo — commit it so the whole team inherits it.

## Single project (one build root, one language stack)

```
ctx-optimize init && ctx-optimize add .
```

`init` scaffolds `.ctxoptimize/config.json` (name + adapters dir + example
pack samples) and writes the AGENTS.md/CLAUDE.md pointer block. `add .`
gathers code + markdown + manifests + routes + git co-change in seconds.
Confirm with `ctx-optimize status --json` (nodes > 0) and one `query`.

## Monorepo — you MUST scan and confirm before you build

Never init a monorepo blind: one giant graph is wrong, and you don't know the
module list. Drive this exact loop:

1. **Scan (read-only):** `ctx-optimize scan --json` (`--depth N` if the tree
   is deep; default 5). It finds every project by build-file markers —
   nothing is written.
2. **Show the user the FULL found list** — every module, not a sample — and
   ask them to confirm. If something's missing, re-scan deeper or add globs;
   if something's noise, they'll drop it from the config after. Do NOT skip
   this and do NOT silently build a single graph.
3. **On their okay:** `ctx-optimize init --scan --yes` — writes every found
   module into `config.json` `modules[]` (the user owns the list afterward)
   and scaffolds the root.
4. **Gather:** `ctx-optimize add .` at the root fans out one worker per
   module in parallel (`--jobs N` to tune), building one store per module
   plus the root navigator (`modules.json` + `navigator.md`).

## After onboarding

- Verify: `status --json`, then a `query` — cite a real hit back to the user.
- Customize if the graph misses their routes / deps / k8s / language →
  `./references/customize.md` (check `routes/manifests/languages list` first).
- Non-code sources (DB, docs, queues) → `./references/adapters.md`.
- Share the store with the team → `./references/push-pull.md`.
- Querying the built monorepo (scope-follows-cwd, navigator, merge) →
  `./references/multi-module.md`. Onboarding builds it; that guide drives it.
- Interactive setup instead of the CLI? `ctx-optimize serve` → Repos tab
  onboards, re-gathers, and removes repos from the dashboard
  (`./references/dashboard.md`).
