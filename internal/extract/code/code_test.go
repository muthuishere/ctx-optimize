package code

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// One fixture per wave-1 language; each must yield its declarations with
// qualified names, locations, contains edges — and the whole batch must pass
// the door's validation.
func TestExtractAllLanguages(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"main.go":   "package main\n\nimport \"fmt\"\n\nfunc Greet(name string) string {\n\treturn fmt.Sprintf(\"hi %s\", name)\n}\n\nfunc main() {\n\tGreet(\"x\")\n}\n",
		"app.py":    "import os\n\nclass Billing:\n    def refund(self, amount):\n        return validate(amount)\n\ndef validate(amount):\n    return amount > 0\n",
		"ui.js":     "import React from 'react';\n\nclass Widget {\n  render() { return draw(); }\n}\n\nfunction draw() { return 1; }\n",
		"svc.ts":    "import {x} from './x';\n\nexport interface Refund { id: string }\n\nexport class RefundService {\n  process(r: Refund) { return audit(r); }\n}\n\nfunction audit(r: Refund) { return r.id; }\n",
		"App.tsx":   "import React from 'react';\n\nexport function App() {\n  return <div/>;\n}\n",
		"Main.java": "import java.util.List;\n\npublic class Main {\n    public void run() { helper(); }\n    static void helper() {}\n}\n",
		"lib.c":     "#include <stdio.h>\n\nstruct point { int x; };\n\nint area(int w, int h) { return w * h; }\n\nint main(void) { return area(2, 3); }\n",
		"geo.cpp":   "#include <vector>\n\nnamespace geo {\nclass Shape {\npublic:\n  int area() { return compute(); }\n};\nint compute() { return 42; }\n}\n",
		"Svc.cs":    "using System;\n\nnamespace App {\n  public class Svc {\n    public void Run() { Helper(); }\n    static void Helper() {}\n  }\n}\n",
		"lib.rs":    "use std::io;\n\npub struct Ledger { pub id: u64 }\n\npub trait Post { fn post(&self); }\n\npub fn settle(l: &Ledger) -> u64 { check(l.id) }\n\nfn check(v: u64) -> u64 { v }\n",
		"App.kt":    "import java.util.List\n\nclass Cart {\n    fun total(): Int { return tax(10) }\n}\n\nfun tax(v: Int): Int = v\n",
		"main.dart": "import 'dart:io';\n\nclass Order {\n  int total() { return 1; }\n}\n\nint price(int v) { return v; }\n",
		"main.zig":  "const std = @import(\"std\");\n\nfn add(a: i32, b: i32) i32 {\n    return a + b;\n}\n\npub fn main() void {\n    _ = add(1, 2);\n}\n",
		"App.swift": "import Foundation\n\nprotocol Payable { func pay() }\n\nclass Invoice {\n    func total() -> Int { return round2(3) }\n}\n\nfunc round2(_ v: Int) -> Int { return v }\n",
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
		"app.py::Billing":         "class",
		"app.py::Billing.refund":  "function",
		"app.py::validate":        "function",
		"ui.js::Widget":           "class",
		"ui.js::Widget.render":    "method",
		"svc.ts::Refund":          "interface",
		"svc.ts::RefundService":   "class",
		"App.tsx::App":            "function",
		"Main.java::Main":         "class",
		"Main.java::Main.run":     "method",
		"lib.c::area":             "function",
		"lib.c::point":            "struct",
		"geo.cpp::geo":            "module",
		"geo.cpp::geo.Shape":      "class",
		"Svc.cs::App":             "module",
		"Svc.cs::App.Svc":         "class",
		"lib.rs::Ledger":          "struct",
		"lib.rs::Post":            "trait",
		"lib.rs::settle":          "function",
		"module://fmt":            "module",
		"module://react":          "module",
		"module://stdio.h":        "module",
		"module://java.util.List": "module",
		"App.kt::Cart":            "class",
		"App.kt::Cart.total":      "function",
		"App.kt::tax":             "function",
		"main.dart::Order":        "class",
		"main.dart::price":        "function",
		"main.zig::add":           "function",
		"App.swift::Payable":      "interface",
		"App.swift::Invoice":      "class",
		"App.swift::round2":       "function",
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
	if n := byID["main.go::Greet"]; n.Location != "L5-L7" {
		t.Errorf("Greet location = %q", n.Location)
	}

	// Calls resolve module-wide by unique name as INFERRED.
	edges := map[string]string{}
	for _, e := range batch.Edges {
		edges[e.Source+"→"+e.Target+"/"+e.Relation] = e.Confidence
	}
	wantCalls := []string{
		"main.go::main→main.go::Greet/calls",
		"app.py::Billing.refund→app.py::validate/calls",
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
