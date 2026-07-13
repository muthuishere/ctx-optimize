package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/project"
)

// fakeMonorepo builds a two-module repo with distinct symbols per module so
// scope resolution is observable in query hits.
func fakeMonorepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	files := map[string]string{
		"README.md":                 "# Acme monorepo\n",
		"go.work":                   "go 1.22\n",
		"services/api/go.mod":       "module acme/api\n",
		"services/api/handler.go":   "package api\n\nfunc HandleCheckout() {}\n\nfunc helperApi() { HandleCheckout() }\n",
		"services/api/README.md":    "# API service\n",
		"services/worker/go.mod":    "module acme/worker\n",
		"services/worker/worker.go": "package worker\n\nfunc RunPayrollJob() {}\n\nfunc loop() { RunPayrollJob() }\n",
		// A module NESTED inside another (the beam maven-archetypes case):
		// must get its own store, and must not be double-extracted by api.
		"services/api/plugin/go.mod":    "module acme/api/plugin\n",
		"services/api/plugin/plugin.go": "package plugin\n\nfunc NestedPluginEntry() {}\n",
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
	return repo
}

func runCLI(t *testing.T, wantCode int, args ...string) (string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb)
	if code != wantCode {
		t.Fatalf("%v: exit %d (want %d): %s%s", args, code, wantCode, out.String(), errb.String())
	}
	return out.String(), errb.String()
}

func TestScanVerbReadOnly(t *testing.T) {
	repo := fakeMonorepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	out, _ := runCLI(t, 0, "scan", "--path", repo, "--json")
	var res struct {
		Modules []struct{ Path string } `json:"modules"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("scan --json not parseable: %v\n%s", err, out)
	}
	if len(res.Modules) != 3 || res.Modules[0].Path != "services/api" ||
		res.Modules[1].Path != "services/api/plugin" || res.Modules[2].Path != "services/worker" {
		t.Fatalf("scan found %+v", res.Modules)
	}
	// Read-only: nothing written.
	if _, err := os.Stat(filepath.Join(repo, ".ctxoptimize")); !os.IsNotExist(err) {
		t.Fatal("scan must not scaffold")
	}
}

func TestInitScanWritesFullListAndAddFansOut(t *testing.T) {
	repo := fakeMonorepo(t)
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)

	out, _ := runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	if !strings.Contains(out, "3 modules") {
		t.Fatalf("init --scan output: %s", out)
	}
	cfg, err := project.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Modules) != 3 {
		t.Fatalf("config modules: %+v", cfg.Modules)
	}

	out, _ = runCLI(t, 0, "add", "--path", repo)
	if !strings.Contains(out, "== services/api") || !strings.Contains(out, "== services/worker") || !strings.Contains(out, "== navigator") {
		t.Fatalf("fan-out output: %s", out)
	}
	key := filepath.Base(repo)
	for _, p := range []string{
		key + "/services/api/graph/nodes.ndjson",
		key + "/services/worker/graph/nodes.ndjson",
		key + "/modules.json",
		key + "/navigator.md",
		key + "/wiki/index.md",
	} {
		if _, err := os.Stat(filepath.Join(storeRoot, filepath.FromSlash(p))); err != nil {
			t.Fatalf("missing store artifact %s: %v", p, err)
		}
	}
	// Exclusion: the root residual must not contain module code.
	data, err := os.ReadFile(filepath.Join(storeRoot, key, "graph", "nodes.ndjson"))
	if err == nil && strings.Contains(string(data), "HandleCheckout") {
		t.Fatal("module code leaked into the root residual store")
	}
	// Nested module: own store, and NOT double-extracted by its parent.
	nested, err := os.ReadFile(filepath.Join(storeRoot, filepath.FromSlash(key+"/services/api/plugin/graph/nodes.ndjson")))
	if err != nil || !strings.Contains(string(nested), "NestedPluginEntry") {
		t.Fatalf("nested module store missing its symbol: %v", err)
	}
	parent, err := os.ReadFile(filepath.Join(storeRoot, filepath.FromSlash(key+"/services/api/graph/nodes.ndjson")))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(parent), "NestedPluginEntry") {
		t.Fatal("nested module code double-extracted into its parent module store")
	}
	// The parent's manifest must not claim the nested store's artifacts.
	mf, err := os.ReadFile(filepath.Join(storeRoot, filepath.FromSlash(key+"/services/api/manifest.json")))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mf), "plugin/graph") {
		t.Fatal("parent manifest fingerprints the nested store")
	}
}

func TestQueryScopes(t *testing.T) {
	repo := fakeMonorepo(t)
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)

	// Module scope: local answer, labeled with the module.
	apiDir := filepath.Join(repo, "services", "api")
	out, _ := runCLI(t, 0, "query", "HandleCheckout", "--path", apiDir)
	if !strings.Contains(out, "[services-api]") || !strings.Contains(out, "HandleCheckout") {
		t.Fatalf("module-scope query: %s", out)
	}

	// Escalation: symbol from the SIBLING module — zero local hits must
	// escalate to root federation and still find it.
	out, _ = runCLI(t, 0, "query", "RunPayrollJob", "--path", apiDir)
	if !strings.Contains(out, "escalating to root") || !strings.Contains(out, "RunPayrollJob") {
		t.Fatalf("escalation query: %s", out)
	}

	// Root scope: federated, labeled.
	out, _ = runCLI(t, 0, "query", "RunPayrollJob", "--path", repo)
	if !strings.Contains(out, "[root:") || !strings.Contains(out, "RunPayrollJob") {
		t.Fatalf("root query: %s", out)
	}
}

func TestCardEscalatesAcrossModules(t *testing.T) {
	repo := fakeMonorepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)

	apiDir := filepath.Join(repo, "services", "api")
	out, _ := runCLI(t, 0, "card", "RunPayrollJob", "--path", apiDir)
	if !strings.Contains(out, "found in services/worker") || !strings.Contains(out, "RunPayrollJob") {
		t.Fatalf("card escalation: %s", out)
	}
}

func TestAdoptionRule(t *testing.T) {
	repo := fakeMonorepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)

	apiDir := filepath.Join(repo, "services", "api")
	out, _ := runCLI(t, 0, "init", "--path", apiDir)
	if !strings.Contains(out, "adopted as module") {
		t.Fatalf("init in declared module must adopt: %s", out)
	}
	cfg, err := project.Load(apiDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModuleOf != filepath.Base(repo) {
		t.Fatalf("child config module_of = %q", cfg.ModuleOf)
	}
}

func TestFanOutDeterministicAcrossJobs(t *testing.T) {
	repo := fakeMonorepo(t)

	hashStore := func(root string) map[string]string {
		t.Helper()
		sums := map[string]string{}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sums[filepath.ToSlash(rel)] = string(sha256.New().Sum(data))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return sums
	}

	storeA := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeA)
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	outA, _ := runCLI(t, 0, "add", "--path", repo, "--jobs", "1")

	storeB := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeB)
	outB, _ := runCLI(t, 0, "add", "--path", repo, "--jobs", "8")

	replA := strings.ReplaceAll(outA, storeA, "STORE")
	replB := strings.ReplaceAll(outB, storeB, "STORE")
	if replA != replB {
		t.Fatalf("output differs across --jobs:\n--- jobs=1\n%s\n--- jobs=8\n%s", replA, replB)
	}
	a, b := hashStore(storeA), hashStore(storeB)
	if len(a) != len(b) {
		t.Fatalf("store file count differs: %d vs %d", len(a), len(b))
	}
	for p, h := range a {
		if b[p] != h {
			t.Fatalf("store file %s differs across --jobs", p)
		}
	}
}

func TestSingleModuleRepoUnchanged(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	if err := os.WriteFile(filepath.Join(repo, "notes.md"), []byte("# Solo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, 0, "init", "--path", repo)
	out, _ := runCLI(t, 0, "add", "--path", repo)
	if strings.Contains(out, "==") || strings.Contains(out, "navigator") {
		t.Fatalf("single-module add must not fan out: %s", out)
	}
	out, _ = runCLI(t, 0, "query", "Solo", "--path", repo)
	if strings.Contains(out, "[") && strings.Contains(out, "scope") {
		t.Fatalf("single-module query output must stay label-free: %s", out)
	}
}
