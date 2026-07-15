package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// One fixture per wave-1 language; each must yield its declarations with
// qualified names, locations, contains edges — and the whole batch must pass
// the door's validation.
func TestExtractAllLanguages(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"main.go":   "package main\n\nimport \"fmt\"\n\ntype Store struct{}\n\nfunc (s *Store) Merge(v int) int {\n\treturn v\n}\n\nfunc Greet(name string) string {\n\treturn fmt.Sprintf(\"hi %s\", name)\n}\n\nfunc main() {\n\ts := &Store{}\n\ts.Merge(1)\n\tGreet(\"x\")\n}\n",
		"app.py":    "import os\n\nclass Billing:\n    def refund(self, amount):\n        self.audit(amount)\n        return validate(amount)\n    def audit(self, amount):\n        pass\n\ndef validate(amount):\n    return amount > 0\n",
		"ui.js":     "import React from 'react';\n\nclass Widget {\n  render() { return draw(); }\n}\n\nfunction draw() { return 1; }\n",
		"svc.ts":    "import {x} from './x';\n\nexport interface Refund { id: string }\n\nexport class RefundService {\n  process(r: Refund) { return audit(r); }\n}\n\nfunction audit(r: Refund) { return r.id; }\n",
		"App.tsx":   "import React from 'react';\n\nexport function App() {\n  return <div/>;\n}\n",
		"Main.java": "import java.util.List;\n\npublic class Main {\n    public void run() { helper(); }\n    static void helper() {}\n}\n",
		"lib.c":     "#include <stdio.h>\n\ntypedef int MyInt;\n\nstruct point { int x; };\n\nMyInt area(MyInt w, MyInt h) { return w * h; }\n\nint main(void) { return area(2, 3); }\n",
		"geo.cpp":   "#include <vector>\n\nnamespace geo {\nclass Shape {\npublic:\n  int area() { return compute(); }\n};\nint compute() { return 42; }\n}\n",
		"Svc.cs":    "using System;\n\nnamespace App {\n  public class Svc {\n    public Svc Run() { Helper(); return this; }\n    static void Helper() {}\n  }\n}\n",
		"lib.rs":    "use std::io;\n\npub struct Ledger { pub id: u64 }\n\npub trait Post { fn post(&self); }\n\npub fn settle(l: &Ledger) -> u64 { check(l.id) }\n\nfn check(v: u64) -> u64 { v }\n",
		"main.zig":  "const std = @import(\"std\");\n\nfn add(a: i32, b: i32) i32 {\n    return a + b;\n}\n\npub fn main() void {\n    _ = add(1, 2);\n}\n",
		"db.sql":    "CREATE TABLE refunds (id INT PRIMARY KEY, amount INT);\n\nCREATE VIEW big_refunds AS SELECT * FROM refunds WHERE amount > 100;\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	batch, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Validate(); err != nil {
		t.Fatalf("code batch failed the door: %v", err)
	}

	byID := map[string]schema.Node{}
	for _, n := range batch.Nodes {
		byID[n.ID] = n
	}
	wantNodes := map[string]string{ // id → kind
		"main.go::Greet":          "function",
		"main.go::Store":          "type",
		"main.go::Store.Merge":    "method",
		"app.py::Billing":         "class",
		"app.py::Billing.refund":  "function",
		"app.py::Billing.audit":   "function",
		"app.py::validate":        "function",
		"ui.js::Widget":           "class",
		"ui.js::Widget.render":    "method",
		"svc.ts::Refund":          "interface",
		"svc.ts::RefundService":   "class",
		"App.tsx::App":            "function",
		"Main.java::Main":         "class",
		"Main.java::Main.run":     "method",
		"lib.c::area":             "function",
		"lib.c::MyInt":            "type",
		"lib.c::point":            "struct",
		"geo.cpp::geo":            "module",
		"geo.cpp::geo.Shape":      "class",
		"Svc.cs::App":             "module",
		"Svc.cs::App.Svc":         "class",
		"Svc.cs::App.Svc.Run":     "method",
		"lib.rs::Ledger":          "struct",
		"lib.rs::Post":            "trait",
		"lib.rs::settle":          "function",
		"module://fmt":            "module",
		"module://react":          "module",
		"module://stdio.h":        "module",
		"module://java.util.List": "module",
		"main.zig::add":           "function",
		"db.sql::refunds":         "table",
		"db.sql::big_refunds":     "view",
	}
	for id, kind := range wantNodes {
		n, ok := byID[id]
		if !ok {
			t.Errorf("missing node %s", id)
			continue
		}
		if n.Kind != kind {
			t.Errorf("%s kind = %s, want %s", id, n.Kind, kind)
		}
	}
	// Locations are ranges.
	if n := byID["main.go::Store.Merge"]; !strings.HasPrefix(n.Location, "L7-") {
		t.Errorf("Store.Merge location = %q", n.Location)
	}

	// Calls resolve module-wide by unique name as INFERRED.
	edges := map[string]string{}
	for _, e := range batch.Edges {
		edges[e.Source+"→"+e.Target+"/"+e.Relation] = e.Confidence
	}
	wantCalls := []string{
		"main.go::main→main.go::Greet/calls",
		"main.go::main→main.go::Store.Merge/calls", // s.Merge: callee = Merge, not s
		"app.py::Billing.refund→app.py::validate/calls",
		"app.py::Billing.refund→app.py::Billing.audit/calls", // self.audit: callee = audit
		"ui.js::Widget.render→ui.js::draw/calls",
		"lib.rs::settle→lib.rs::check/calls",
	}
	for _, k := range wantCalls {
		if edges[k] != "INFERRED" {
			t.Errorf("missing INFERRED call edge %s (got %q)", k, edges[k])
		}
	}
	// Imports are EXTRACTED.
	if edges["main.go→module://fmt/imports"] != "EXTRACTED" {
		t.Errorf("missing import edge for fmt")
	}
}

// Dynamic grammar pack: drop <name>.wasm + <name>.json into
// .ctxoptimize/grammars/ and the language just works — no recompile.
// Uses the kotlin pack shipped in the repo's grammars/ dir.
func TestDynamicGrammarPack(t *testing.T) {
	pack := filepath.Join("..", "..", "..", "grammars", "kotlin.wasm")
	if _, err := os.Stat(pack); err != nil {
		t.Skip("kotlin pack not present")
	}
	root := t.TempDir()
	gdir := filepath.Join(root, ".ctxoptimize", "grammars")
	os.MkdirAll(gdir, 0o755)
	data, err := os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(gdir, "kotlin.wasm"), data, 0o644)
	cfg, err := os.ReadFile(filepath.Join("..", "..", "..", "grammars", "kotlin.json"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(gdir, "kotlin.json"), cfg, 0o644)
	t.Setenv("CTX_OPTIMIZE_GRAMMARS", filepath.Join(root, "nonexistent")) // isolate from ~

	kt := "import java.util.List\n\nclass Cart {\n    fun total(): Int { return tax(10) }\n}\n\nfun tax(v: Int): Int = v\n"
	os.WriteFile(filepath.Join(root, "App.kt"), []byte(kt), 0o644)

	batch, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Validate(); err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	for _, n := range batch.Nodes {
		byID[n.ID] = n.Kind
	}
	if byID["App.kt::Cart"] != "class" || byID["App.kt::tax"] != "function" {
		t.Fatalf("pack language not extracted: %v", byID)
	}
	found := false
	for _, e := range batch.Edges {
		if e.Source == "App.kt::Cart.total" && e.Target == "App.kt::tax" && e.Relation == "calls" {
			found = true
		}
	}
	if !found {
		t.Fatal("pack call edge missing")
	}
}

// A pack with a config but no wasm fails loudly, never silently skips.
func TestBrokenPackFailsLoudly(t *testing.T) {
	root := t.TempDir()
	gdir := filepath.Join(root, ".ctxoptimize", "grammars")
	os.MkdirAll(gdir, 0o755)
	os.WriteFile(filepath.Join(gdir, "lua.json"),
		[]byte(`{"name":"lua","exts":[".lua"],"decls":{"function_declaration":"function"}}`), 0o644)
	t.Setenv("CTX_OPTIMIZE_GRAMMARS", filepath.Join(root, "nonexistent"))
	if _, err := Extract(root); err == nil {
		t.Fatal("expected missing-wasm error")
	}
}

func TestLangForFile(t *testing.T) {
	if l := LangForFile("x.min.js"); l != nil {
		t.Fatal("minified js must be skipped")
	}
	if l := LangForFile("x.tsx"); l == nil || l.Name != "tsx" {
		t.Fatalf("tsx: %+v", l)
	}
	if l := LangForFile("README.md"); l != nil {
		t.Fatal("md is not code")
	}
}

// Symbol cards start here: every declaration carries its signature line and
// the comment block sitting directly above it, so query/card answers never
// force a file read.
func TestSignatureAndDoc(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"main.go": "package main\n\n// Greet says hi.\n// Politely.\nfunc Greet(name string) string {\n\treturn name\n}\n",
		"app.py":  "class Billing:\n    # refunds money\n    def refund(self, amount):\n        return amount\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	batch, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]schema.Node{}
	for _, n := range batch.Nodes {
		byID[n.ID] = n
	}
	g := byID["main.go::Greet"]
	if g.Metadata["signature"] != "func Greet(name string) string" {
		t.Errorf("go signature = %q", g.Metadata["signature"])
	}
	if g.Metadata["doc"] != "// Greet says hi.\n// Politely." {
		t.Errorf("go doc = %q", g.Metadata["doc"])
	}
	r := byID["app.py::Billing.refund"]
	if r.Metadata["signature"] != "def refund(self, amount):" {
		t.Errorf("py signature = %q", r.Metadata["signature"])
	}
	if r.Metadata["doc"] != "# refunds money" {
		t.Errorf("py doc = %q", r.Metadata["doc"])
	}
	// A blank line breaks the doc chain — the package clause's distance from
	// Billing means the class gets no doc.
	if d := byID["app.py::Billing"].Metadata["doc"]; d != "" {
		t.Errorf("class doc should be empty, got %q", d)
	}
}

func TestIsMinified(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"normal wrapped code", "package x\n\nfunc a() {}\nfunc b() {}\n", false},
		{"long but wrapped file", strings.Repeat("const x = someReasonableCall(a, b, c)\n", 5000), false},
		{"minified single line", "!function(){" + strings.Repeat("var a=1;", 20000) + "}();", true},
		{"empty", "", false},
	}
	for _, c := range cases {
		if got := isMinified([]byte(c.src)); got != c.want {
			t.Errorf("%s: isMinified = %v, want %v", c.name, got, c.want)
		}
	}
}

// A minified .js file in a gathered tree contributes NO nodes — it is dropped
// by shape, so bundlers' output never pollutes hubs/query.
func TestExtractSkipsMinifiedFile(t *testing.T) {
	dir := t.TempDir()
	// real hand-written module
	if err := os.WriteFile(filepath.Join(dir, "real.js"),
		[]byte("export function greet(name) {\n  return 'hi ' + name\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// minified bundle: one enormous line, under maxFileBytes
	if err := os.WriteFile(filepath.Join(dir, "bundle.min.js"),
		[]byte("!function(){"+strings.Repeat("var a=1;", 20000)+"}();"), 0o644); err != nil {
		t.Fatal(err)
	}
	batch, err := ExtractPaths(dir, []string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range batch.Nodes {
		if strings.Contains(n.Source, "bundle.min.js") {
			t.Fatalf("minified file leaked a node: %s (%s)", n.Label, n.Source)
		}
	}
	// the real file must still be indexed
	var sawGreet bool
	for _, n := range batch.Nodes {
		if n.Label == "greet" {
			sawGreet = true
		}
	}
	if !sawGreet {
		t.Fatal("real file's greet() was not indexed")
	}
}
