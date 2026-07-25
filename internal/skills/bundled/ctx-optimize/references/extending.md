# Extending coverage — the doors, and the trap in each

**Coverage is the user's lane; honesty is ours.** ctx-optimize does not chase
every format — grammar packs, manifest packs, and adapter scripts are how a
team adds what it needs (`./customize.md` is the how-to). This file is the
part a pack author MUST know before shipping one: each door has a measured
failure mode, and none of them announce themselves.

## Grammar packs — a new pack can DELETE working `calls` edges

A grammar pack's `names` mapping yields the **bare identifier**, and call
resolution keys on that bare name (`declRef{label: name}`, `code.go:599`,
keyed at `code.go:328`): a name that is not unique module-wide is **dropped**
rather than guessed. So adding a pack whose declaration names duplicate names
that already exist makes previously-resolved calls ambiguous — and they
silently disappear.

Measured on real Apache beam with a proto grammar pack: **126 correct `calls`
edges destroyed (1.10% of 11,501) plus 1 invented wrong edge**, with a 66.6%
repo-wide name-collision rate (83.3% in `sdks/go`). That is why proto does not
ship in core.

- **Count before and after.**
  `ctx-optimize edges --relation calls --ndjson | wc -l` before adding the
  pack, and again after `add .`. A DROP is the pack eating edges, not the
  graph getting cleaner.
- Qualified labels do **not** fix this. A pack cannot emit one (`packConfig`
  has only `name/exts/decls/names/calls/imports`, `langs.go:224-231`) and it
  would not help anyway — resolution keys on the bare name.
- Losing edges but wanting the decls? Keep the pack and say so when you
  answer: `calls` for the colliding names is INFERRED and now incomplete.

## Manifest packs — the selector is narrower than it looks

`{"file": "...", "format": "json|xml|yaml", "path": "...", "emit":
"dependency|task", "namespace": "..."}`. What the selector cannot do:

- **Root-anchored and exact-depth** (`matches()` requires
  `len(stack) == len(segs)`, `packs.go:405`). `target/@name` on a real Ant
  build yields **0**; `project/target/@name` yields **19**. Always write the
  path from the document root.
- **`*` matches exactly one level. There is no descendant operator** — an
  element that appears at two depths needs one rule per depth.
- **One match = one string.** Two attributes of the same element cannot be
  correlated (no `name`+`version` pairing for xml), and a comma-list
  attribute is taken raw — no splitting, no edge.
- **`emit` is only `{dependency, task}`.** A fact that is neither — a logback
  appender, a spring bean, an Ant `<property>` — has **no honest kind**; it
  would land as a `task` and lie. Use an adapter script for those.

## Adapter scripts — the universal door

Anything the two pack shapes cannot express (predicates, joins, sibling
lookups, a live system, rendered Helm values) goes through an adapter script
that prints one validated batch: `./adapters.md`. Databases/buckets/queues/
APIs come first through native sources (`./sources.md`) — a connector beats a
hand-written script.

## `deps` ecosystem coverage — name it, so silence isn't misread

Dependency extraction covers **npm · go · maven · gradle (→ `maven`
namespace) · nuget · pypi · crates**. Ruby (`Gemfile`) and PHP
(`composer.json`) are **NOT covered** — an adapter-door case; say so instead
of reporting no dependencies. pip-compile lock files are deliberately
**skipped**: they list transitive pins, not declared dependencies, and
`deps` must not present them as declarations.

`ctx-optimize deps` printing `(0 dependencies)` means "no declaration this
build recognizes", never "this repo declares none" — check the manifest's
ecosystem against the list above before you answer.
