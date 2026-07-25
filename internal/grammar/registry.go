// registry.go — curated well-known grammars so `languages add kotlin` works
// by name alone: the repo, the ref that actually carries a generated
// parser.c, and the real file extensions to seed the mapping draft. Anything
// not listed still works via an explicit GitHub URL or local dir.
package grammar

// Known maps language name → where its tree-sitter grammar lives.
type Known struct {
	URL  string
	Ref  string // branch/tag whose tarball contains src/parser.c ("" = HEAD)
	Exts []string
	// DeclRules seeds a HOMOICONIC grammar's mapping. node-types.json cannot
	// yield decls for these — a Clojure definition is an ordinary list whose
	// head symbol carries the meaning — so without a seed the suggested pack
	// comes out empty and cannot load. Raw JSON, spliced into the draft.
	DeclRules string
}

// clojureDeclRules: STOCK clojure.core only — this entry serves every Clojure
// project, not any one dialect. Framework and project macros (Compojure's
// defroutes, re-frame's reg-event-db, cljgo/bri's defcommand, your own
// defjob) are added by the PROJECT in its repo-local
// .ctxoptimize/grammars/*.json, which LoadPacks reads first.
//
// The clojure.core defining macros, matched by head symbol
// with the name read from the next element. Two rules because `(in-ns 'x)`
// wraps its name in a quote. Everything here is a literal read; a project's
// own defining macros are added by editing head_match in the written pack.
const clojureDeclRules = `[
    {
      "node": "list_lit",
      "head_type": "sym_lit",
      "name_type": "sym_lit",
      "skip_inside": ["quoting_lit", "syn_quoting_lit", "dis_expr"],
      "head_match": {
        "ns": "module", "def": "variable", "defonce": "variable",
        "defn": "function", "defn-": "function", "defmacro": "macro",
        "definline": "function", "defmulti": "function", "defmethod": "function",
        "defprotocol": "interface", "defrecord": "class", "deftype": "class",
        "defstruct": "class", "deftest": "test"
      }
    },
    {
      "node": "list_lit",
      "head_type": "sym_lit",
      "name_type": "sym_lit",
      "name_unwrap": ["quoting_lit", "syn_quoting_lit"],
      "skip_inside": ["dis_expr"],
      "head_match": { "in-ns": "module", "clojure.core/in-ns": "module" }
    }
  ]`

// cljgoDeclRules: clojure.core PLUS the four definers cljgo itself adds
// (defroute/defroutes from its router, defcommand/defcommands from bri's CLI
// layer). Unlike a framework's macros, these ARE the language — cljgo ships
// them — so they belong in a named entry rather than in each project's pack.
//
// Kept as its own list rather than composed from clojureDeclRules at run time:
// a registry entry is DATA, and the whole value of a literal table is that you
// can read what it will emit without executing anything.
//
// Source of truth is tree-sitter-cljgo's definers.json, which generates that
// repo's own .ctxoptimize/grammars/cljgo.json plus its editor integrations. This
// is a copy, so it can drift; the repo's pack wins where both are present,
// because LoadPacks reads repo-local first.
const cljgoDeclRules = `[
    {
      "node": "list_lit",
      "head_type": "sym_lit",
      "name_type": "sym_lit",
      "skip_inside": ["quoting_lit", "syn_quoting_lit", "dis_expr"],
      "head_match": {
        "ns": "module", "def": "variable", "defonce": "variable",
        "defn": "function", "defn-": "function", "defmacro": "macro",
        "definline": "function", "defmulti": "function", "defmethod": "function",
        "defprotocol": "interface", "defrecord": "class", "deftype": "class",
        "defstruct": "class", "deftest": "test",
        "defroute": "variable", "defroutes": "variable",
        "defcommand": "function", "defcommands": "variable"
      }
    },
    {
      "node": "list_lit",
      "head_type": "sym_lit",
      "name_type": "sym_lit",
      "name_unwrap": ["quoting_lit", "syn_quoting_lit"],
      "skip_inside": ["dis_expr"],
      "head_match": { "in-ns": "module", "clojure.core/in-ns": "module" }
    }
  ]`

var KnownGrammars = map[string]Known{
	"clojure": {URL: "https://github.com/sogaiu/tree-sitter-clojure",
		Exts:      []string{".clj", ".cljc", ".cljs", ".cljr", ".edn", ".bb"},
		DeclRules: clojureDeclRules},
	// cljgo claims ONLY its own extensions. tree-sitter-cljgo's own pack also
	// declares .clj/.cljc — correct for a project written in cljgo, wrong for a
	// shared registry, where it would take every plain Clojure file away from
	// the `clojure` entry (packByExt beats the embedded set AND is order-
	// dependent between packs). A project that wants cljgo semantics for .clj
	// copies that repo's pack into its own .ctxoptimize/grammars/, which wins.
	"cljgo": {URL: "https://github.com/muthuishere/tree-sitter-cljgo",
		Exts:      []string{".cljgo", ".cljg"},
		DeclRules: cljgoDeclRules},
	"kotlin":  {URL: "https://github.com/fwcd/tree-sitter-kotlin", Exts: []string{".kt", ".kts"}},
	"swift":   {URL: "https://github.com/alex-pinkus/tree-sitter-swift", Ref: "with-generated-files", Exts: []string{".swift"}},
	"dart":    {URL: "https://github.com/UserNobody14/tree-sitter-dart", Exts: []string{".dart"}},
	"lua":     {URL: "https://github.com/tree-sitter-grammars/tree-sitter-lua", Exts: []string{".lua"}},
	"ruby":    {URL: "https://github.com/tree-sitter/tree-sitter-ruby", Exts: []string{".rb", ".rake", ".gemspec"}},
	"php":     {URL: "https://github.com/tree-sitter/tree-sitter-php", Exts: []string{".php"}},
	"scala":   {URL: "https://github.com/tree-sitter/tree-sitter-scala", Exts: []string{".scala", ".sc"}},
	"haskell": {URL: "https://github.com/tree-sitter/tree-sitter-haskell", Exts: []string{".hs"}},
	"ocaml":   {URL: "https://github.com/tree-sitter/tree-sitter-ocaml", Exts: []string{".ml", ".mli"}},
	"bash":    {URL: "https://github.com/tree-sitter/tree-sitter-bash", Exts: []string{".sh", ".bash", ".zsh"}},
	"julia":   {URL: "https://github.com/tree-sitter/tree-sitter-julia", Exts: []string{".jl"}},
	"html":    {URL: "https://github.com/tree-sitter/tree-sitter-html", Exts: []string{".html", ".htm"}},
	"css":     {URL: "https://github.com/tree-sitter/tree-sitter-css", Exts: []string{".css"}},
	"yaml":    {URL: "https://github.com/tree-sitter-grammars/tree-sitter-yaml", Exts: []string{".yaml", ".yml"}},
	"toml":    {URL: "https://github.com/tree-sitter-grammars/tree-sitter-toml", Exts: []string{".toml"}},
}
