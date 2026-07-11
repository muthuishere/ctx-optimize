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
}

var KnownGrammars = map[string]Known{
	"kotlin":  {URL: "https://github.com/fwcd/tree-sitter-kotlin", Exts: []string{".kt", ".kts"}},
	"swift":   {URL: "https://github.com/alex-pinkus/tree-sitter-swift", Ref: "with-generated-files", Exts: []string{".swift"}},
	"dart":    {URL: "https://github.com/UserNobody14/tree-sitter-dart", Exts: []string{".dart"}},
	"lua":     {URL: "https://github.com/tree-sitter-grammars/tree-sitter-lua", Exts: []string{".lua"}},
	"ruby":    {URL: "https://github.com/tree-sitter/tree-sitter-ruby", Exts: []string{".rb", ".rake", ".gemspec"}},
	"php":     {URL: "https://github.com/tree-sitter/tree-sitter-php", Exts: []string{".php"}},
	"scala":   {URL: "https://github.com/tree-sitter/tree-sitter-scala", Exts: []string{".scala", ".sc"}},
	"elixir":  {URL: "https://github.com/elixir-lang/tree-sitter-elixir", Exts: []string{".ex", ".exs"}},
	"haskell": {URL: "https://github.com/tree-sitter/tree-sitter-haskell", Exts: []string{".hs"}},
	"ocaml":   {URL: "https://github.com/tree-sitter/tree-sitter-ocaml", Exts: []string{".ml", ".mli"}},
	"bash":    {URL: "https://github.com/tree-sitter/tree-sitter-bash", Exts: []string{".sh", ".bash", ".zsh"}},
	"julia":   {URL: "https://github.com/tree-sitter/tree-sitter-julia", Exts: []string{".jl"}},
	"html":    {URL: "https://github.com/tree-sitter/tree-sitter-html", Exts: []string{".html", ".htm"}},
	"css":     {URL: "https://github.com/tree-sitter/tree-sitter-css", Exts: []string{".css"}},
	"yaml":    {URL: "https://github.com/tree-sitter-grammars/tree-sitter-yaml", Exts: []string{".yaml", ".yml"}},
	"toml":    {URL: "https://github.com/tree-sitter-grammars/tree-sitter-toml", Exts: []string{".toml"}},
}
