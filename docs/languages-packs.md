# Languages, routes, manifests — extend extraction without recompiling

Three pack systems, one doctrine: **drop-in files, no toolchain, no fork.**
Grammar packs teach the code extractor new languages; route packs teach it
your framework's URL→handler wiring; manifest packs teach it your build
tool's dependency files.

## Languages

12 languages are embedded (tree-sitter compiled to WASM, zero setup):
go, python, javascript, typescript, tsx, java, c, cpp, csharp, rust, zig,
sql. Everything else is a **grammar pack** — `<name>.wasm` + `<name>.json`
in `~/ctxoptimize/grammars/` (machine) or `.ctxoptimize/grammars/`
(committed with the repo; kotlin/swift/dart packs ship there already).

### See what you have

```sh
$ ctx-optimize languages list
embedded: go, python, javascript, typescript, tsx, java, c, cpp, csharp, rust, zig, sql
packs:    (none)
addable by name (`ctx-optimize languages add <name>`): bash, css, dart, elixir, haskell, html, julia, kotlin, lua, ocaml, php, ruby, scala, swift, toml, yaml
anything else: `ctx-optimize languages add <github-url-of-tree-sitter-grammar>`
```

### Add a known language — one command

```sh
ctx-optimize languages add kotlin
ctx-optimize add .        # .kt files now emit real symbol nodes
```

Behind the scenes: the grammar is compiled to a WASM pack **in pure Go** —
zig is taken from PATH or auto-downloaded once (sha256-verified against
ziglang.org's index) into `~/ctxoptimize/toolchain/`. That download is the
only network, it happens at YOUR command, and never again.

### Add any language on earth — from its tree-sitter grammar

```sh
ctx-optimize languages add https://github.com/tree-sitter/tree-sitter-haskell
```

The node-type mapping (`<name>.json`) is auto-drafted from the grammar's
`node-types.json` and marked `_review` — open it, check the function/class
kinds look right, done. A malformed pack fails loudly at `add`-time, never
silently skips files.

### Share it with the team

```sh
mv ~/ctxoptimize/grammars/haskell.* .ctxoptimize/grammars/
git add .ctxoptimize/grammars && git commit -m "haskell grammar pack"
```

Repo packs override embedded grammars for the same extension — you can even
swap the bundled behavior of a language per-repo.

```sh
ctx-optimize languages remove haskell     # delete a pack
```

## Routes

Core recognizers cover FastAPI, Flask, Express, NestJS, Angular, React
Router, Vue, OpenAPI/Drupal/Ingress YAML — route nodes link to their
handlers, so `affected <handler>` surfaces the URL that binds it. Your
in-house router:

```sh
ctx-optimize routes add myrouter          # scaffolds .ctxoptimize/routes/myrouter.json
# edit the JSON: match patterns for registerRoute(...) etc., then
ctx-optimize add .
ctx-optimize routes list                  # core + discovered packs
```

`--global` targets `~/ctxoptimize/routes/` instead of the repo; a GitHub
repo or raw pack-json URL installs someone else's pack.

## Manifests

Core recognizers: package.json, pom.xml, csproj/sln, go.mod, gradle, k8s.
Dependencies land as `dep:` nodes that federate across build tools and
modules; k8s topology becomes graph. Custom build tool:

```sh
ctx-optimize manifests add mybuild        # scaffolds .ctxoptimize/manifests/mybuild.json
ctx-optimize manifests list
```

## When to reach for which

| You want | Use |
|---|---|
| a language extracted | grammar pack (`languages add`) |
| URLs linked to handlers | route pack (`routes add`) |
| deps/topology from a build file | manifest pack (`manifests add`) |
| anything else entirely | an [adapter](adapters.md) |
