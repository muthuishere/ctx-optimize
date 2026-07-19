// Gates for ADR 2026-07-19-config-reconciliation (narrowed scope: the
// add/shrink-guard fix and the up reconcile). Each test reproduces one
// failure from the volentis stress transcript.
package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// (a) The volentis fossil: a whole-tree root store predating the module
// split. Declaring modules and re-adding shrinks the residual massively —
// that gather must SUCCEED without --force (the old guard refused
// 206717→3018 forever).
func TestResidualRegatherSkipsShrinkGuard(t *testing.T) {
	repo := fakeMonorepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())

	// Whole-tree gather first: plain init (no modules), root store = everything.
	runCLI(t, 0, "init", "--path", repo)
	runCLI(t, 0, "add", repo, "--path", repo)

	// Now the split: scan writes modules[], add fans out. The residual
	// (README + go.work only) is a tiny fraction of the whole-tree store —
	// pre-fix this tripped "refusing to shrink producer" on the root.
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	out, _ := runCLI(t, 0, "add", repo, "--path", repo)
	if strings.Contains(out, "refusing to shrink") {
		t.Fatalf("residual re-gather must not trip the shrink guard:\n%s", out)
	}
	if !strings.Contains(out, "== navigator") {
		t.Fatalf("fan-out did not complete:\n%s", out)
	}
}

// (b) Module stores KEEP the guard: gutting a module's code and re-adding
// still refuses the >50% shrink until --force.
func TestModuleStoreKeepsShrinkGuard(t *testing.T) {
	repo := fakeMonorepo(t)
	// Beef the module up so the later gutting is an unambiguous >50% drop.
	extra := "package api\n\nfunc A1() {}\n\nfunc A2() {}\n\nfunc A3() {}\n\nfunc A4() {}\n\nfunc A5() {}\n"
	if err := os.WriteFile(filepath.Join(repo, "services", "api", "extra.go"), []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", repo, "--path", repo)

	// Gut services/api: delete shared.go + extra.go, shrink handler.go.
	if err := os.Remove(filepath.Join(repo, "services", "api", "shared.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "services", "api", "extra.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "services", "api", "handler.go"), []byte("package api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	apiDir := filepath.Join(repo, "services", "api")
	out, errOut := runCLI(t, 1, "add", apiDir, "--path", apiDir)
	if !strings.Contains(out+errOut, "refusing to shrink") {
		t.Fatalf("module store must keep the shrink guard:\n%s%s", out, errOut)
	}
	// --force applies the real deletion.
	out, _ = runCLI(t, 0, "add", apiDir, "--path", apiDir, "--force")
	if strings.Contains(out, "refusing to shrink") {
		t.Fatalf("--force must clear the module guard:\n%s", out)
	}
}

// (d) Broken/empty root + populated module stores: up re-gathers ONLY the
// residual — never the full fan-out (the transcript rebuilt all 19 modules
// twice because up keyed off the root node count).
func TestUpRegathersOnlyMissingResidual(t *testing.T) {
	repo := fakeMonorepo(t)
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", repo, "--path", repo)

	// Break the root store only: empty its graph, keep module stores.
	rootGraph := filepath.Join(storeRoot, filepath.Base(repo), "graph")
	for _, f := range []string{"nodes.ndjson", "edges.ndjson"} {
		if err := os.WriteFile(filepath.Join(rootGraph, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out, _ := runCLI(t, 0, "up", "--path", repo)
	if !strings.Contains(out, "declared stores missing — gathering only those") {
		t.Fatalf("up must reconcile per module, got:\n%s", out)
	}
	if !strings.Contains(out, "== .\n") {
		t.Fatalf("up must re-gather the residual:\n%s", out)
	}
	if strings.Contains(out, "== services/api") || strings.Contains(out, "gathering from source") {
		t.Fatalf("up must NOT re-gather populated modules or full-rebuild:\n%s", out)
	}
}

// (e) A module added to config.json (no commit — no git at all here) is a
// missing store on the next up, and up gathers EXACTLY it.
func TestUpGathersNewlyDeclaredModule(t *testing.T) {
	repo := fakeMonorepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", repo, "--path", repo)

	// New module appears on disk and in config (uncommitted edit).
	for p, body := range map[string]string{
		"services/newmod/go.mod":    "module acme/newmod\n",
		"services/newmod/newmod.go": "package newmod\n\nfunc BrandNewEntry() {}\n",
	} {
		full := filepath.Join(repo, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(repo, ".ctxoptimize", "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), `"modules": [`, `"modules": [
    {"path": "services/newmod"},`, 1)
	if edited == string(data) {
		t.Fatalf("could not inject module into config:\n%s", data)
	}
	if err := os.WriteFile(cfgPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := runCLI(t, 0, "up", "--path", repo)
	if !strings.Contains(out, "== services/newmod") {
		t.Fatalf("up must gather the newly declared module:\n%s", out)
	}
	if strings.Contains(out, "== services/api") || strings.Contains(out, "gathering from source") {
		t.Fatalf("up must gather ONLY the new module:\n%s", out)
	}
	// It is queryable from the root afterwards (navigator refreshed).
	q, _ := runCLI(t, 0, "query", "BrandNewEntry", "--path", repo)
	if !strings.Contains(q, "BrandNewEntry") {
		t.Fatalf("new module not federated after up:\n%s", q)
	}
}
