# ADR — `decl_rules`: declarations matched by head symbol (the homoiconic family)

Status: **IMPLEMENTED** — 2026-07-25. Amends
`2026-07-25-core-language-coverage`, which recorded the opposite verdict.

## What this changes

That ADR lists, under **"FAILS — needs a guess, therefore OUT"**:

> homoiconic packs (Clojure/EDN/Elixir) | labels every decl after the macro;
> real name never appears

**That row rejects a different mechanism than the one shipped here.** It
describes `list_lit → function` in `decls`, taking the name from the *head* —
which does produce exactly the garbage described. `decl_rules` matches **on**
the head and reads the name from the **second element**. The row rejected the
thing that was tried, then blocked the thing that was not.

## The governing rule, applied

> *"Emit a fact only if it is read LITERALLY out of the file. If producing it
> needs a guess, skip it."*

`decl_rules` passes. `(defn fetch-user [] …)` — `fetch-user` is a literal
string at a deterministic position. No inference, the same shape as
`gradle task <name>` and `resource "type" "name"`, both of which that ADR
already PASSES.

### Measured, not asserted (658-file Clojure corpus)

| | |
|---|---|
| definer-headed forms | 1,372 |
| resolved to a literal name in second position | 1,362 (99.3%) |
| inside a quote / syntax-quote | 5 (0.36%) — excluded by `skip_inside` |
| **wrong facts emitted** | **0** |

Every failure mode lands on the under-claim side, which the rule calls "always
the correct failure mode":

- `s/def`, `:rename` aliasing → head text no longer matches exactly → nothing
  emitted.
- a project's own `(defsomething x)` → not in `head_match` → nothing emitted
  until added.
- `(defn ^:private f …)` → metadata in the name slot → form skipped, not
  named wrongly.
- `(defn …)` inside a syntax-quote → a macro CONSTRUCTING code. Excluded via
  `skip_inside`, which is a structural test against real grammar nodes
  (`quoting_lit` / `syn_quoting_lit`), not a heuristic.

## Design

`DeclRule` on `Lang` + `decl_rules` in the pack config. `NameStrategy` already
established that *where a declaration's name lives* varies by language
("declarator" for C/C++, "lastBeforeParams" for C#); this is the third case,
expressed as pack DATA rather than a hardcoded Go strategy.

Why data and not grammar: a Lisp program **creates defining macros at run
time**, so no parser can know them. `LoadPacks` already reads
`<repo>/.ctxoptimize/grammars/`, so a project ships its own definers in its own
repo — no release, no registry entry, no adapter. That is the
"enterprise adapts without adapters" requirement met for this family.

## Shipped

- `decl_rules` in the pack format: `node`, `head_type`, `head_match`,
  `name_type`, `name_unwrap`, `skip_inside`.
- Pack validation accepts `decls` **or** `decl_rules` (was: `decls` required,
  which hard-rejected every possible Clojure pack).
- `clojure` in `KnownGrammars` with a `DeclRules` seed, so
  `ctx-optimize languages add clojure` yields a **loadable** pack. Verified
  end-to-end on a real Clojure/cljgo repo: 942 nodes, 39 modules, queries
  resolve real names.
- **G4 fixed**: `grammar build` now FAILS when the suggested mapping yields no
  declarations, naming the homoiconic case and pointing at `decl_rules`. It
  previously printed "pack ready … next `ctx-optimize add` picks it up" for a
  pack `add` hard-rejects one command later.
- **Ext guessing fixed**: `Suggest` no longer seeds `exts` from the grammar
  name (`tree-sitter-clojure` → `".clojure"`, an extension nobody uses that
  silently matches nothing). Empty, with the reason in `_review`.
- `TestHomoiconicDeclRules` pins all of it, including the two failure modes
  that must NOT recur: a decl named after its macro, and a syntax-quoted form
  emitted as a definition.

## Not claimed

Measured on **Clojure only**. Fennel, Janet, Elisp and Racket are the same
shape and should work, but no corpus was run — do not advertise them until one
is. `elixir` stays out of `KnownGrammars`: its `call` node lacks the
`!namespace` distinction `sym_lit` provides, and the owner declined it
separately.
