# Multi-module flow (monorepos — scan, confirm, fan out)

A repo with many projects gets one store PER module plus a root **navigator**
— never one giant graph. The flow is scan → show → okay → write → add:

1. **Scan (read-only):** `ctx-optimize scan --json` (add `--depth N` if the
   tree is deep; default 5). This finds ALL projects by build-file markers.
2. **Show the user the FULL list** — every found project, not a sample. If
   they say something is missing, re-scan deeper or add globs. Never skip
   this confirmation and never silently build a single giant graph.
3. **On okay:** `ctx-optimize init --scan --yes` — writes every found module
   into `.ctxoptimize/config.json` `modules[]` (generated once; the user owns
   and edits the list after) and scaffolds the root.
4. **Gather:** `ctx-optimize add .` at the root fans out one worker per
   module in parallel (`--jobs N` to tune) and writes the navigator
   (`modules.json` + `navigator.md` in the root store).

Asking questions after that is automatic — scope follows your cwd:

- **Inside a module dir** → answers come from THAT module's store, labeled
  `[module]`. Zero hits escalate to root federation automatically.
- **At the root** → the navigator ranks modules and federates the query
  across the best-matching ones (`--modules all|a,b` to widen/pin, `--root`
  to force root scope from inside a module).
- `card X` inside a module that doesn't own X answers from the owning module
  and says so (`[not in api — found in services/worker]`).
- Cross-module edge analysis needs a merged store — offer
  `ctx-optimize merge <mod>... --into <name>` only when the user wants one
  graph; it is never automatic.
- The unified wiki starts at the root store's `wiki/index.md` (the
  navigator); each module keeps its own full wiki.
