# Grammar packs — optional languages, no recompile

Each pack is a pair: `<name>.wasm` + `<name>.json`. To enable one, copy the
pair to `~/ctxoptimize/grammars/` (machine-wide) or a repo's
`.ctxoptimize/grammars/` (travels with the repo). The next `ctx-optimize add`
picks it up. A pack's extensions override the embedded set.

Shipped here: kotlin (7.5MB), swift (5.7MB), dart (2.9MB) — kept out of the
embedded bundle for size.

Build your own from any tree-sitter grammar:

```sh
scripts/wasm/build-grammar.sh ~/src/tree-sitter-lua lua lua.wasm
```

then write `lua.json` mapping the grammar's node types (see kotlin.json;
`decls` maps AST node type → graph kind; `names`/`calls`/`imports` are node
type lists). The embedded languages: go, python, javascript, typescript,
tsx, java, c, cpp, c#, rust, zig, sql.
