package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/project"
	"github.com/muthuishere/ctx-optimize/internal/version"
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
	cfg.Remote = &project.Remote{Push: "node .ctxoptimize/push.js", Pull: "node .ctxoptimize/pull.js"}
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
	if cfg2.RemoteCommand("push") != "node .ctxoptimize/push.js" {
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
	if len(cfg3.Modules) != 3 || cfg3.RemoteCommand("push") != "node .ctxoptimize/push.js" {
		t.Fatalf("re-scan must restore modules and keep remote: %+v", cfg3)
	}

	// -- 6: re-adoption makes the root residual shrink — and that is a
	// CORRECT gather (ADR 2026-07-19-config-reconciliation): the residual's
	// scope follows the module list, so the count-shrink guard is skipped
	// for it. One plain add converges; the symbol lives in exactly one
	// store afterwards. (Module stores keep the guard — pinned in
	// TestModuleStoreKeepsShrinkGuard.)
	out, errb = runCLI(t, 0, "add", "--path", repo)
	if strings.Contains(out+errb, "refusing to shrink") {
		t.Fatalf("residual shrink after re-adoption must NOT be refused:\n%s%s", out, errb)
	}
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

// TestGlobalInstructionsAndSkills: the machine-global flat keys —
// `instructions` picks which files init touches (CLAUDE|AGENTS|ALL|NONE),
// `skills` picks which dirs install --skills writes. Legacy v0.2.6
// {"agents":{"type":...}} configs still read.
func TestGlobalInstructionsAndSkills(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)

	// Defaults: ALL for both, listed together.
	out, _ := runCLI(t, 0, "config")
	if !strings.Contains(out, "instructions = ALL") || !strings.Contains(out, "skills = ALL") {
		t.Fatalf("default config listing: %s", out)
	}

	// instructions=CLAUDE → init creates only CLAUDE.md.
	runCLI(t, 0, "config", "instructions", "claude")
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, 0, "init", "--path", repo)
	if _, err := os.Stat(filepath.Join(repo, "CLAUDE.md")); err != nil {
		t.Fatal("CLAUDE.md missing with instructions=CLAUDE")
	}
	if _, err := os.Stat(filepath.Join(repo, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("AGENTS.md must not be created with instructions=CLAUDE")
	}

	// instructions=NONE → repo untouched, init says so.
	runCLI(t, 0, "config", "instructions", "NONE")
	repo2 := t.TempDir()
	out, _ = runCLI(t, 0, "init", "--path", repo2)
	if !strings.Contains(out, "NONE") {
		t.Fatalf("init should say instructions are off:\n%s", out)
	}
	for _, fn := range []string{"CLAUDE.md", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(repo2, fn)); !os.IsNotExist(err) {
			t.Fatalf("%s created despite instructions=NONE", fn)
		}
	}

	// Typos refused for both keys; config unchanged.
	_, errb := runCLI(t, 1, "config", "instructions", "CURSOR")
	if !strings.Contains(errb, "CLAUDE, AGENTS, ALL, or NONE") {
		t.Fatalf("bad instructions value: %s", errb)
	}
	_, errb = runCLI(t, 1, "config", "skills", "NONE")
	if !strings.Contains(errb, "CLAUDE, AGENTS, or ALL") {
		t.Fatalf("bad skills value: %s", errb)
	}
	out, _ = runCLI(t, 0, "config", "instructions")
	if strings.TrimSpace(out) != "NONE" {
		t.Fatalf("failed set must not change config: %q", out)
	}

	// skills key round-trips.
	runCLI(t, 0, "config", "skills", "agents")
	out, _ = runCLI(t, 0, "config", "skills")
	if strings.TrimSpace(out) != "AGENTS" {
		t.Fatalf("skills round-trip: %q", out)
	}

	// Legacy v0.2.6 file shape reads as instructions.
	if err := os.WriteFile(filepath.Join(storeRoot, "config.json"),
		[]byte(`{"agents":{"type":"AGENTS"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ = runCLI(t, 0, "config", "instructions")
	if strings.TrimSpace(out) != "AGENTS" {
		t.Fatalf("legacy agents.type alias broken: %q", out)
	}
}

// TestConfigHooksAndProjectLevel: the hooks key validates like the others,
// and --project writes committable overrides that beat global — init obeys
// the project's instructions even when global says otherwise.
func TestConfigHooksAndProjectLevel(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()

	// hooks key round-trips and rejects junk.
	runCLI(t, 0, "config", "hooks", "agents")
	out, _ := runCLI(t, 0, "config", "hooks")
	if strings.TrimSpace(out) != "AGENTS" {
		t.Fatalf("hooks round-trip: %q", out)
	}
	_, errb := runCLI(t, 1, "config", "hooks", "CURSOR")
	if !strings.Contains(errb, "CLAUDE, AGENTS, ALL, or NONE") {
		t.Fatalf("bad hooks value: %s", errb)
	}

	// Global says NONE; the project pins CLAUDE — project must win at init.
	runCLI(t, 0, "config", "instructions", "NONE")
	out, _ = runCLI(t, 0, "config", "instructions", "CLAUDE", "--project", "--path", repo)
	if !strings.Contains(out, "commit it") {
		t.Fatalf("project set should point at the committable file: %s", out)
	}
	var pc map[string]any
	data, err := os.ReadFile(filepath.Join(repo, ".ctxoptimize", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &pc); err != nil || pc["instructions"] != "CLAUDE" {
		t.Fatalf("project config not written: %s (%v)", data, err)
	}
	runCLI(t, 0, "init", "--path", repo)
	if _, err := os.Stat(filepath.Join(repo, "CLAUDE.md")); err != nil {
		t.Fatal("project instructions=CLAUDE must beat global NONE")
	}
	if _, err := os.Stat(filepath.Join(repo, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("AGENTS.md must not appear with project instructions=CLAUDE")
	}

	// Listing from inside the repo shows the winning source.
	out, _ = runCLI(t, 0, "config", "--path", repo)
	if !strings.Contains(out, "instructions = CLAUDE  (project)") || !strings.Contains(out, "hooks = AGENTS  (global)") {
		t.Fatalf("effective listing wrong:\n%s", out)
	}
}

// TestInstallUpdateUninstall: the whole surface lifecycle against a hermetic
// HOME — install writes skills + global rule, update is an exact refresh
// (orphans from older versions die, local edits are restored to bundled
// truth), uninstall needs no flag and removes skills, hooks, and the rule.
func TestInstallUpdateUninstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	skillMd := filepath.Join(home, ".claude", "skills", "ctx-optimize", "SKILL.md")

	// -- install -------------------------------------------------------------
	out, _ := runCLI(t, 0, "install", "--claude")
	if !strings.Contains(out, "global rule: added") {
		t.Fatalf("install must write the global rule:\n%s", out)
	}
	if _, err := os.Stat(skillMd); err != nil {
		t.Fatalf("skill not installed: %v", err)
	}

	// -- update: exact replace -----------------------------------------------
	orphan := filepath.Join(filepath.Dir(skillMd), "references", "removed-in-v2.md")
	if err := os.WriteFile(orphan, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundled, err := os.ReadFile(skillMd)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillMd, []byte("local edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ = runCLI(t, 0, "update", "--claude")
	// Test binaries are dev builds: the binary lane must skip WITHOUT any
	// network, then still refresh the surfaces.
	if !strings.Contains(out, "dev build — self-update skipped") || !strings.Contains(out, "skills + hooks now match this binary") {
		t.Fatalf("dev-build update must skip the binary and refresh surfaces:\n%s", out)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("update left an orphan file from an older version")
	}
	after, err := os.ReadFile(skillMd)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(bundled) {
		t.Fatal("update must restore the bundled SKILL.md exactly")
	}

	// -- uninstall: no flag needed, everything install wrote goes ------------
	out, _ = runCLI(t, 0, "uninstall")
	if !strings.Contains(out, "removed skill:") || !strings.Contains(out, "removed global rule from:") {
		t.Fatalf("uninstall report incomplete:\n%s", out)
	}
	if _, err := os.Stat(filepath.Dir(skillMd)); !os.IsNotExist(err) {
		t.Fatal("skill dir survived uninstall")
	}
	claudeMd, err := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
	if err == nil && strings.Contains(string(claudeMd), "ctx-optimize:global:begin") {
		t.Fatal("global rule survived uninstall")
	}
	// Legacy spelling still accepted.
	runCLI(t, 0, "uninstall", "--skills")
}

// TestUpdateCheck: with a release-tagged version, `update --check` reports
// what's available (against a hermetic API server) and touches nothing —
// no install, no swap, no surfaces.
func TestUpdateCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	t.Setenv("CTX_OPTIMIZE_UPDATE_API", srv.URL)
	orig := version.Version
	version.Version = "0.1.0"
	defer func() { version.Version = orig }()

	out, _ := runCLI(t, 0, "update", "--check")
	if !strings.Contains(out, "v9.9.9 available (current 0.1.0)") {
		t.Fatalf("check must report the available version:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); !os.IsNotExist(err) {
		t.Fatal("--check must not install surfaces")
	}

	// Up to date → says so, then still refreshes surfaces (no --check).
	version.Version = "9.9.9"
	out, _ = runCLI(t, 0, "update", "--claude")
	if !strings.Contains(out, "binary: up to date") || !strings.Contains(out, "skills + hooks now match this binary") {
		t.Fatalf("up-to-date update must refresh surfaces:\n%s", out)
	}
}
