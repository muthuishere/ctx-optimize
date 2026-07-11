package grammar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGrammarFixture(t *testing.T, root string) string {
	t.Helper()
	src := filepath.Join(root, "src")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "parser.c"), []byte("/* stub */"), 0o644)
	os.WriteFile(filepath.Join(src, "grammar.json"), []byte(`{"name":"mylang"}`), 0o644)
	nodeTypes := `[
	  {"type":"function_declaration","named":true},
	  {"type":"class_declaration","named":true},
	  {"type":"local_variable_declaration","named":true},
	  {"type":"parameter_declaration","named":true},
	  {"type":"identifier","named":true},
	  {"type":"type_identifier","named":true},
	  {"type":"call_expression","named":true},
	  {"type":"import_statement","named":true},
	  {"type":"binary_expression","named":true},
	  {"type":"(","named":false}
	]`
	os.WriteFile(filepath.Join(src, "node-types.json"), []byte(nodeTypes), 0o644)
	return root
}

func TestSuggest(t *testing.T) {
	dir := writeGrammarFixture(t, t.TempDir())
	out, err := Suggest("mylang", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Review  string            `json:"_review"`
		Name    string            `json:"name"`
		Exts    []string          `json:"exts"`
		Decls   map[string]string `json:"decls"`
		Names   []string          `json:"names"`
		Calls   []string          `json:"calls"`
		Imports []string          `json:"imports"`
	}
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Review == "" || cfg.Name != "mylang" || cfg.Exts[0] != ".mylang" {
		t.Fatalf("header: %+v", cfg)
	}
	if cfg.Decls["function_declaration"] != "function" || cfg.Decls["class_declaration"] != "class" {
		t.Fatalf("decls: %v", cfg.Decls)
	}
	// variable/parameter noise must NOT become decls
	if _, ok := cfg.Decls["local_variable_declaration"]; ok {
		t.Fatal("variable noise leaked into decls")
	}
	if _, ok := cfg.Decls["parameter_declaration"]; ok {
		t.Fatal("parameter noise leaked into decls")
	}
	if len(cfg.Names) != 2 || cfg.Calls[0] != "call_expression" || cfg.Imports[0] != "import_statement" {
		t.Fatalf("lists: names=%v calls=%v imports=%v", cfg.Names, cfg.Calls, cfg.Imports)
	}
}

func TestFindParserDir(t *testing.T) {
	// at root
	root := writeGrammarFixture(t, t.TempDir())
	if got, err := findParserDir(root); err != nil || got != root {
		t.Fatalf("root: %v %v", got, err)
	}
	// one level down
	parent := t.TempDir()
	sub := filepath.Join(parent, "typescript")
	os.MkdirAll(sub, 0o755)
	writeGrammarFixture(t, sub)
	if got, err := findParserDir(parent); err != nil || got != sub {
		t.Fatalf("subdir: %v %v", got, err)
	}
	// nothing → helpful error
	if _, err := findParserDir(t.TempDir()); err == nil || !strings.Contains(err.Error(), "parser.c") {
		t.Fatalf("expected parser.c error, got %v", err)
	}
}

func TestGrammarName(t *testing.T) {
	dir := writeGrammarFixture(t, t.TempDir())
	name, err := grammarName(dir)
	if err != nil || name != "mylang" {
		t.Fatalf("%q %v", name, err)
	}
}

func TestZigTargetMapping(t *testing.T) {
	got := zigTarget()
	for _, part := range []string{"x86_64", "aarch64"} {
		if strings.HasPrefix(got, part) {
			return
		}
	}
	t.Fatalf("unexpected target %q", got)
}

// Full build: network + compiler. Opt-in, house style (runtime skip).
func TestBuildIntegration(t *testing.T) {
	if os.Getenv("CTX_OPTIMIZE_TEST_GRAMMAR_BUILD") == "" {
		t.Skip("set CTX_OPTIMIZE_TEST_GRAMMAR_BUILD=1 to run (downloads grammar + compiles)")
	}
	out := t.TempDir()
	wasmPath, cfgPath, err := Build(Options{
		Source: "https://github.com/tree-sitter-grammars/tree-sitter-lua",
		OutDir: out,
	}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(wasmPath); err != nil || fi.Size() < 100_000 {
		t.Fatalf("wasm too small or missing: %v", err)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatal(err)
	}
}
