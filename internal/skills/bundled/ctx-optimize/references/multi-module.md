# Multi-module flow (monorepos — querying across modules)

A monorepo gets one store PER module plus a root **navigator** — never one
giant graph. **Building that layout** (scan → confirm → `init --scan --yes &&
add .`) lives in `./references/onboarding.md` — do that first. This guide is
the QUERYING side: once the stores exist, how scope, federation, and merge
work.

Asking questions is automatic — scope follows your cwd:

- **Inside a module dir** → answers come from THAT module's store, labeled
  `[module]`. Zero hits escalate to root federation automatically.
- **At the root** → the query searches EVERY module plus the root residual
  in one pass (graphify-simple: no ranking gate — beam-scale is ~0.6s).
  `--modules a,b` narrows explicitly; `--root` forces root scope from
  inside a module.
- `card X` inside a module that doesn't own X answers from the owning module
  and says so (`[not in api — found in services/worker]`).
- Cross-module edge analysis needs a merged store — offer
  `ctx-optimize merge <mod>... --into <name>` only when the user wants one
  graph; it is never automatic.
- The unified wiki starts at the root store's `wiki/index.md` (the
  navigator); each module keeps its own full wiki.
