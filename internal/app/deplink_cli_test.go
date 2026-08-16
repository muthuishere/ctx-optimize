package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRepo lays a fixture out under root; keys are slash paths.
func writeRepo(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// End-to-end through the real `add` path: a repo with go + npm code AND their
// manifests must yield module://<spec> --resolves_to--> dep:<ns>/<name> edges,
// the repo's own go module must NOT self-link, and `affected <dep>` must cross
// the boundary to the importing files.
func TestDeplinkEndToEnd(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()
	writeRepo(t, repo, map[string]string{
		"go.mod": "module github.com/acme/svc\n\ngo 1.22\n\nrequire (\n" +
			"\tgithub.com/redis/go-redis/v9 v9.5.0\n" +
			"\tgithub.com/lib/pq v1.10.9 // indirect\n)\n",
		"main.go": "package main\n\nimport (\n" +
			"\t\"fmt\"\n" + // stdlib — must not link
			"\t\"github.com/redis/go-redis/v9\"\n" + // external — must link
			"\t\"github.com/acme/svc/internal/store\"\n" + // self — must NOT link
			")\n\nfunc main() { fmt.Println(redis.Nil) }\n",
		"internal/store/store.go": "package store\n\nfunc Open() {}\n",
		"package.json": `{
  "dependencies": {"react": "^18.2.0"},
  "devDependencies": {"typescript": "^5.4.0"}
}`,
		"app.tsx": "import React from 'react';\n" +
			"import ts from 'typescript';\n" +
			"import { helper } from './local';\n" + // relative — must not link
			"export const App = () => React.createElement('div');\n",
	})

	runCLI(t, 0, "init", "--path", repo)
	out, _ := runCLI(t, 0, "add", repo, "--path", repo)
	if !strings.Contains(out, "deplink:") {
		t.Fatalf("add must report the deplink lane: %s", out)
	}

	key := filepath.Base(repo)
	edges := readFileString(t, filepath.Join(storeRoot, key, "graph", "edges.ndjson"))
	nodes := readFileString(t, filepath.Join(storeRoot, key, "graph", "nodes.ndjson"))

	// Every expected link is present…
	wantLinks := []string{
		`"source":"module://github.com/redis/go-redis/v9","target":"dep:go/github.com/redis/go-redis/v9","relation":"resolves_to"`,
		`"source":"module://react","target":"dep:npm/react","relation":"resolves_to"`,
		`"source":"module://typescript","target":"dep:npm/typescript","relation":"resolves_to"`,
	}
	for _, w := range wantLinks {
		if !strings.Contains(edges, w) {
			t.Errorf("missing resolves_to edge: %s", w)
		}
	}
	// …and every dep-boundary link is INFERRED + synthesized_by:deplink (never
	// claimed EXTRACTED); in-repo links (module://self/... -> file) come from
	// importresolve, where the go.mod module line makes EXTRACTED honest.
	for _, line := range strings.Split(edges, "\n") {
		if !strings.Contains(line, `"relation":"resolves_to"`) {
			continue
		}
		if strings.Contains(line, `"target":"dep:`) {
			if !strings.Contains(line, `"confidence":"INFERRED"`) || !strings.Contains(line, `"synthesized_by":"deplink"`) {
				t.Errorf("dep resolves_to edge not INFERRED+synthesized_by: %s", line)
			}
		} else if !strings.Contains(line, `"synthesized_by":"importresolve"`) {
			t.Errorf("non-dep resolves_to edge from unknown producer: %s", line)
		}
	}
	// The self-module import now resolves to the actual file (ADR 2026-08-13 D7).
	if !strings.Contains(edges, `"source":"module://github.com/acme/svc/internal/store","target":"internal/store/store.go","relation":"resolves_to","confidence":"EXTRACTED"`) {
		t.Error("self-module import must resolve_to its in-repo file, EXTRACTED")
	}

	// The repo's OWN module must never become a dependency link (spike 1).
	//
	// Checked on the EDGE, not by scanning the whole file for two substrings
	// that may sit on different lines. ADR 22 D0 emits
	// `go.mod -publishes-> dep:go/github.com/acme/svc` — the module's identity,
	// which is the point — and the old scan read that as a violation. Matching
	// source AND target on one edge is strictly stronger than the scan it
	// replaces: it would also catch a self-link the scan could miss.
	for _, line := range strings.Split(edges, "\n") {
		if !strings.Contains(line, `"relation":"resolves_to"`) {
			continue
		}
		var e struct{ Source, Target string }
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if strings.HasPrefix(e.Source, "module://github.com/acme/svc") &&
			strings.HasPrefix(e.Target, "dep:go/github.com/acme/svc") {
			t.Errorf("self-module import resolves_to a dep node: %s", line)
		}
	}
	// stdlib and relative specifiers never link.
	for _, spec := range []string{`"source":"module://fmt","relation":"resolves_to"`} {
		if strings.Contains(edges, spec) {
			t.Errorf("stdlib must not link: %s", spec)
		}
	}

	// Node-level scope aggregate distinguishes runtime from dev (issue #5).
	if !strings.Contains(nodes, `"id":"dep:npm/react"`) || !strings.Contains(nodes, `"scopes":"runtime"`) {
		t.Error("react dep node must carry scopes=runtime")
	}
	if !nodeHasScopes(nodes, "dep:npm/typescript", "dev") {
		t.Error("typescript dep node must carry scopes=dev")
	}
	// scope_class also present on the declares edges.
	if !strings.Contains(edges, `"scope_class":"dev"`) || !strings.Contains(edges, `"scope_class":"runtime"`) {
		t.Error("declares edges must carry normalized scope_class")
	}

	// affected crosses the dependency boundary to the importing file.
	aff, _ := runCLI(t, 0, "affected", "dep:npm/react", "--path", repo)
	if !strings.Contains(aff, "app.tsx") {
		t.Errorf("affected dep:npm/react must reach the importing file: %s", aff)
	}
}

// Re-add after removing a dependency prunes its resolves_to edge (deplink has
// its own producer-scoped Replace lifecycle).
func TestDeplinkPrunesOnRemoval(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()
	pkg := filepath.Join(repo, "package.json")
	writeRepo(t, repo, map[string]string{
		"package.json": `{"dependencies": {"react": "^18.2.0", "lodash": "^4.17.21"}}`,
		"app.tsx":      "import React from 'react';\nimport _ from 'lodash';\n",
	})
	runCLI(t, 0, "init", "--path", repo)
	runCLI(t, 0, "add", repo, "--path", repo)

	key := filepath.Base(repo)
	edgesPath := filepath.Join(storeRoot, key, "graph", "edges.ndjson")
	if !strings.Contains(readFileString(t, edgesPath), "dep:npm/lodash") {
		t.Fatal("lodash link must exist before removal")
	}

	// Drop lodash from the manifest AND the import; re-add.
	if err := os.WriteFile(pkg, []byte(`{"dependencies": {"react": "^18.2.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.tsx"), []byte("import React from 'react';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, 0, "add", repo, "--path", repo)

	edges := readFileString(t, edgesPath)
	if strings.Contains(edges, "dep:npm/lodash") {
		t.Error("removed dependency's resolves_to edge must be pruned")
	}
	if !strings.Contains(edges, `"target":"dep:npm/react","relation":"resolves_to"`) {
		t.Error("surviving dependency's link must remain")
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// nodeHasScopes checks a specific dep node's line carries scopes=want (order
// of metadata keys in the marshaled line is not assumed).
func nodeHasScopes(nodes, id, want string) bool {
	for _, line := range strings.Split(nodes, "\n") {
		if strings.Contains(line, `"id":"`+id+`"`) {
			return strings.Contains(line, `"scopes":"`+want+`"`)
		}
	}
	return false
}
