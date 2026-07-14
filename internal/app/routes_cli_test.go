package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// routes list / add / remove lifecycle: scaffold into the repo, list shows
// core + pack, remove deletes repo-first, --global lands in the store root.
func TestRoutesVerbLifecycle(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()

	out, _ := runCLI(t, 0, "routes", "list", "--path", repo)
	if !strings.Contains(out, "fastapi-route") || !strings.Contains(out, "ingress-route") {
		t.Fatalf("list must show core recognizers: %s", out)
	}
	if !strings.Contains(out, "packs: (none)") {
		t.Fatalf("empty pack list expected: %s", out)
	}

	out, _ = runCLI(t, 0, "routes", "add", "myfw", "--path", repo)
	repoPack := filepath.Join(repo, ".ctxoptimize", "routes", "myfw.json")
	if !strings.Contains(out, repoPack) {
		t.Fatalf("scaffold must print the pack path: %s", out)
	}
	if _, err := os.Stat(repoPack); err != nil {
		t.Fatalf("scaffold missing: %v", err)
	}
	// Scaffold must be immediately loadable — a template that fails the
	// loader would wedge every subsequent add.
	out, _ = runCLI(t, 0, "routes", "list", "--path", repo)
	if !strings.Contains(out, "pack:  myfw") {
		t.Fatalf("list must show the scaffolded pack: %s", out)
	}

	// Re-scaffolding the same name refuses (edit or remove first).
	_, errOut := runCLI(t, 1, "routes", "add", "myfw", "--path", repo)
	if !strings.Contains(errOut, "already exists") {
		t.Fatalf("duplicate scaffold must refuse: %s", errOut)
	}

	// --global scaffolds into <store-root>/routes.
	runCLI(t, 0, "routes", "add", "housefw", "--global", "--path", repo)
	globalPack := filepath.Join(storeRoot, "routes", "housefw.json")
	if _, err := os.Stat(globalPack); err != nil {
		t.Fatalf("--global scaffold missing: %v", err)
	}

	// remove: repo first, then global — and says which.
	out, _ = runCLI(t, 0, "routes", "remove", "myfw", "--path", repo)
	if !strings.Contains(out, "removed repo route pack") {
		t.Fatalf("remove must say repo: %s", out)
	}
	out, _ = runCLI(t, 0, "routes", "remove", "housefw", "--path", repo)
	if !strings.Contains(out, "removed global route pack") {
		t.Fatalf("remove must say global: %s", out)
	}
	_, errOut = runCLI(t, 1, "routes", "remove", "housefw", "--path", repo)
	if !strings.Contains(errOut, "no route pack") {
		t.Fatalf("removing a missing pack must fail loudly: %s", errOut)
	}
}

// A scaffolded pack is live at add time; a malformed pack fails the add
// loudly, naming the file.
func TestRoutesPackThroughAdd(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	repo := t.TempDir()
	src := "function handler() {}\nregisterRoute('/thing', handler);\n"
	if err := os.WriteFile(filepath.Join(repo, "app.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, 0, "init", "--path", repo)
	runCLI(t, 0, "routes", "add", "myfw", "--path", repo)
	runCLI(t, 0, "add", repo, "--path", repo)
	out, _ := runCLI(t, 0, "query", "GET /thing", "--path", repo, "--json")
	if !strings.Contains(out, "route:GET /thing") {
		t.Fatalf("pack route must be queryable: %s", out)
	}

	// Corrupt the pack: the next add must fail loudly, naming the file.
	bad := filepath.Join(repo, ".ctxoptimize", "routes", "myfw.json")
	if err := os.WriteFile(bad, []byte(`{"name": "myfw", "rules": [{"path_arg": 0}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errOut := runCLI(t, 1, "add", repo, "--path", repo)
	if !strings.Contains(errOut, "myfw.json") || !strings.Contains(errOut, "call is required") {
		t.Fatalf("malformed pack must fail the add naming the file: %s", errOut)
	}
}

// routes add <url>: a direct .json URL installs a validated pack; garbage is
// refused. Hermetic via httptest — no live network.
func TestRoutesAddFromURL(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	repo := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good.json":
			w.Write([]byte(`{"name": "webfw", "rules": [{"call": "handle", "path_arg": 0}]}`))
		case "/bad.json":
			w.Write([]byte(`{"name": "", "rules": []}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, _ := runCLI(t, 0, "routes", "add", srv.URL+"/good.json", "--path", repo)
	installed := filepath.Join(repo, ".ctxoptimize", "routes", "webfw.json")
	if !strings.Contains(out, `installed route pack "webfw"`) || !strings.Contains(out, srv.URL) {
		t.Fatalf("install must say what and from where: %s", out)
	}
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("installed pack missing: %v", err)
	}

	_, errOut := runCLI(t, 1, "routes", "add", srv.URL+"/bad.json", "--path", repo)
	if !strings.Contains(errOut, "name is required") {
		t.Fatalf("invalid pack from URL must be refused: %s", errOut)
	}
	_, errOut = runCLI(t, 1, "routes", "add", srv.URL+"/missing.json", "--path", repo)
	if !strings.Contains(errOut, "404") {
		t.Fatalf("fetch failure must surface: %s", errOut)
	}

	// Non-github, non-.json URLs are refused up front.
	_, errOut = runCLI(t, 1, "routes", "add", srv.URL+"/whatever", "--path", repo)
	if !strings.Contains(errOut, "github.com") {
		t.Fatalf("non-json non-github URL must be refused: %s", errOut)
	}
}

// The repo-tree scan behind `routes add <github-url>`: routes/*.json all
// install (and must all validate); without routes/, top-level pack-shaped
// .json files install and non-pack json (package.json) is skipped.
func TestInstallRoutePacksFromTree(t *testing.T) {
	dest := t.TempDir()
	var sb strings.Builder

	tree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tree, "routes"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(tree, "routes", "a.json"),
		[]byte(`{"name": "afw", "rules": [{"call": "a", "path_arg": 0}]}`), 0o644)
	os.WriteFile(filepath.Join(tree, "routes", "b.json"),
		[]byte(`{"name": "bfw", "rules": [{"call": "b", "path_arg": 0}]}`), 0o644)
	n, err := installRoutePacksFromTree(tree, dest, "test-origin", &sb)
	if err != nil || n != 2 {
		t.Fatalf("routes/ tree: n=%d err=%v", n, err)
	}
	for _, f := range []string{"afw.json", "bfw.json"} {
		if _, err := os.Stat(filepath.Join(dest, f)); err != nil {
			t.Errorf("missing installed %s", f)
		}
	}

	// A claimed pack that fails validation is loud.
	badTree := t.TempDir()
	os.MkdirAll(filepath.Join(badTree, "routes"), 0o755)
	os.WriteFile(filepath.Join(badTree, "routes", "broken.json"), []byte(`{"name": "x"}`), 0o644)
	if _, err := installRoutePacksFromTree(badTree, dest, "test-origin", &sb); err == nil {
		t.Fatal("invalid claimed pack must fail loudly")
	}

	// Top-level fallback: pack json installs, package.json skipped.
	flat := t.TempDir()
	os.WriteFile(filepath.Join(flat, "package.json"), []byte(`{"name": "npm-thing", "version": "1.0.0"}`), 0o644)
	os.WriteFile(filepath.Join(flat, "flatfw.json"),
		[]byte(`{"name": "flatfw", "rules": [{"call": "f", "path_arg": 0}]}`), 0o644)
	n, err = installRoutePacksFromTree(flat, dest, "test-origin", &sb)
	if err != nil || n != 1 {
		t.Fatalf("flat tree: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "flatfw.json")); err != nil {
		t.Errorf("missing installed flatfw.json")
	}
	if _, err := os.Stat(filepath.Join(dest, "npm-thing.json")); err == nil {
		t.Errorf("package.json must not install as a pack")
	}
}
