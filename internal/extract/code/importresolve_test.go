package code

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func edgeSet(b *schema.Batch) map[string]string {
	m := map[string]string{}
	for _, e := range b.Edges {
		m[e.Source+" -"+e.Relation+"-> "+e.Target] = e.Confidence
	}
	return m
}

func nodeIDs(b *schema.Batch) map[string]bool {
	m := map[string]bool{}
	for _, n := range b.Nodes {
		m[n.ID] = true
	}
	return m
}

func TestRelativeImportRewritesToFileNode(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		// The SAME specifier "./util" from two folders must land on two
		// DIFFERENT files — the shared module://./util node cannot say that.
		"a/main.ts":  `import { x } from "./util"` + "\n",
		"a/util.ts":  `export const x = 1` + "\n",
		"b/other.ts": `import { y } from "./util"` + "\n",
		"b/util.ts":  `export const y = 2` + "\n",
		"a/ext.ts":   `import fs from "fs"` + "\n",
	})
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	edges := edgeSet(b)
	if edges["a/main.ts -imports-> a/util.ts"] != schema.Extracted {
		t.Fatalf("relative import not rewritten to file node: %v", edges)
	}
	if edges["b/other.ts -imports-> b/util.ts"] != schema.Extracted {
		t.Fatalf("sibling folder resolved wrong: %v", edges)
	}
	nodes := nodeIDs(b)
	if nodes["module://./util"] {
		t.Fatal("orphaned module://./util placeholder not pruned")
	}
	// External specifiers stay exactly as before — never fabricated.
	if edges["a/ext.ts -imports-> module://fs"] != schema.Extracted {
		t.Fatal("external import edge lost")
	}
	if !nodes["module://fs"] {
		t.Fatal("external module node pruned")
	}
}

func TestTSConfigAliasResolves(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"tsconfig.json": `{
  // JSONC comments must not break parsing
  "compilerOptions": { "baseUrl": ".", "paths": { "@/*": ["src/*"] } }
}`,
		"src/lib/http.ts": `export function apiRequest() {}` + "\n",
		"src/page.ts":     `import { apiRequest } from "@/lib/http"` + "\n",
	})
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	edges := edgeSet(b)
	if edges["src/page.ts -imports-> src/lib/http.ts"] != schema.Extracted {
		t.Fatalf("alias import not rewritten: %v", edges)
	}
	if nodeIDs(b)["module://@/lib/http"] {
		t.Fatal("resolved alias placeholder not pruned")
	}
}

func TestGoModuleImportGainsResolvesTo(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.22\n",
		"main.go": `package main

import (
	"fmt"
	"example.com/app/internal/pay"
)

func main() { fmt.Println(pay.Charge()) }
`,
		"internal/pay/pay.go":  "package pay\n\nfunc Charge() int { return 1 }\n",
		"internal/pay/util.go": "package pay\n\nfunc helper() {}\n",
	})
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	edges := edgeSet(b)
	// The package import keeps its module node (globally unique ID)…
	if edges["main.go -imports-> module://example.com/app/internal/pay"] != schema.Extracted {
		t.Fatalf("go import edge lost: %v", edges)
	}
	// …and every file of the package is joined, deplink-style.
	if edges["module://example.com/app/internal/pay -resolves_to-> internal/pay/pay.go"] != schema.Extracted ||
		edges["module://example.com/app/internal/pay -resolves_to-> internal/pay/util.go"] != schema.Extracted {
		t.Fatalf("go package resolves_to missing: %v", edges)
	}
	// Stdlib stays a dead-end honestly.
	if edges["main.go -imports-> module://fmt"] != schema.Extracted {
		t.Fatal("stdlib import edge lost")
	}
	for k := range edges {
		if k == "module://fmt -resolves_to-> main.go" {
			t.Fatal("stdlib must not resolve into the repo")
		}
	}
}

func TestPythonDottedAndRelativeImports(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"pkg/__init__.py": "",
		"pkg/svc.py":      "import pkg.db\n",
		"pkg/db.py":       "x = 1\n",
	})
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	edges := edgeSet(b)
	if edges["pkg/svc.py -imports-> pkg/db.py"] != schema.Extracted {
		t.Fatalf("dotted python import not rewritten: %v", edges)
	}
}

func TestUnresolvableSpecifiersUntouched(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"app.ts": `import React from "react"
import missing from "./does-not-exist"
`,
	})
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	edges := edgeSet(b)
	if edges["app.ts -imports-> module://react"] != schema.Extracted ||
		edges["app.ts -imports-> module://./does-not-exist"] != schema.Extracted {
		t.Fatalf("unresolved imports must stay as module:// dead-ends: %v", edges)
	}
	nodes := nodeIDs(b)
	if !nodes["module://react"] || !nodes["module://./does-not-exist"] {
		t.Fatal("unresolved module nodes must survive")
	}
}

func TestDeterministicResolvedOutput(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":   "module example.com/m\n",
		"main.go":  "package main\n\nimport \"example.com/m/a\"\n\nfunc main() {}\n",
		"a/a1.go":  "package a\n",
		"a/a2.go":  "package a\n",
		"web/x.ts": `import "./y"` + "\n",
		"web/y.ts": "export {}\n",
	})
	b1, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(b1.Edges) != len(b2.Edges) {
		t.Fatal("edge count varies between runs")
	}
	for i := range b1.Edges {
		if b1.Edges[i].Source != b2.Edges[i].Source || b1.Edges[i].Target != b2.Edges[i].Target {
			t.Fatalf("edge order varies at %d", i)
		}
	}
}

func TestStripJSONC(t *testing.T) {
	in := []byte(`{"a": "http://x/*not a comment*/", // trailing
/* block */ "b": 1}`)
	var v map[string]any
	if err := jsonUnmarshal(stripJSONC(in), &v); err != nil {
		t.Fatalf("stripJSONC broke valid JSONC: %v", err)
	}
	if v["a"] != "http://x/*not a comment*/" {
		t.Fatalf("string contents mangled: %q", v["a"])
	}
}

func TestGoModReplaceDirectiveResolvesLocalPath(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod": `module example.com/root

go 1.22

replace (
	example.com/api => ./staging/api
	example.com/remote => example.com/fork v1.2.3
)
`,
		"main.go":                "package main\n\nimport \"example.com/api/types\"\n\nfunc main() {}\n",
		"staging/api/types/t.go": "package types\n",
	})
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	edges := edgeSet(b)
	if edges["module://example.com/api/types -resolves_to-> staging/api/types/t.go"] != schema.Extracted {
		t.Fatalf("replace-directive package not resolved: %v", edges)
	}
}
