// Gates for ADR 2026-07-19-config-reconciliation (narrowed scope: the
// add/shrink-guard fix and the up reconcile). Each test reproduces one
// failure from the volentis stress transcript.
package app

import (
	"bytes"
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

// A hand-written .ctxoptimize/ carrying ONLY config.json: `up` fills in the
// missing inert templates and NEVER overwrites anything the user brought
// (ADR 2026-07-19-up-progress-and-scaffold).
func TestUpScaffoldsMissingTemplatesWithoutOverwriting(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	if err := os.WriteFile(filepath.Join(repo, "pay.go"), []byte("package pay\n\nfunc Charge() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Only config.json — hand-written, with a custom store name.
	cfgDir := filepath.Join(repo, ".ctxoptimize")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const myCfg = `{"name":"my-custom-store"}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(myCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// One sample pre-existing AND user-edited — must survive untouched.
	editedPush := "// my own transport, hands off\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "push.js.sample"), []byte(editedPush), 0o644); err != nil {
		t.Fatal(err)
	}

	runCLI(t, 0, "up", "--path", repo)

	// Every template now present.
	for _, rel := range []string{
		"adapters/example.js.sample", "push.js.sample", "pull.js.sample",
		"remote.example.md", "instructions.md", "config.json",
	} {
		if _, err := os.Stat(filepath.Join(cfgDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("template not scaffolded: %s (%v)", rel, err)
		}
	}
	// NOTHING the user brought was overwritten.
	if got, _ := os.ReadFile(filepath.Join(cfgDir, "config.json")); string(got) != myCfg {
		t.Fatalf("config.json was modified: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(cfgDir, "push.js.sample")); string(got) != editedPush {
		t.Fatalf("edited sample was overwritten: %q", got)
	}
	// The custom store name was honored.
	if _, err := os.Stat(filepath.Join(os.Getenv("CTX_OPTIMIZE_STORE"), "my-custom-store", "graph")); err != nil {
		t.Fatalf("custom store name not honored: %v", err)
	}
	// Idempotent: a second up creates nothing new and still overwrites nothing.
	out, _ := runCLI(t, 0, "up", "--path", repo)
	if strings.Contains(out, "scaffolded") {
		t.Fatalf("second up must not re-scaffold:\n%s", out)
	}
	if got, _ := os.ReadFile(filepath.Join(cfgDir, "push.js.sample")); string(got) != editedPush {
		t.Fatalf("second up overwrote the edited sample: %q", got)
	}
}

// Fan-out emits live progress ticks (stderr) while stdout stays the ordered,
// deterministic result output.
func TestFanOutEmitsProgress(t *testing.T) {
	repo := fakeMonorepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)

	var prog bytes.Buffer
	old := progressOut
	progressOut = &prog
	defer func() { progressOut = old }()

	out, _ := runCLI(t, 0, "add", repo, "--path", repo)
	got := prog.String()
	if !strings.Contains(got, "gathering ") || !strings.Contains(got, "modules (jobs=") {
		t.Fatalf("missing the fan-out header:\n%s", got)
	}
	if !strings.Contains(got, "[1/") {
		t.Fatalf("missing per-module progress ticks:\n%s", got)
	}
	// Progress must NOT pollute stdout (determinism + clean pipes).
	if strings.Contains(out, "[1/") || strings.Contains(out, "gathering ") {
		t.Fatalf("progress leaked into stdout:\n%s", out)
	}
}

// merge reaches nested module stores (ADR 2026-07-19-merge-nested-module-keys):
// by module DIR path and by store-relative key — the two forms a monorepo
// user naturally types. Pre-fix both failed with `no module "api"`.
func TestMergeReachesNestedModuleStores(t *testing.T) {
	repo := fakeMonorepo(t)
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", repo, "--path", repo)
	rootKey := filepath.Base(repo)

	// By dir path (scope resolution).
	out, _ := runCLI(t, 0, "merge",
		filepath.Join(repo, "services", "api"),
		filepath.Join(repo, "services", "worker"),
		"--into", "everything", "--path", repo)
	if !strings.Contains(out, "everything") {
		t.Fatalf("merge by dir path failed:\n%s", out)
	}
	q, _ := runCLI(t, 0, "query", "RunPayrollJob", "--path", repo, "--store", storeRoot)
	_ = q // module query still fine; now check the merged store directly
	data, err := os.ReadFile(filepath.Join(storeRoot, "everything", "graph", "nodes.ndjson"))
	if err != nil || !strings.Contains(string(data), "RunPayrollJob") || !strings.Contains(string(data), "HandleCheckout") {
		t.Fatalf("merged store missing module symbols: %v", err)
	}

	// By store-relative key (slashes preserved).
	out, _ = runCLI(t, 0, "merge", rootKey+"/services/api", "--into", "apionly", "--path", repo)
	if !strings.Contains(out, "apionly") {
		t.Fatalf("merge by store-relative key failed:\n%s", out)
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
