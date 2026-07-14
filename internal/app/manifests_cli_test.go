package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// manifests list / add / remove lifecycle: scaffold into the repo, list shows
// core + pack, remove deletes repo-first, --global lands in the store root.
func TestManifestsVerbLifecycle(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()

	out, _ := runCLI(t, 0, "manifests", "list", "--path", repo)
	if !strings.Contains(out, "npm-package-json") || !strings.Contains(out, "k8s-resource") {
		t.Fatalf("list must show core recognizers: %s", out)
	}
	if !strings.Contains(out, "packs: (none)") {
		t.Fatalf("empty pack list expected: %s", out)
	}

	out, _ = runCLI(t, 0, "manifests", "add", "internal-deps", "--path", repo)
	repoPack := filepath.Join(repo, ".ctxoptimize", "manifests", "internal-deps.json")
	if !strings.Contains(out, repoPack) {
		t.Fatalf("scaffold must print the pack path: %s", out)
	}
	if _, err := os.Stat(repoPack); err != nil {
		t.Fatalf("scaffold missing: %v", err)
	}
	// Scaffold must be immediately loadable — a template that fails the
	// loader would wedge every subsequent add.
	out, _ = runCLI(t, 0, "manifests", "list", "--path", repo)
	if !strings.Contains(out, "pack:  internal-deps") {
		t.Fatalf("list must show the scaffolded pack: %s", out)
	}

	// Re-scaffolding the same name refuses (edit or remove first).
	_, errOut := runCLI(t, 1, "manifests", "add", "internal-deps", "--path", repo)
	if !strings.Contains(errOut, "already exists") {
		t.Fatalf("duplicate scaffold must refuse: %s", errOut)
	}

	// --global scaffolds into <store-root>/manifests.
	runCLI(t, 0, "manifests", "add", "housepack", "--global", "--path", repo)
	globalPack := filepath.Join(storeRoot, "manifests", "housepack.json")
	if _, err := os.Stat(globalPack); err != nil {
		t.Fatalf("--global scaffold missing: %v", err)
	}

	// remove: repo first, then global — and says which.
	out, _ = runCLI(t, 0, "manifests", "remove", "internal-deps", "--path", repo)
	if !strings.Contains(out, "removed repo manifest pack") {
		t.Fatalf("remove must say repo: %s", out)
	}
	out, _ = runCLI(t, 0, "manifests", "remove", "housepack", "--path", repo)
	if !strings.Contains(out, "removed global manifest pack") {
		t.Fatalf("remove must say global: %s", out)
	}
	_, errOut = runCLI(t, 1, "manifests", "remove", "housepack", "--path", repo)
	if !strings.Contains(errOut, "no manifest pack") {
		t.Fatalf("removing a missing pack must fail loudly: %s", errOut)
	}
}

// Deps are queryable after add; removing a dependency from package.json
// prunes its node and declares edge on re-add (producer-scoped Replace proof).
func TestManifestDepQueryAndPrune(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()
	manifest := filepath.Join(repo, "package.json")
	if err := os.WriteFile(manifest, []byte(`{
  "dependencies": {"express": "^4.18.2", "lodash": "~4.17.21", "zod": "^3.22.0"}
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, 0, "init", "--path", repo)
	out, _ := runCLI(t, 0, "add", repo, "--path", repo)
	if !strings.Contains(out, "manifests:") {
		t.Fatalf("add must report the manifests lane: %s", out)
	}
	out, _ = runCLI(t, 0, "query", "express", "--path", repo, "--json")
	if !strings.Contains(out, "dep:npm/express") {
		t.Fatalf("dep must be queryable: %s", out)
	}

	// Drop lodash, re-add: the dep node and its declares edge must go.
	if err := os.WriteFile(manifest, []byte(`{
  "dependencies": {"express": "^4.18.2", "zod": "^3.22.0"}
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, 0, "add", repo, "--path", repo)
	key := filepath.Base(repo)
	nodes, err := os.ReadFile(filepath.Join(storeRoot, key, "graph", "nodes.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(nodes), "dep:npm/lodash") {
		t.Fatal("removed dependency must be pruned from nodes")
	}
	edges, err := os.ReadFile(filepath.Join(storeRoot, key, "graph", "edges.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(edges), "dep:npm/lodash") {
		t.Fatal("removed dependency must lose its declares edge")
	}
	if !strings.Contains(string(nodes), "dep:npm/express") {
		t.Fatal("surviving dependency must remain")
	}
}

// The same dep id lands in BOTH module stores of a monorepo — store-level
// federation (one id, per-module provenance).
func TestManifestDepFederatesAcrossModules(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()
	files := map[string]string{
		".ctxoptimize/config.json": `{"name": "maniroot", "modules": [{"path": "web"}, {"path": "api"}]}`,
		"web/package.json":         `{"dependencies": {"express": "^4.18.2", "react": "^18.2.0"}}`,
		"api/package.json":         `{"dependencies": {"express": "^5.0.0", "pg": "^8.11.0"}}`,
	}
	for p, content := range files {
		full := filepath.Join(repo, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runCLI(t, 0, "add", "--path", repo)
	for _, mod := range []string{"web", "api"} {
		nodes, err := os.ReadFile(filepath.Join(storeRoot, "maniroot", mod, "graph", "nodes.ndjson"))
		if err != nil {
			t.Fatalf("%s store missing: %v", mod, err)
		}
		if !strings.Contains(string(nodes), `"dep:npm/express"`) {
			t.Fatalf("dep:npm/express must exist in the %s store (same id federates)", mod)
		}
	}
}

// K8s topology is reachable through the normal verbs: card on a service
// shows its selects/routes_to neighborhood.
func TestManifestK8sCard(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	repo := t.TempDir()
	k8s := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: ghcr.io/example/api:1.0.0
---
apiVersion: v1
kind: Service
metadata:
  name: api
spec:
  selector:
    app: api
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: edge
spec:
  rules:
    - http:
        paths:
          - path: /
            backend:
              service:
                name: api
`
	if err := os.MkdirAll(filepath.Join(repo, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "deploy", "stack.yaml"), []byte(k8s), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, 0, "init", "--path", repo)
	runCLI(t, 0, "add", repo, "--path", repo)
	out, _ := runCLI(t, 0, "card", "k8s://default/service/api", "--path", repo)
	if !strings.Contains(out, "deployment/api") {
		t.Fatalf("service card must show its selected deployment:\n%s", out)
	}
	out, _ = runCLI(t, 0, "query", "ingress edge", "--path", repo, "--json")
	if !strings.Contains(out, "k8s://default/ingress/edge") {
		t.Fatalf("ingress resource must be queryable: %s", out)
	}
}

// A malformed manifest pack fails the add loudly, naming the file.
func TestManifestPackMalformedFailsAdd(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte(`{"dependencies": {"express": "^4.18.2"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, 0, "init", "--path", repo)
	packDir := filepath.Join(repo, ".ctxoptimize", "manifests")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "broken.json"), []byte(`{"name": "broken"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errOut := runCLI(t, 1, "add", repo, "--path", repo)
	if !strings.Contains(errOut, "broken.json") || !strings.Contains(errOut, "rule is required") {
		t.Fatalf("malformed pack must fail the add naming the file: %s", errOut)
	}
}

// manifests add <url>: a direct .json URL installs a validated pack; garbage
// is refused. Hermetic via httptest — no live network.
func TestManifestsAddFromURL(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	repo := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good.json":
			w.Write([]byte(`{"name": "goodpack", "rules": [{"file": "*.deps.json", "format": "json", "path": "libs.*", "emit": "dependency", "namespace": "x"}]}`))
		case "/bad.json":
			w.Write([]byte(`{"name": "", "rules": []}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, _ := runCLI(t, 0, "manifests", "add", srv.URL+"/good.json", "--path", repo)
	installed := filepath.Join(repo, ".ctxoptimize", "manifests", "goodpack.json")
	if !strings.Contains(out, `installed manifest pack "goodpack"`) || !strings.Contains(out, srv.URL) {
		t.Fatalf("install must say what and from where: %s", out)
	}
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("installed pack missing: %v", err)
	}

	_, errOut := runCLI(t, 1, "manifests", "add", srv.URL+"/bad.json", "--path", repo)
	if !strings.Contains(errOut, "name is required") {
		t.Fatalf("invalid pack from URL must be refused: %s", errOut)
	}
	_, errOut = runCLI(t, 1, "manifests", "add", srv.URL+"/missing.json", "--path", repo)
	if !strings.Contains(errOut, "404") {
		t.Fatalf("fetch failure must surface: %s", errOut)
	}

	// Non-github, non-.json URLs are refused up front.
	_, errOut = runCLI(t, 1, "manifests", "add", srv.URL+"/whatever", "--path", repo)
	if !strings.Contains(errOut, "github.com") {
		t.Fatalf("non-json non-github URL must be refused: %s", errOut)
	}
}

// The repo-tree scan behind `manifests add <github-url>`: manifests/*.json
// all install (and must all validate); without manifests/, top-level
// pack-shaped .json files install and non-pack json is skipped.
func TestInstallManifestPacksFromTree(t *testing.T) {
	dest := t.TempDir()
	var sb strings.Builder

	tree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tree, "manifests"), 0o755); err != nil {
		t.Fatal(err)
	}
	rule := `{"file": "*.x.json", "format": "json", "path": "a.*", "emit": "dependency"}`
	os.WriteFile(filepath.Join(tree, "manifests", "a.json"),
		[]byte(`{"name": "apack", "rules": [`+rule+`]}`), 0o644)
	os.WriteFile(filepath.Join(tree, "manifests", "b.json"),
		[]byte(`{"name": "bpack", "rules": [`+rule+`]}`), 0o644)
	n, err := installManifestPacksFromTree(tree, dest, "test-origin", &sb)
	if err != nil || n != 2 {
		t.Fatalf("manifests/ tree: n=%d err=%v", n, err)
	}
	for _, f := range []string{"apack.json", "bpack.json"} {
		if _, err := os.Stat(filepath.Join(dest, f)); err != nil {
			t.Errorf("missing installed %s", f)
		}
	}

	// A claimed pack that fails validation is loud.
	badTree := t.TempDir()
	os.MkdirAll(filepath.Join(badTree, "manifests"), 0o755)
	os.WriteFile(filepath.Join(badTree, "manifests", "broken.json"), []byte(`{"name": "x"}`), 0o644)
	if _, err := installManifestPacksFromTree(badTree, dest, "test-origin", &sb); err == nil {
		t.Fatal("invalid claimed pack must fail loudly")
	}

	// Top-level fallback: pack json installs, package.json skipped.
	flat := t.TempDir()
	os.WriteFile(filepath.Join(flat, "package.json"), []byte(`{"name": "npm-thing", "version": "1.0.0"}`), 0o644)
	os.WriteFile(filepath.Join(flat, "flatpack.json"),
		[]byte(`{"name": "flatpack", "rules": [`+rule+`]}`), 0o644)
	n, err = installManifestPacksFromTree(flat, dest, "test-origin", &sb)
	if err != nil || n != 1 {
		t.Fatalf("flat tree: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "flatpack.json")); err != nil {
		t.Errorf("missing installed flatpack.json")
	}
	if _, err := os.Stat(filepath.Join(dest, "npm-thing.json")); err == nil {
		t.Errorf("package.json must not install as a pack")
	}
}
