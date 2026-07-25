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
addable by name (`ctx-optimize languages add <name>`): bash, cljgo, clojure, css, dart, haskell, html, julia, kotlin, lua, ocaml, php, ruby, scala, swift, toml, yaml
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

## Homoiconic languages — `decl_rules`

Lisps break the assumption every other pack relies on: **a declaration has no
node type of its own.** `(defn fetch-user [] …)` parses as an ordinary
`list_lit` whose *head symbol* (`defn`) carries the meaning and whose *second
element* (`fetch-user`) is the name.

Mapping `list_lit → function` in `decls` therefore produces garbage — every
node is named after the macro:

```
function  defn          ← should be fetch-user
function  defn          ← should be helper
variable  def           ← should be config
                        ← `handler` never appears at all
```

`decl_rules` matches on the head symbol instead and reads the name from the
next element:

```json
{
  "name": "clojure",
  "exts": [".clj", ".cljc", ".cljs"],
  "decl_rules": [
    {
      "node": "list_lit",
      "head_type": "sym_lit",
      "name_type": "sym_lit",
      "skip_inside": ["quoting_lit", "syn_quoting_lit", "dis_expr"],
      "head_match": { "defn": "function", "def": "variable", "ns": "module" }
    }
  ]
}
```

| field | meaning |
|---|---|
| `node` | container node type to test |
| `head_type` | required type of the **first** named child |
| `head_match` | that child's literal text → the kind emitted |
| `name_type` | required type of the **second** named child (the name) |
| `name_unwrap` | wrapper types to step over first — `(in-ns 'app.core)` quotes its name |
| `skip_inside` | ancestor types that void a match — a `defn` inside a syntax-quote is a macro *constructing* code, not a definition |

A pack may use `decls`, `decl_rules`, or both.

### It is literal, and it under-claims

Everything emitted is read verbatim from the file. The matcher deliberately
**misses** rather than guesses:

- `s/def`, `(:refer-clojure :rename {defn my-defn})` — head text no longer
  matches exactly, so nothing is emitted.
- `(defsomething x)` from a project's own macro — not in `head_match`, so
  nothing is emitted **until you add it** (see below).
- `(defn ^:private f …)` — metadata in the name slot, so the form is skipped
  rather than named wrongly.

Measured on a 658-file Clojure codebase: 1,372 definer-headed forms, 1,362
resolved to a literal name, **0 wrong**. The 5 forms inside syntax-quotes were
excluded by `skip_inside`.

### The `clojure` entry is dialect-neutral; a dialect gets its own entry

`ctx-optimize languages add clojure` ships **stock `clojure.core` only** —
`.clj`, `.cljc`, `.cljs`, `.cljr`, `.edn`, `.bb`. It is not tuned for any one
project. FRAMEWORK macros — Compojure's `defroutes`, re-frame's `reg-event-db`,
your own `defjob` — belong to the project, so the project carries them.

A **language** is different. cljgo ships `defroute`/`defroutes` and
`defcommand`/`defcommands` as part of itself, so it is a named entry of its own:

```sh
ctx-optimize languages add cljgo    # clojure.core's 14 definers + cljgo's 4
```

It claims **only `.cljgo` and `.cljg`**. `tree-sitter-cljgo`'s own pack also
declares `.clj`/`.cljc` — correct for a project written in cljgo, wrong for a
shared registry, where it would take every plain Clojure file away from the
`clojure` entry (pack extensions beat the embedded set, and between two packs the
winner is order-dependent). A project that wants cljgo semantics for `.clj` copies
that repo's `.ctxoptimize/grammars/cljgo.json` into its own, which is read first
and wins.

The registry copy can drift from `tree-sitter-cljgo`'s `definers.json`, which is
its source of truth. Where both are present the repo-local pack wins, so drift
resolves toward the author.

### Add your project's own defining macros

Edit `head_match` in the pack — a repo-local `.ctxoptimize/grammars/*.json` is
read before the machine one, so this ships **with your repo**, no release
needed:

```json
"head_match": { "defn": "function", "defroute": "variable", "defjob": "function" }
```

This is why the mechanism is DATA and not grammar: a Lisp program creates
defining macros at run time, so no parser can know them, but a table can.
