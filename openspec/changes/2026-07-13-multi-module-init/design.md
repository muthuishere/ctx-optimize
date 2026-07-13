# Design — multi-module: scan, mirrored stores, navigator, federation

Mechanics for the ADR in proposal.md. House rules apply throughout: stdlib
only, deterministic output, sorted/atomic artifacts, fail closed.

## internal/scan (new)

```go
type Module struct{ Path, Name, Marker string } // Path repo-relative, slash-form
type Options struct{ Depth int; Markers, Include, Exclude []string }
func Scan(root string, o Options) ([]Module, error)
```

- One bounded `filepath.WalkDir`: prune noise dirs (`.git node_modules vendor
  dist build target .venv .next __pycache__` + `Exclude` globs), stop below
  `Depth` (default 5, measured from root).
- A directory ≠ root containing any marker becomes a module root. Built-in
  markers: `go.mod go.work package.json pom.xml settings.gradle
  settings.gradle.kts build.gradle build.gradle.kts Cargo.toml pyproject.toml
  setup.py` + a child `.ctxoptimize/` dir. Config `scan.markers` appends;
  `scan.include` globs force-add dirs with no marker.
- Exhaustive: never stops early; records `Marker` (evidence) per module;
  reports when markers sat exactly at the depth boundary (suggest --depth+).
- Nested module roots are all kept (multi-level); sorted by Path.
- Default module Name = Path with `/`→`-`.

## project.Config additions

```go
Modules  []scan.Module `json:"modules,omitempty"`   // root config: the generated, owned list
ModuleOf string        `json:"module_of,omitempty"` // child config: "<root-key>" (opt-in)
Scan     *scan.Options `json:"scan,omitempty"`      // generator knobs
```

## Verbs

- `scan [path] [--depth N] [--json]` — read-only: prints found tree +
  the exact proposed config.json. `--json`: `{modules:[...], clipped:bool}`.
- `init --scan [--yes] [--depth N] [--modules "globs"]` — scan → print →
  confirm (y/N on stdin unless `--yes`) → write FULL list to root
  config.json `modules[]` + scaffold root (adapters/, agent pointer). No
  child scaffolds. Plain `init` unchanged; plain `init` inside a dir some
  ancestor root declares as a module ADOPTS (writes `module_of` child
  config, store = mirrored key).

## Store keys (mirrored)

- Module store key = `<rootKey>/<modulePath>` — new `store.SanitizeKeyPath`
  sanitizes per segment, preserves `/`. `store.Open(root, key)` already
  joins paths; nested dirs come free. Root store stays `<rootKey>/` (its
  graph = root residual tree).
- Existing single-module repos: key has no `/`, nothing changes.

## add fan-out

At a path whose resolved config has `modules[]`:

1. Expand globs → concrete module list (sorted). Recurse into child configs
   (multi-level; each dir once, first declaration wins).
2. Worker pool `min(NumCPU,8)` (`--jobs N` overrides; `--jobs 1` serial):
   each worker = existing single-module gather (markdown + code + module's
   own adapters if child .ctxoptimize/ present) → module store. Per-module
   output buffered; printed in list order (bytes independent of schedule).
3. Root residual: `ExtractExcluding(root, moduleDirs)` — extractors gain an
   exclude list (absolute dirs pruned in the walk); `Extract(root)` stays
   as `ExtractExcluding(root, nil)`.
4. Regenerate navigator + root wiki front page + manifests. Any module
   failure fails the run (fail-closed).
5. `add` inside one module: that module only + navigator entry refresh.

## internal/navigator (new)

Written into the ROOT store dir on every multi-module add:

- `modules.json`: `{version:1, root:"acme", modules:[{name, path (pre-
  expanded, concrete), store, nodes, edges, languages, hubs:[top 100 by
  degree], summary (first README heading line), updated}]}` — sorted,
  pretty-printed. Pre-expanded paths make cwd→module an O(1) prefix match.
- `navigator.md`: rendered table + per-module hub lines; doubles as the
  unified wiki front page (`wiki/index.md` links relative into each module
  store's own wiki — same mirrored root, plain relative links).

## Resolution (all read verbs + add)

`resolveScope(f)`:

1. Walk cwd→up to nearest `.ctxoptimize/config.json` (stop at FS root).
2. Nearest config: `module_of` set → MODULE scope (store = mirrored key).
   Has `modules[]` → cwd inside a pre-expanded module path → MODULE scope;
   else ROOT scope. Plain config → SINGLE scope (today's behavior).
3. No config found → if `<basename>` store exists, SINGLE (compat); else
   error "no store — run `ctx-optimize init`". Read verbs stop creating
   store dirs (read-only open).

Query behavior:

- MODULE: query module store. Zero hits → escalate: enclosing module (if
  nested) → ROOT federation. Each block labeled `[scope]`.
- ROOT: rank modules from modules.json (name+summary+hubs term match), take
  top K=3 (`--modules all|a,b`, `--root` forces from module scope), CONCAT
  selected stores' nodes/edges (+ root residual) → one query.Run pass
  (single ranking, deterministic). Zero hits at K → widen to all.
- card/affected/path/explain: MODULE scope → module store; symbol miss →
  navigator hubs lookup → owning store (labeled); still missing → modules
  in sorted order, first match. ROOT scope → concat ALL module stores +
  residual (cross-module edges absent by construction; boundary note when
  an affected/path result touches a node whose store ≠ scope store).

## Sync

`remote push/pull` resolve scope the same way: MODULE pushes its prefix,
ROOT pushes the whole `<rootKey>/` tree. S3 keys already mirror store paths
— no protocol change. Merged stores are never synced.

## Skill (internal/skills/SKILL.md)

New flow: monorepo signals → `scan --json` → show FULL list → user okay →
`init --scan --yes` → `add .` → queries route via navigator; adapter
creation flow (agent authors adapter scripts into .ctxoptimize/adapters/,
env-var NAMES only).

## Tests

Hermetic (t.TempDir + CTX_OPTIMIZE_STORE): scan depth/prune/exhaustive/
clipped-report; config round-trip; key mirroring; fan-out determinism
(--jobs 1 vs N byte-equal store trees); exclusion (no double extraction);
navigator content; walk-up resolution from nested dirs; escalation ladder;
adoption rule. Integration: beam / the-factory / nexus-workspace manual
matrix in tasks.md.
