# ADR — `languages add <url>`: auto-identify the pack, never guess it

Status: **DRAFT** — awaiting owner sign-off. No product code written.
Follows `2026-07-25-homoiconic-decl-rules`, which shipped `decl_rules` but left
the URL path unable to produce one.

## The problem

`ctx-optimize languages add clojure` yields a loadable pack. The same command
given a **URL** does not:

```
$ ctx-optimize languages add https://github.com/muthuishere/tree-sitter-cljgo
downloading muthuishere/tree-sitter-cljgo…
compiling cljgo (zig cc → wasm32-wasi)…
suggested mapping written: …/cljgo.json — REVIEW IT
ctx-optimize: grammar cljgo built, but no declaration node types could be
inferred, so …/cljgo.json cannot be loaded as written.
exit 1
```

The wasm is correct (1.8MB, compiles clean). Two fields of the mapping are not:

| field | why it is empty |
|---|---|
| `decl_rules` | the seed lives in `KnownGrammars`, keyed on the short NAME (`build.go:55`, `KnownGrammars[opts.Source]`). A URL matches no key. |
| `exts` | `Suggest` no longer guesses extensions from the repo name — deliberately, since `tree-sitter-clojure` produced `.clojure`, matching nothing while looking configured. Nothing replaced it. |

The obvious rescue — seed `clojure.core`'s definers whenever a grammar looks
Lisp-shaped — is a **guess about someone else's dialect**, and a wrong
`head_match` is a confidently-wrong graph: the one outcome `decl_rules` exists to
prevent.

## Decision

Auto-identify from **declarations the grammar repo already makes about itself**.
A ladder of literal reads, highest precedence first. Nothing on it infers
semantics.

| rung | source | answers | measured coverage |
|---|---|---|---|
| **L1** | `<repo>/ctxoptimize/<name>.json` | the whole pack, verbatim | **1 / 38 repos** |
| **L2a** | `tree-sitter.json` → `grammars[].file-types` | `exts` | **32 / 38** |
| **L2b** | `package.json` → `tree-sitter[].file-types` | `exts` | **8 / 38** |
| **L3** | `KnownGrammars[name]` (exists today) | `decl_rules` seed | n/a |
| **L4** | probe: parse the repo's own sources | observed head symbols | **UNMEASURED** |
| **L5** | today's behaviour | loud failure, exit 1 | — |

## Spike results (2026-07-25)

Surveyed **38 public tree-sitter grammar repos** — the twelve embedded languages,
the three shipped packs (kotlin/swift/dart), the build/config family
(make, cmake, toml, xml, powershell), and the whole homoiconic family
(clojure, cljgo, fennel, janet, elisp, racket, scheme, commonlisp).

### S-A: exts are declared — but you must read BOTH files

| | repos |
|---|---|
| declare `file-types` **somewhere** | **35 / 38 (92%)** |
| `tree-sitter.json` only | 27 |
| both files | 5 |
| `package.json` only | 3 |
| declare nowhere | 3 — `tree-sitter-cmake`, `tree-sitter-powershell` (no manifest at all), `tree-sitter-regex` |

My first pass read only `package.json` and scored **8/38 (21%)** — I had measured
the wrong file. The modern tree-sitter CLI moved the declaration to
`tree-sitter.json` → `grammars[].file-types`. Reading only the modern file is the
symmetric mistake, and it fails in the worst possible place:

| repo | `tree-sitter.json` | `package.json` |
|---|---|---|
| sogaiu/tree-sitter-clojure | **absent** | `bb, clj, cljc, cljs` |
| muthuishere/tree-sitter-cljgo | **absent** | `cljgo, cljg, clj, cljc` |
| travonted/tree-sitter-fennel | **absent** | `fnl` |

**The Lisp family — the only family that needs `decl_rules` at all — declares in
the OLD file.** Read `tree-sitter.json` first, fall back to `package.json`. Across
all eight homoiconic repos, coverage of the union is **8/8**.

### S-B: L1 is a convention with one implementer, and it is ours

Probed all 38 repos for `ctxoptimize/<name>.json` and
`.ctxoptimize/grammars/<name>.json`:

```
FOUND muthuishere/tree-sitter-cljgo  ctxoptimize/cljgo.json@master
1 of 38 repos ship one
```

So L1 is **not a discovery mechanism for the ecosystem** — it is a door we would
be *proposing*, currently walked through by exactly one repo, the owner's. That
does not make it wrong: it is the only rung that can ever be complete, because
the author of a run-time-defined macro is the only party who can enumerate it.
But it must not be sold as "auto-identify works on public grammars."

Where it does apply, it works. `tree-sitter-cljgo` generates
`ctxoptimize/cljgo.json` from a `definers.json` the repo calls its
"SINGLE SOURCE OF TRUTH for cljgo's defining forms", alongside its editor
integrations. Paired with the wasm `languages add` had already built, it gathered
`tree-sitter-cljgo/examples`:

| | |
|---|---|
| files | 7 |
| declarations | 20 function, 22 variable, 7 module |
| nodes named after a defining macro | **0** |
| call sites emitted as declarations | **0** |

`defcommand` is covered — a definer no registry could know, because cljgo creates
it at run time.

No new trust boundary: `LoadPacks` already reads repo-local
`.ctxoptimize/grammars/` first, so third-party pack JSON already shapes graphs.
L1 reads the same shape from the grammar repo and validates it through the same
gate. We execute nothing.

### S-C: L2 would silently hijack embedded languages — the blocking hazard

`internal/extract/code/code.go:124-138` builds `packByExt` and consults it
**before** `LangForFile`. A pack extension beats the embedded set by design
("users can override built-ins"). So seeding `exts` from a declaration is not
inert:

```
tree-sitter/tree-sitter-python      collides: .py
tree-sitter/tree-sitter-go          collides: .go
tree-sitter/tree-sitter-javascript  collides: .cjs .js .jsx .mjs
tree-sitter/tree-sitter-typescript  collides: .js .ts .tsx
tree-sitter/tree-sitter-c           collides: .c .h
tree-sitter/tree-sitter-cpp         collides: .cc .cpp .h .hpp
tree-sitter/tree-sitter-java .java · -rust .rs · -c-sharp .cs · -zig .zig
```

**10 of 38 declare at least one extension already owned by an embedded
language**, and `tree-sitter-typescript` declares `.js` — so
`languages add https://…/tree-sitter-typescript` would take `.js` away from the
tuned embedded javascript mapping and hand it to a `_review` draft. Every Go, Python
and JS file in the repo would then be extracted by a guessed mapping, silently.

That is a worse outcome than the empty `exts` we have today. **Mitigation is
mandatory, not optional**: drop any declared extension already owned by an
embedded language, name each drop on stderr, and fail rather than write a pack
whose exts are entirely consumed that way.

### S-D: L4 — NOT MEASURED

`Instance.Parse` and `NewEngineFromBytes` are exported (`wasm.go:192`,
`wasm.go:93`), so probing is *mechanically* available: parse real files, count
head symbols sitting in front of a plain-symbol name, rank, write the observed
list into the `_review` draft with counts. I did not build it and I have no
numbers for it. Two things are known without running it:

**The fixture trap is real** — confirmed by reading the format, not by probing.
`sogaiu/tree-sitter-clojure/test/corpus/*.txt` holds the expected **AST** as
s-expressions:

```
#()
--------------------------------------------------------------------------------
(source
  (anon_fn_lit))
```

Parsed as Lisp, `source` and `anon_fn_lit` become high-frequency "definers" — a
confidently-wrong seed produced by the mechanism meant to prevent one. Any probe
must be **extension-scoped** (L2 exts only: `examples/*.cljg` in, `.txt`
fixtures out), never a whole-repo sweep.

**L4 cannot name kinds.** Whether `defcommand` is a `function` or a `macro` is
not in the text. Heads matching a known table would take that table's kind;
heads that do not would go under `_review_unmapped` and NOT into `head_match`.
Discovering a definer and admitting we cannot classify it is the under-claim.

## Why it is honest

| case | behaviour |
|---|---|
| repo ships a pack | used verbatim — the author's own truth |
| repo declares `file-types` in either file | exts read literally |
| declared ext owned by an embedded language | dropped and named, never silently overridden |
| dialect definer (`defcommand`) in no table | L1 covers it; L4 might; otherwise absent |
| L4 finds a head it cannot classify | reported, NOT written into `head_match` |
| repo declares nothing (cmake, powershell, regex) | L5: exit 1, same message as today |

The ladder can leave a pack incomplete. It cannot make one wrong.

## Open questions for the owner

1. **L1 verbatim, or reviewed?** Verbatim makes the cljgo URL work in one
   command, and it is the honest reading of an author's declaration. It also
   means a graph shaped by a file we never showed you. Print a summary
   (`name, exts, N rules, M heads`) and use it, or write it as `_review` and
   require a confirming `add`?
2. **Is L1 worth building for n=1?** It is your own repo, and the convention has
   no other adopters. Fair answers include "yes — define the door, it is 40
   lines" and "no — a documented copy-two-files recipe is enough."
3. **Is L4 worth building at all?** It is the only rung that finds an unlisted
   project definer, it is the most code (~100 lines + test), and it is the only
   rung with no measurements behind it. Deferrable without blocking L1/L2.
4. **Clobbering.** `languages add` writes into `~/ctxoptimize/grammars/`. A
   hand-edited pack already there must not be silently overwritten.
5. **Does blessing `ctxoptimize/` make it a convention** grammar authors are
   expected to follow? If yes it belongs in `docs/languages-packs.md` as a
   documented contract, not an undocumented path we happen to probe.

## Not claimed

- 38 repos is a hand-picked sample, not a random draw from the ecosystem — chosen
  to cover our own languages plus the whole homoiconic family. Percentages
  describe that sample.
- L4 has **no measurements at all**. Its cost estimate is a guess.
- The L1 end-to-end run used one pack (cljgo) over 7 files. It proves the
  mechanism works, not that author packs are generally good.
- Only Clojure has been measured for `decl_rules` at all. Fennel, Janet, Elisp
  and Racket are the same shape and stay unadvertised until a corpus is run.
