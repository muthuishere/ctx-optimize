package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/project"
)

// TestMultiModuleLifecycle replays a whole team's worth of store history
// against one repo: init --scan, fan-out add, config edits, re-init (plain
// and --scan), module pruned from config, module re-adopted, symbol renamed,
// qualified card guesses, and repeated adds. Every step asserts the store
// stays truthful — no clobbered config, no duplicate pointer blocks, no
// stale or duplicated nodes, no silent shrink.
func TestMultiModuleLifecycle(t *testing.T) {
	repo := fakeMonorepo(t)
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	rootKey := filepath.Base(repo)
	rootNodes := filepath.Join(storeRoot, rootKey, "graph", "nodes.ndjson")
	workerNodes := filepath.Join(storeRoot, rootKey, "services/worker", "graph", "nodes.ndjson")

	// -- 1: generate + gather ------------------------------------------------
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)

	// Federated root query reaches a module symbol; module-scoped query stays
	// in its module; a symbol from ANOTHER module escalates instead of 404ing.
	out, _ := runCLI(t, 0, "query", "RunPayrollJob", "--path", repo)
	if !strings.Contains(out, "RunPayrollJob") {
		t.Fatalf("federated root query missed module symbol:\n%s", out)
	}
	apiDir := filepath.Join(repo, "services/api")
	out, _ = runCLI(t, 0, "query", "HandleCheckout", "--path", apiDir)
	if !strings.Contains(out, "HandleCheckout") {
		t.Fatalf("module-scoped query missed own symbol:\n%s", out)
	}
	out, _ = runCLI(t, 0, "query", "RunPayrollJob", "--path", apiDir)
	if !strings.Contains(out, "RunPayrollJob") {
		t.Fatalf("zero-hit module query must escalate repo-wide:\n%s", out)
	}

	// -- 2: card survives invented qualifiers, suggests on total miss --------
	out, _ = runCLI(t, 0, "card", "acme.api.HandleCheckout", "--path", repo)
	if !strings.Contains(out, "HandleCheckout") {
		t.Fatalf("qualified card guess should resolve via last segment:\n%s", out)
	}
	out, errb := runCLI(t, 1, "card", "Chekout", "--path", repo)
	if !strings.Contains(out+errb, "did you mean") || !strings.Contains(out+errb, "HandleCheckout") {
		t.Fatalf("card total miss must suggest nearest labels:\n%s%s", out, errb)
	}

	// -- 3: user owns the config; re-init must not clobber it ----------------
	cfg, err := project.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Remote = &project.Remote{URL: "file:///tmp/team-remote"}
	kept := cfg.Modules[:0]
	for _, m := range cfg.Modules {
		if m.Path != "services/worker" {
			kept = append(kept, m)
		}
	}
	cfg.Modules = kept
	if err := project.Save(repo, cfg); err != nil {
		t.Fatal(err)
	}

	runCLI(t, 0, "init", "--path", repo) // plain re-init
	cfg2, err := project.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Remote == nil || cfg2.Remote.URL != "file:///tmp/team-remote" {
		t.Fatalf("plain re-init clobbered remote: %+v", cfg2.Remote)
	}
	if len(cfg2.Modules) != 2 {
		t.Fatalf("plain re-init clobbered pruned modules: %+v", cfg2.Modules)
	}
	claude, err := os.ReadFile(filepath.Join(repo, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(claude), "ctx-optimize:begin"); n != 1 {
		t.Fatalf("pointer block must stay single after re-init, got %d", n)
	}

	// -- 4: pruned module's code joins the ROOT residual; orphan store noted -
	out, _ = runCLI(t, 0, "add", "--path", repo)
	if !strings.Contains(out, "no longer in config.json") || !strings.Contains(out, "services/worker") {
		t.Fatalf("orphan store must be surfaced:\n%s", out)
	}
	rootGraph, err := os.ReadFile(rootNodes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rootGraph), "RunPayrollJob") {
		t.Fatal("pruned module's code must be swept into the root residual graph")
	}
	out, _ = runCLI(t, 0, "query", "RunPayrollJob", "--path", repo)
	if !strings.Contains(out, "RunPayrollJob") {
		t.Fatalf("root query lost the pruned module's symbols:\n%s", out)
	}

	// -- 5: re-scan restores the module list, preserves name+remote ----------
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	cfg3, err := project.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg3.Modules) != 3 || cfg3.Remote == nil || cfg3.Remote.URL != "file:///tmp/team-remote" {
		t.Fatalf("re-scan must restore modules and keep remote: %+v", cfg3)
	}

	// -- 6: re-adoption makes the root residual shrink — guard fires loudly,
	// --force prunes, and the symbol lives in exactly one store afterwards ---
	out, errb = runCLI(t, 1, "add", "--path", repo)
	if !strings.Contains(out+errb, "refusing to shrink") {
		t.Fatalf("root residual shrink after re-adoption must be refused:\n%s%s", out, errb)
	}
	runCLI(t, 0, "add", "--force", "--path", repo)
	rootGraph, err = os.ReadFile(rootNodes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rootGraph), "RunPayrollJob") {
		t.Fatal("re-adopted module's code must leave the root residual (duplicate nodes)")
	}
	workerGraph, err := os.ReadFile(workerNodes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workerGraph), "RunPayrollJob") {
		t.Fatal("re-adopted module store missing its own symbol")
	}

	// -- 7: rename prunes the old truth -------------------------------------
	handler := filepath.Join(apiDir, "handler.go")
	if err := os.WriteFile(handler, []byte("package api\n\nfunc HandlePurchase() {}\n\nfunc helperApi() { HandlePurchase() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ = runCLI(t, 0, "add", "--path", repo)
	if !strings.Contains(out, "pruned") {
		t.Fatalf("rename must prune stale nodes:\n%s", out)
	}
	apiGraph, err := os.ReadFile(filepath.Join(storeRoot, rootKey, "services/api", "graph", "nodes.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(apiGraph), "HandleCheckout") || !strings.Contains(string(apiGraph), "HandlePurchase") {
		t.Fatal("renamed symbol not swapped in the module graph")
	}

	// -- 8: steady state is a no-op ------------------------------------------
	out, _ = runCLI(t, 0, "add", "--path", repo)
	if strings.Contains(out, "pruned") || !strings.Contains(out, "added 0 nodes") {
		t.Fatalf("unchanged re-add must be a no-op:\n%s", out)
	}
}

// TestModuleChildConfigAdoption: plain `init` inside a declared module joins
// the root's world (module_of child config, mirrored store) — never a shadow
// store keyed by basename.
func TestModuleChildConfigAdoption(t *testing.T) {
	repo := fakeMonorepo(t)
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)

	apiDir := filepath.Join(repo, "services/api")
	runCLI(t, 0, "init", "--path", apiDir)
	child, err := project.Load(apiDir)
	if err != nil {
		t.Fatal(err)
	}
	if child.ModuleOf == "" {
		t.Fatalf("init inside a declared module must write module_of, got %+v", child)
	}
	if _, err := os.Stat(filepath.Join(storeRoot, "api")); !os.IsNotExist(err) {
		t.Fatal("adoption must not create a shadow store keyed by basename")
	}
	// Scope still resolves module-first through the child config.
	out, _ := runCLI(t, 0, "query", "HandleCheckout", "--path", apiDir)
	if !strings.Contains(out, "HandleCheckout") {
		t.Fatalf("module query through child config broke:\n%s", out)
	}
}
