# Grammar packs — optional languages, no recompile

Each pack is a pair: `<name>.wasm` + `<name>.json`. To enable one, copy the
pair to `~/ctxoptimize/grammars/` (machine-wide) or a repo's
`.ctxoptimize/grammars/` (travels with the repo). The next `ctx-optimize add`
picks it up. A pack's extensions override the embedded set.

Shipped here: kotlin (7.5MB), swift (5.7MB), dart (2.9MB), clojure (1.8MB) —
kept out of the embedded bundle for size.

Build your own from any tree-sitter grammar:

```sh
scripts/wasm/build-grammar.sh ~/src/tree-sitter-lua lua lua.wasm
```

then write `lua.json` mapping the grammar's node types (see kotlin.json;
`decls` maps AST node type → graph kind; `names`/`calls`/`imports` are node
type lists). The embedded languages: go, python, javascript, typescript,
tsx, java, c, cpp, c#, rust, zig, sql.

## Languages with no declaration node type

Lisps don't fit `decls`. In Clojure, `(defn fetch-user [] …)` is an ordinary
`list_lit` — the *head symbol* carries the meaning and the *second element* is
the name — so mapping `list_lit → function` yields nodes all called `defn`
while the real names never appear.

Those packs use **`decl_rules`** instead, which matches on the head symbol
(see `clojure.json`). Add your project's own defining macros to `head_match`
in a repo-local `.ctxoptimize/grammars/*.json`; it is read before the
machine-wide one, so the table ships with your repo. Full field reference:
`docs/languages-packs.md`.
