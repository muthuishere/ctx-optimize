package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePack(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Route packs: declarative call-shaped rules discovered from the machine dir
// (<store-root>/routes) and the repo dir (.ctxoptimize/routes), matching call
// expressions generically across languages.
func TestRoutePackMatching(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	root := t.TempDir()
	writePack(t, RepoRoutesDir(root), "myfw.json", `{
  "name": "myfw",
  "rules": [
    {"call": "registerRoute", "path_arg": 0, "handler_arg": 1, "method": "GET"},
    {"call": "route", "path_arg": 1, "method_arg": 0, "handler_arg": 2}
  ]
}`)
	files := map[string]string{
		"main.go": `package main

func handler() {}

func main() {
	registerRoute("/go", handler)
	registerRoute(dynamicPath, handler)
}
`,
		"app.js": `function jsHandler() {}
function itemsHandler() {}
api.registerRoute('/js', jsHandler);
route('post', '/items', itemsHandler);
route(verb, '/never', itemsHandler);
`,
		"svc.py": `def py_handler():
    pass

api.registerRoute("/py", py_handler)
`,
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
		t.Fatalf("batch failed the door: %v", err)
	}
	assertRoutes(t, batch,
		map[string]string{
			"main.go::route:GET /go":    "GET /go",
			"app.js::route:GET /js":     "GET /js",
			"app.js::route:POST /items": "POST /items",
			"svc.py::route:GET /py":     "GET /py",
		},
		map[string]string{
			"main.go::route:GET /go→main.go::handler":        "route-pack:myfw",
			"app.js::route:GET /js→app.js::jsHandler":        "route-pack:myfw",
			"app.js::route:POST /items→app.js::itemsHandler": "route-pack:myfw",
			"svc.py::route:GET /py→svc.py::py_handler":       "route-pack:myfw",
		},
		[]string{"main.go::route:GET dynamicPath", "app.js::route:ROUTE /never"},
	)
	// synthesized_by is stamped on the route NODE too.
	for _, n := range batch.Nodes {
		if n.Kind == "route" && n.Metadata["synthesized_by"] != "route-pack:myfw" {
			t.Errorf("node %s synthesized_by = %q", n.ID, n.Metadata["synthesized_by"])
		}
	}
}

// Repo packs beat machine packs on name collision (grammar-pack precedence);
// a machine pack with a distinct name still applies.
func TestRoutePackPrecedence(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	root := t.TempDir()
	writePack(t, filepath.Join(storeRoot, "routes"), "fw.json",
		`{"name": "fw", "rules": [{"call": "machineRoute", "path_arg": 0}]}`)
	writePack(t, filepath.Join(storeRoot, "routes"), "global.json",
		`{"name": "global", "rules": [{"call": "globalRoute", "path_arg": 0, "method": "post"}]}`)
	writePack(t, RepoRoutesDir(root), "fw.json",
		`{"name": "fw", "rules": [{"call": "repoRoute", "path_arg": 0}]}`)
	src := `machineRoute('/machine');
repoRoute('/repo');
globalRoute('/global');
`
	if err := os.WriteFile(filepath.Join(root, "a.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	batch, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	assertRoutes(t, batch,
		map[string]string{
			"a.js::route:ROUTE /repo":  "ROUTE /repo",  // repo fw.json won
			"a.js::route:POST /global": "POST /global", // fixed method upcased
		},
		map[string]string{},
		[]string{"a.js::route:ROUTE /machine"}, // shadowed machine pack rule
	)
}

// Malformed packs fail the gather LOUDLY, naming the file — never a silent
// skip (grammar-pack precedent).
func TestRoutePackMalformedFailsLoudly(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	cases := map[string]string{
		"not json":      `{`,
		"missing name":  `{"rules": [{"call": "x", "path_arg": 0}]}`,
		"no rules":      `{"name": "fw", "rules": []}`,
		"missing call":  `{"name": "fw", "rules": [{"path_arg": 0}]}`,
		"missing path":  `{"name": "fw", "rules": [{"call": "x"}]}`,
		"negative path": `{"name": "fw", "rules": [{"call": "x", "path_arg": -1}]}`,
	}
	for label, content := range cases {
		t.Run(label, func(t *testing.T) {
			root := t.TempDir()
			writePack(t, RepoRoutesDir(root), "bad.json", content)
			if err := os.WriteFile(filepath.Join(root, "a.js"), []byte("function f() {}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Extract(root)
			if err == nil {
				t.Fatal("malformed pack must fail the gather")
			}
			if !strings.Contains(err.Error(), "bad.json") {
				t.Errorf("error must name the pack file: %v", err)
			}
		})
	}
}
