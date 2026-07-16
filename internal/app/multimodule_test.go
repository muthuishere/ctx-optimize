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
		// Same-named hub symbol in BOTH modules: the cross-module echo that
		// gates the boundary note on module-scoped affected/path.
		"services/api/shared.go":    "package api\n\nfunc SharedThing() {}\n\nfunc useSharedA() { SharedThing() }\n",
		"services/worker/shared.go": "package worker\n\nfunc SharedThing() {}\n\nfunc useSharedB() { SharedThing() }\n",
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

func TestAffectedBoundaryNote(t *testing.T) {
	repo := fakeMonorepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)
	apiDir := filepath.Join(repo, "services", "api")

	// SharedThing also lives in services/worker's hubs → cross-module echo
	// → the module-scoped answer must carry the boundary note.
	out, _ := runCLI(t, 0, "affected", "SharedThing", "--path", apiDir)
	if !strings.Contains(out, "note: module-scoped") || !strings.Contains(out, "--root") {
		t.Fatalf("expected boundary note:\n%s", out)
	}
	// HandleCheckout exists only in api → no echo → no note.
	out, _ = runCLI(t, 0, "affected", "HandleCheckout", "--path", apiDir)
	if strings.Contains(out, "note: module-scoped") {
		t.Fatalf("note printed without a cross-module echo:\n%s", out)
	}
	// --root answers repo-wide: no module boundary, no note.
	out, _ = runCLI(t, 0, "affected", "HandleCheckout", "--path", apiDir, "--root")
	if strings.Contains(out, "note: module-scoped") {
		t.Fatalf("--root must not carry the module note:\n%s", out)
	}
	// JSON carries the note as a field, not mixed into the document.
	out, _ = runCLI(t, 0, "affected", "SharedThing", "--json", "--path", apiDir)
	var res struct {
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("affected --json not parseable: %v\n%s", err, out)
	}
	if !strings.Contains(res.Note, "module-scoped") {
		t.Fatalf("json note missing: %+v", res)
	}
}

func TestAffectedEscalatesAcrossModules(t *testing.T) {
	repo := fakeMonorepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)
	apiDir := filepath.Join(repo, "services", "api")

	// RunPayrollJob is not in api: escalate, answer repo-wide, say where.
	out, _ := runCLI(t, 0, "affected", "RunPayrollJob", "--path", apiDir)
	if !strings.Contains(out, "found in services/worker") || !strings.Contains(out, "impacts") {
		t.Fatalf("affected escalation: %s", out)
	}
}

func TestPathBoundaryAndEscalation(t *testing.T) {
	repo := fakeMonorepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)
	apiDir := filepath.Join(repo, "services", "api")

	// Both endpoints local, but SharedThing echoes in another module → note.
	out, _ := runCLI(t, 0, "path", "SharedThing", "useSharedA", "--path", apiDir)
	if !strings.Contains(out, "note: module-scoped") {
		t.Fatalf("expected boundary note on path:\n%s", out)
	}
	// Endpoints live in a sibling module → escalate repo-wide, labeled.
	out, _ = runCLI(t, 0, "path", "RunPayrollJob", "loop", "--path", apiDir)
	if !strings.Contains(out, "answered repo-wide") {
		t.Fatalf("path escalation: %s", out)
	}
}

func TestStatusShowsSavings(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	if err := os.WriteFile(filepath.Join(repo, "notes.md"), []byte("# Solo Notes\n\nBody line.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, 0, "init", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)
	runCLI(t, 0, "query", "Solo", "--path", repo) // records one served answer
	out, _ := runCLI(t, 0, "status", "--path", repo)
	if !strings.Contains(out, "tokens saved") || !strings.Contains(out, "served: 1 answers") {
		t.Fatalf("status must surface the savings line:\n%s", out)
	}
}

// A module whose code vanishes must NOT keep its stale graph silently: the
// empty code batch has to reach Replace so the shrink guard refuses loudly,
// and --force does the honest prune. (Regression: gatherInto used to skip
// empty code batches entirely — "added 0 nodes", exit 0, deleted functions
// still answered queries.)
func TestEmptiedModuleHitsShrinkGuard(t *testing.T) {
	repo := fakeMonorepo(t)
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)

	// Gut the worker module: all code gone, go.mod remains.
	if err := os.Remove(filepath.Join(repo, "services/worker/worker.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "services/worker/shared.go")); err != nil {
		t.Fatal(err)
	}

	out, errb := runCLI(t, 1, "add", "--path", repo)
	if !strings.Contains(out+errb, "refusing to shrink") {
		t.Fatalf("emptied module must trip the shrink guard, got:\n%s%s", out, errb)
	}

	out, _ = runCLI(t, 0, "add", "--force", "--path", repo)
	if !strings.Contains(out, "pruned") {
		t.Fatalf("--force must prune the deleted code, got:\n%s", out)
	}
	data, err := os.ReadFile(filepath.Join(storeRoot, filepath.Base(repo), "services/worker/graph/nodes.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "RunPayrollJob") {
		t.Fatal("deleted function still in the graph after --force add")
	}
}

// writeFiles drops path→content pairs under repo (creating dirs).
func writeFiles(t *testing.T, repo string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		full := filepath.Join(repo, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestMultiPathModuleGathersScatteredFoldersIntoOneStore is the ADR 2026-07-14
// Move 1 acceptance: a module = a NAME + a SET of scattered dirs (the .NET
// src/ + tests/ split). The two folders must land in ONE store keyed by the
// name, with IDs repo-root-relative, and — the whole point — a call from the
// test folder to the source folder must RESOLVE (single extraction pass).
func TestMultiPathModuleGathersScatteredFoldersIntoOneStore(t *testing.T) {
	repo := t.TempDir()
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)

	writeFiles(t, repo, map[string]string{
		".ctxoptimize/config.json": `{"name":"acme","modules":[` +
			`{"name":"Billing","paths":["src/Billing","tests/Billing.Tests"]}]}`,
		"src/Billing/billing.go":            "package billing\n\nfunc Charge() {}\n",
		"tests/Billing.Tests/billing_ck.go": "package tests\n\nfunc VerifyCharge() { Charge() }\n",
	})

	runCLI(t, 0, "add", "--path", repo)

	// One store, keyed by the module NAME — not two folder-mirrored stores.
	if _, err := os.Stat(filepath.Join(storeRoot, "acme", "Billing", "graph")); err != nil {
		t.Fatalf("expected one store acme/Billing: %v", err)
	}
	for _, gone := range []string{"src", "tests", "src-Billing", "tests-Billing.Tests"} {
		if _, err := os.Stat(filepath.Join(storeRoot, "acme", gone, "graph")); err == nil {
			t.Fatalf("unexpected folder-mirrored store %q — module must be name-keyed", gone)
		}
	}

	// Node sources are repo-root-relative so the two folders can't collide.
	nodes, err := os.ReadFile(filepath.Join(storeRoot, "acme", "Billing", "graph", "nodes.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nodes), "src/Billing/billing.go") ||
		!strings.Contains(string(nodes), "tests/Billing.Tests/billing_ck.go") {
		t.Fatalf("sources not repo-root-relative in the merged store:\n%s", nodes)
	}

	// The payoff: test→source call resolved across the folder split.
	out, _ := runCLI(t, 0, "card", "Charge", "--path", repo, "--json")
	var card struct {
		CalledBy []string `json:"called_by"`
	}
	if err := json.Unmarshal([]byte(out), &card); err != nil {
		t.Fatalf("card --json: %v\n%s", err, out)
	}
	found := false
	for _, c := range card.CalledBy {
		if strings.Contains(c, "VerifyCharge") {
			found = true
		}
	}
	if !found {
		t.Fatalf("test→source call did not resolve across folders; called_by=%v", card.CalledBy)
	}
}

// TestMultiPathModuleScopeFromSubdir: asking from inside one of a multi-path
// module's folders resolves to that module (not a shadow store), and a
// module-scoped add re-gathers ALL its folders into the one store.
func TestMultiPathModuleScopeFromSubdir(t *testing.T) {
	repo := t.TempDir()
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)

	writeFiles(t, repo, map[string]string{
		".ctxoptimize/config.json": `{"name":"acme","modules":[` +
			`{"name":"Billing","paths":["src/Billing","tests/Billing.Tests"]}]}`,
		"src/Billing/billing.go":            "package billing\n\nfunc Charge() {}\n",
		"tests/Billing.Tests/billing_ck.go": "package tests\n\nfunc VerifyCharge() { Charge() }\n",
	})
	runCLI(t, 0, "add", "--path", repo)

	// Module-scoped add from inside the TEST folder must refresh the whole
	// Billing store (both folders), keyed by name — not a store for tests/.
	runCLI(t, 0, "add", "--path", filepath.Join(repo, "tests/Billing.Tests"))
	nodes, err := os.ReadFile(filepath.Join(storeRoot, "acme", "Billing", "graph", "nodes.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nodes), "src/Billing/billing.go") {
		t.Fatalf("module-scoped add from tests/ dropped the src/ folder:\n%s", nodes)
	}

	// A query from inside the source folder still finds the symbol.
	out, _ := runCLI(t, 0, "query", "Charge", "--path", filepath.Join(repo, "src/Billing"))
	if !strings.Contains(out, "Charge") {
		t.Fatalf("module-scoped query missed Charge:\n%s", out)
	}
}

// --instructions on init: chooses the pointer target (accepting agents.md /
// claude.md forms), persists to project config, re-init stays idempotent
// (identical content is never rewritten), and typos fail loudly.
func TestInitInstructionsFlag(t *testing.T) {
	repo := t.TempDir()
	writeFiles(t, repo, map[string]string{"a.go": "package a\n\nfunc A() {}\n"})
	store := t.TempDir()

	runCLI(t, 0, "init", "--path", repo, "--store", store, "--instructions", "agents.md")
	if _, err := os.Stat(filepath.Join(repo, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatal("CLAUDE.md written despite --instructions agents.md")
	}
	data, err := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	if err != nil || !strings.Contains(string(data), "ctx-optimize") {
		t.Fatalf("AGENTS.md missing pointer: %v", err)
	}
	cfg, err := project.Load(repo)
	if err != nil || cfg.Instructions != "AGENTS" {
		t.Fatalf("instructions not persisted: %+v %v", cfg, err)
	}

	// Idempotent re-init: same bytes, and the CLI says so.
	before := sha256.Sum256(data)
	out, _ := runCLI(t, 0, "init", "--path", repo, "--store", store)
	if !strings.Contains(out, "already current") {
		t.Errorf("re-init did not report already-current: %s", out)
	}
	after, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	if sha256.Sum256(after) != before {
		t.Error("re-init rewrote identical pointer content")
	}

	// Typo: loud refusal, nothing written.
	runCLI(t, 1, "init", "--path", repo, "--store", store, "--instructions", "agnets")
}
