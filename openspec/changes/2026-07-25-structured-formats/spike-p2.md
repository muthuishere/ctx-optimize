# Spike — P2: Python / Rust / Ruby / PHP dependency extraction

Date 2026-07-25. Throwaway prototype lived in a scratch dir; **no product code
touched**. Every number below is from a command reproduced in "Commands run".

## VERDICT

**Is P2 worth it — YES.** On a real Python repo (`corpus-flask`) ctx-optimize
today emits **0 dependency nodes and 0 `declares` edges** while the repo's
manifests declare **61 dependencies (41 distinct)**. What it emits instead is
**221 `config_key` nodes, 199 of them from `pyproject.toml`** — noise where the
fact should be. `deps` prints `(0 dependencies)` on that repo. Python manifests
appear in **14 of 52** top-level dirs under `~/muthu/gitworkspace`
(28 `pyproject.toml` + 39 `requirements*.txt` at maxdepth 5), Rust in **4**.

**Is it implementable stdlib-only — YES.** A 230-line TOML *table walker* in
the `yamlwalk` spirit (107 lines) extracted **1639/1639 dependency
declarations across 103 real manifests at 100.00% precision and 100.00%
recall**, judged against Python `tomllib` (a real TOML parser) as an
independent oracle. On a deliberately adversarial torture corpus it scored
**44/44 correct (100.00% precision, 2 conservative skips the oracle got
wrong)**. Runtime for all 103 files: **0.009s**.

**Scope recommendation: P2a (pyproject + requirements + Cargo), not P2b.**
Ruby/PHP prevalence in the sample is 1 `Gemfile` and 3 `composer.json`, all but
one inside vendored `chromium/third_party`. `composer.json` is JSON — it is
~20 lines on the existing `encoding/json` npm pattern whenever it is wanted;
`Gemfile` is executable Ruby DSL and is the only genuinely awkward one. Neither
justifies being in the first cut.

## 1. Prevalence (measured)

`find` at maxdepth 5, `node_modules`/`.git` excluded.

| manifest | `~/muthu/gitworkspace` | `~/ctx-bench-arena/corpora` |
|---|---|---|
| `package.json` (recognized today, baseline) | 277 | — |
| `requirements*.txt` | 39 | 1 |
| `pyproject.toml` | 28 | 5 |
| `Cargo.toml` | 10 (24 at maxdepth 6) | 2 |
| `composer.json` | 3 | 0 |
| `Gemfile` | 1 | 0 |

Top-level dirs under `~/muthu/gitworkspace`: **52**. Containing a Python
manifest: **14** (chromium, clojure-workspace, ctx-optimize, experimentss,
graphifyread, hari-projects, linux, nexus-workspace, raman-workspace,
secret-lib-workspace, skills-workspace, testrepos, vote, vscode-agent).
Containing `Cargo.toml`: **4**. Even `linux` and `chromium` — the golden
corpora — carry `pyproject.toml`.

`corpus-flask` carries 4 `pyproject.toml` + 1 `requirements.txt` + `uv.lock`.

## 2. Current yield on a real Python repo = zero (confirmed)

`ctx-optimize add ~/ctx-bench-arena/corpora/corpus-flask` into a scratch store
(`code: 2129 nodes, 3660 edges; manifests: 1 nodes, 1 edges` — the one
manifests node is `make:help` from `docs/Makefile`).

| probe | result |
|---|---|
| `ctx-optimize deps` | `(0 dependencies)` |
| `nodes --kind dependency` | `(0 nodes)` |
| `edges --relation declares` | `(0 edges)` |
| `nodes --kind config_key` | **221**, of which **199** sourced from `pyproject.toml` |
| `query "blinker dependency"` | returns `dependency-groups` / `dependencies` **key** nodes only — 10 hits, no `dep:` node |

Hand count from the manifests, reproduced by the prototype: **61 declarations,
41 distinct names** — root `pyproject.toml` 31, `examples/celery/requirements.txt`
21, and 3 each from the three example `pyproject.toml`. So the gap is not
partial: it is total, and the graph currently spends 199 nodes describing the
file that holds the answer without holding the answer.

Projected P2 delta on this repo: **+41 dependency nodes, +61 `declares` edges**
(prototype output; not yet an integrated measurement).

## 3. The stdlib TOML crux — a line walker IS good enough

Prototype: `tomlwalk.go` (230 lines, mechanism) + `main.go` (336 lines, the
three lanes) in the scratch dir. Mechanism = header tracking (`[a.b]`, `[[a.b]]`,
quoted segments), quote/bracket-aware comment cutting, logical-value joining
across continuation lines while `[`/`{` are unbalanced, top-level comma split,
inline-table field lookup. Compare: `yamlwalk.go` is 107 lines, `npm.go` 125.

Oracle: `oracle.py`, an independent implementation of the same semantics over
stdlib `tomllib`. Diff key = `(file, scope, name)`, plus a version-spec check.

### Real-world corpus (103 files: 48 pyproject + 24 Cargo + 40 requirements)

| metric | value |
|---|---|
| oracle declarations | 1639 |
| walker declarations | 1639 |
| exact `(file, scope, name)` matches | 1639 |
| false negatives | **0** |
| false positives | **0** |
| version-spec mismatches | **0** |
| precision / recall | **100.00% / 100.00%** |
| files with any disagreement | **0 of 103** |
| wall time, all 103 files | **0.009s** |

Scope mix in that corpus: `requirements` 840, `dependencies` 476,
`optional-dependencies` 134, `dependency-groups` 96, `build-system` 45,
`dev-dependencies` 16, `build-dependencies` 14, poetry `group` 14,
`target-dependencies` 6. Ecosystems: pypi 1366, crates 275.

Adversarial shapes actually present and handled in that corpus: 34 files with
multi-line `dependencies = [` arrays, 13 Cargo files with inline tables, 4 with
the `[dependencies.<name>]` sub-table form, 2 with `[target.'cfg(unix)'.dependencies]`,
10 requirements files using `-r`/`-e`/`--flags`, 12 pip-compile lock files.
`chromium/third_party/crabbyavif/src/Cargo.toml` is the nastiest real file —
inline tables whose `features = [` array spans lines (legal TOML) — and it
matched the oracle exactly.

The single most valuable real-corpus trap is `corpus-flask`'s own root
`pyproject.toml`: `[tool.tox.env.tests-min]` contains
`commands = [[ "uv", "pip", "install", "blinker==1.9.0", … ]]` — an
array-of-arrays of strings that *look* exactly like PEP 508 requirements. A
naive "any array of version-looking strings is a dependency list" heuristic
would emit ~12 false deps here. Table-anchored matching emitted **31, matching
the hand count exactly, 0 extras**.

### Torture corpus (7 hand-written files, legal TOML)

After two fixes described below: **44 walker deps, 44 exact matches, 0 false
positives**, recall 95.65% against the oracle. The 2 "misses" are **oracle
bugs, not walker bugs**: the oracle names a bare
`"https://example.invalid/direct.whl"` requirement `https`, and
`git+https://…#egg=xpkg` in a requirements file `git`; the walker refuses both
(any candidate name containing `/`, `:` or `\` is dropped). Combined
real+torture: 1683 walker deps, **100.00% precision, 99.88% recall**.

## Failure shapes found (be specific)

Shapes that broke the first draft of the walker, both real defects, both fixed
in ~25 lines:

1. **Multi-line basic strings (`"""` / `'''`) are not skipped.** A
   `description = """…"""` whose body contains the literal text
   `dependencies = ["not-a-dep"]` produced a **false-positive dependency**. Fix:
   track the open delimiter and skip lines until it closes.
   *Frequency in the 72 real TOML files: 0 — but it is a correctness hole, and
   cheap to close.*
2. **Inline dependency *table*: `[tool.poetry]` + `dependencies = { a = "^1", b = "^2" }`.**
   The whole dep table written as one inline table was **missed entirely (2 of 2
   deps)**. Fix: a case that splits the inline table and treats each field as a
   dep. *Frequency in real files: 0.*
3. **Version metadata lost for poetry `{ path = … }` / `{ git = … }`** (no
   `version` field): the walker reported `""` where the oracle reported
   `path:../local`. Cosmetic but it is the `version_spec` edge metadata. Fixed
   by sharing one `depVersion` helper across the pypi and crates lanes.

Shapes that turned out to be **non-issues**:

- **Multi-line inline tables** (`serde = {` newline `version = "0.5"` `}`) —
  the walker handles them; **TOML 1.0 forbids them** and `tomllib` rejects the
  whole file (`Invalid initial character for a key part`). The walker is
  strictly more permissive than the format. 0 real files.
- Comment lines and trailing `# comments` inside multi-line arrays; `#`, `]`,
  `[` and `=` inside quoted values; quoted keys with dots (`"weird.extra"`);
  `[target."cfg(windows)".dependencies]`; CRLF files; extras
  (`pkg[all,fast]>=2,<3`); env markers (`pkg ; python_version >= '3.11'`);
  direct references (`pkg @ https://…`) — all correct on the first draft.

Known conservative gap, deliberate: `git+https://…#egg=xpkg` in
`requirements.txt` yields **no** node rather than `xpkg`. Absent beats wrong is
house doctrine; if the `#egg=` fragment is wanted it is a 3-line addition.

## 4. Namespace and scope equivalents

Today (`internal/extract/manifests/manifests.go:74` `depNode`, plus the call
sites): `npm`, `go`, `maven` (`group:artifact`), `nuget` — id is
`dep:<namespace>/<name>`, node `Label` is the bare name, metadata
`ecosystem: <namespace>`. `declares` edges (`manifests.go:87`) carry
`version_spec` + raw `scope`, and `scopeClass` (`scopeclass.go:14`) normalizes
the raw section name into `runtime|dev|peer|optional|test|build|indirect`;
`applyScopeAggregates` unions those onto the dep node's `Scope`.

Proposed namespaces: **`pypi`**, **`crates`** (and `rubygems` / `packagist` if
P2b ever lands).

**Name normalization is required for `pypi`, and it is measurable, not
theoretical.** Across the real pyproject corpus there are **166 distinct raw
requirement names**; PEP 503 normalization (lowercase, `_`/`.` → `-`) changes
**3** of them (`flit_core`, `poetry_core`, `typing_extensions`) and collapses
the set to **165** — i.e. one real duplicate node avoided in a 28-file sample,
and it would recur on every repo that mixes `flit_core` and `flit-core`. Crates
names need no normalization (181 distinct, unchanged).

Scope vocabulary to add to `scopeClasses`:

| ecosystem | raw scope emitted | `scope_class` |
|---|---|---|
| pypi | `dependencies` (PEP 621 `[project]`) | `runtime` (already mapped) |
| pypi | `optional-dependencies:<extra>` | `optional` (needs a prefix rule) |
| pypi | `dependency-groups:<g>` (PEP 735) | `dev`, except `<g>` matching `test*` → `test` |
| pypi | `build-system` | `build` |
| pypi | poetry `dev-dependencies` | `dev` (**note: today's map has `devDependencies`, not `dev-dependencies`**) |
| pypi | poetry `group:<g>` | `dev`, `test*` → `test`, `docs` → `dev` |
| pypi | `requirements` (`requirements.txt`) | `runtime` |
| pypi | `requirements` from `requirements-dev.txt` / `-test.txt` | `dev` / `test` — derive from the **filename** |
| crates | `dependencies` | `runtime` (already mapped) |
| crates | `dev-dependencies` | `dev` |
| crates | `build-dependencies` | `build` |
| crates | `workspace-*` / `target-*` | same class as the base section |

`scopeClass`'s existing `strings.HasPrefix(lower, "test")` rule does **not**
cover `dependency-groups:tests` (prefix is `dependency`), so the prefixed
scopes need explicit handling — otherwise they silently get no class, which is
"absent beats wrong" but loses filtering on the most common Python dev groups.

One number worth a design decision: **840 of 1366 pypi declarations (61%) come
from `requirements*.txt`, and 12 of the 40 such files are pip-compile/uv lock
files** — i.e. transitive closures. `corpus-flask`'s single requirements file
alone contributes 21 of its 61 declarations, only 2 of which are direct. Unless
lock-style files are marked (a distinct raw scope, e.g. `requirements-lock`,
detectable from the `autogenerated by pip-compile` header), `deps` on a Python
monorepo will be dominated by transitive noise.

## What the implementation must handle

1. `manifestKind` (`manifests.go:245`) cases: `pyproject.toml`, `Cargo.toml`,
   `requirements*.txt` (basename prefix + `.txt` suffix, like the existing
   `taskfile.` prefix rule).
2. A shared `tomlwalk` package next to `yamlwalk`, same contract: *not* a TOML
   parser; deterministic; silent on what it cannot represent. Must implement
   header tracking (incl. `[[a]]` and quoted segments), quote-aware comment
   cutting, continuation-line joining, top-level comma split, inline-table
   field lookup, **and multi-line-string skipping** (failure shape 1).
3. pyproject lanes, table-anchored (never heuristic on array shape):
   `[project] dependencies`, `[project.optional-dependencies].<extra>`,
   `[dependency-groups].<g>`, `[build-system] requires`, `[tool.uv] dev-dependencies`,
   `[tool.poetry.dependencies]`, `[tool.poetry.dev-dependencies]`,
   `[tool.poetry.group.<g>.dependencies]`, the `[tool.poetry.dependencies.<name>]`
   sub-table form, and the inline `dependencies = { … }` form (failure shape 2).
   Skip poetry's `python` pseudo-dependency.
4. PEP 508 name extraction: strip env marker at `;`, direct reference at `@`,
   extras at `[`, then cut at the first of `< > = ! ~ ( , space`; refuse any
   candidate containing `/ : \` or not starting alnum; PEP 503-normalize.
5. Cargo lanes: `[dependencies]`, `[dev-dependencies]`, `[build-dependencies]`,
   each also under `[workspace.…]` and `[target.<cfg>.…]`, plus the
   `[dependencies.<name>]` sub-table form. Value may be a bare string or an
   inline table — take `version`, else `path:`/`git:`/`workspace`.
6. requirements.txt: skip blanks, `#` comments, and any line starting `-`
   (`-r` includes are **not** followed — the included file is scanned on its own
   as a repo file); cut trailing ` #`; derive dev/test class from the filename;
   flag pip-compile/uv-lock files so transitive closures are filterable.
7. `scopeClasses` additions per the table above, including prefixed scopes.
8. Golden net: `internal/golden` fixtures for a poetry repo, a PEP 621 +
   `[dependency-groups]` repo (corpus-flask is the natural one — pin
   41 distinct deps / 61 `declares`), a Cargo workspace with inline tables and
   a `[target.'cfg(…)']` section, and the tox-commands false-positive trap.
9. Additive only — new node kinds/edges on files that today produce no dep
   nodes. No existing node id moves, so unlike P3 this does not disturb
   existing golden snapshots (it adds to them).

## Commands run

```bash
# 1 prevalence
find ~/muthu/gitworkspace -maxdepth 5 -name pyproject.toml \
  -not -path "*/node_modules/*" -not -path "*/.git/*" | wc -l   # and per manifest
find ~/ctx-bench-arena/corpora -maxdepth 5 -name 'requirements*.txt'

# 2 current yield
export CTX_OPTIMIZE_STORE=<scratch>/store
ctx-optimize add ~/ctx-bench-arena/corpora/corpus-flask
cd ~/ctx-bench-arena/corpora/corpus-flask
ctx-optimize deps
ctx-optimize nodes --kind dependency
ctx-optimize nodes --kind config_key | tail -3
ctx-optimize edges --relation declares
ctx-optimize query "blinker dependency"

# 3 the crux (scratch dir: tomlwalk.go, main.go, oracle.py, cmp.py)
go build -o tomlspike .
tr '\n' '\0' < files-all.txt | xargs -0 ./tomlspike  > go.ndjson
tr '\n' '\0' < files-all.txt | xargs -0 python3 oracle.py > oracle.ndjson
python3 cmp.py go.ndjson oracle.ndjson
time ( tr '\n' '\0' < files-all.txt | xargs -0 ./tomlspike > /dev/null )
python3 -c "import tomllib; tomllib.load(open('torture/t3/Cargo.toml','rb'))"

# 4 namespace/scope
grep -rn "depNode(" internal/extract/manifests/*.go | grep -v _test
```

Prototype and corpora file lists are in the session scratch dir
(`scratchpad/spike-p2/`); nothing was written into the repo except this file.
