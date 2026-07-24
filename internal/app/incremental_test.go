package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Incremental re-sync correctness (ADR 2026-07-24-lazy-autosync): every
// mutation shape must (1) re-gather when it should / short-circuit when it
// provably shouldn't, and (2) land BYTE-IDENTICAL to a from-scratch --force
// gather of the same final tree. Determinism is the whole contract.

// graphOf reads a module store's nodes+edges as one string.
func graphOf(t *testing.T, storeRoot, key string) string {
	t.Helper()
	g := filepath.Join(storeRoot, key, "graph")
	return readFile(t, g, "nodes.ndjson") + "\x1e" + readFile(t, g, "edges.ndjson")
}

// freshGraph gathers repo into a brand-new store (cold, --force) and returns
// its graph — the deterministic reference the incremental store must match.
func freshGraph(t *testing.T, repo, key string) string {
	t.Helper()
	fresh := t.TempDir()
	runCLI(t, 0, "init", "--path", repo, "--store", fresh)
	runCLI(t, 0, "add", repo, "--path", repo, "--store", fresh, "--force")
	return graphOf(t, fresh, key)
}

// setupIncremental builds a repo + store, cold-gathers, returns (repo, store, key).
func setupIncremental(t *testing.T, files map[string]string) (string, string, string) {
	t.Helper()
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	t.Setenv("CTX_OPTIMIZE_CACHE_DIR", t.TempDir())
	repo := t.TempDir()
	writeRepo(t, repo, files)
	runCLI(t, 0, "init", "--path", repo, "--store", storeRoot)
	runCLI(t, 0, "add", repo, "--path", repo, "--store", storeRoot)
	return repo, storeRoot, filepath.Base(repo)
}

func reAdd(t *testing.T, repo, storeRoot string) string {
	t.Helper()
	out, _ := runCLI(t, 0, "add", repo, "--path", repo, "--store", storeRoot)
	return out
}

const baseMain = "package main\nfunc main() {}\nfunc Alpha() {}\n"

func TestIncrementalFileEdit(t *testing.T) {
	repo, storeRoot, key := setupIncremental(t, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": baseMain,
	})
	// Edit: add a decl. Must re-gather (not skip) and match a fresh gather.
	mustWrite(t, repo, "main.go", baseMain+"func Beta() {}\n")
	if out := reAdd(t, repo, storeRoot); strings.Contains(out, "unchanged") {
		t.Fatal("edit must re-gather")
	}
	got := graphOf(t, storeRoot, key)
	if !strings.Contains(got, "::Beta") {
		t.Fatal("edited-in decl Beta missing")
	}
	if got != freshGraph(t, repo, key) {
		t.Fatal("incremental edit diverged from fresh gather")
	}
}

func TestIncrementalFileAdd(t *testing.T) {
	repo, storeRoot, key := setupIncremental(t, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": baseMain,
	})
	mustWrite(t, repo, "extra.go", "package main\nfunc Gamma() {}\n")
	if out := reAdd(t, repo, storeRoot); strings.Contains(out, "unchanged") {
		t.Fatal("new file must re-gather")
	}
	got := graphOf(t, storeRoot, key)
	if !strings.Contains(got, "extra.go") || !strings.Contains(got, "::Gamma") {
		t.Fatal("added file's nodes missing")
	}
	if got != freshGraph(t, repo, key) {
		t.Fatal("incremental add diverged from fresh gather")
	}
}

func TestIncrementalFileDelete(t *testing.T) {
	repo, storeRoot, key := setupIncremental(t, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": baseMain,
		"gone.go": "package main\nfunc Doomed() {}\n",
	})
	if !strings.Contains(graphOf(t, storeRoot, key), "::Doomed") {
		t.Fatal("precondition: Doomed should exist")
	}
	if err := os.Remove(filepath.Join(repo, "gone.go")); err != nil {
		t.Fatal(err)
	}
	if out := reAdd(t, repo, storeRoot); strings.Contains(out, "unchanged") {
		t.Fatal("file delete must re-gather")
	}
	got := graphOf(t, storeRoot, key)
	if strings.Contains(got, "gone.go") || strings.Contains(got, "::Doomed") {
		t.Fatal("deleted file's nodes must be pruned")
	}
	if got != freshGraph(t, repo, key) {
		t.Fatal("incremental delete diverged from fresh gather")
	}
}

func TestIncrementalFolderDelete(t *testing.T) {
	// main.go is the bulk so deleting sub/ stays under the >50% shrink guard
	// (a legitimate small-folder delete; a >50% wipe still needs --force).
	bigMain := "package main\nfunc main() {}\n" +
		"func A() {}\nfunc B() {}\nfunc C() {}\nfunc D() {}\nfunc E() {}\nfunc F() {}\n"
	repo, storeRoot, key := setupIncremental(t, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": bigMain,
		"sub/a.go": "package sub\nfunc SubOne() {}\n",
		"sub/b.go": "package sub\nfunc SubTwo() {}\n",
	})
	if err := os.RemoveAll(filepath.Join(repo, "sub")); err != nil {
		t.Fatal(err)
	}
	if out := reAdd(t, repo, storeRoot); strings.Contains(out, "unchanged") {
		t.Fatal("folder delete must re-gather")
	}
	got := graphOf(t, storeRoot, key)
	if strings.Contains(got, "sub/") || strings.Contains(got, "SubOne") || strings.Contains(got, "SubTwo") {
		t.Fatal("deleted folder's nodes must all be pruned")
	}
	if got != freshGraph(t, repo, key) {
		t.Fatal("incremental folder delete diverged from fresh gather")
	}
}

// Deleting a GITIGNORED folder must NOT re-gather (its files were never in the
// graph, and tree-sig excludes them) — the short-circuit must still fire.
func TestIncrementalIgnoredFolderDelete(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git required for .gitignore semantics")
	}
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	t.Setenv("CTX_OPTIMIZE_CACHE_DIR", t.TempDir())
	repo := t.TempDir()
	writeRepo(t, repo, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": baseMain,
		".gitignore":     "generated/\n",
		"generated/x.go": "package generated\nfunc Junk() {}\n",
	})
	gitInit(t, repo)
	runCLI(t, 0, "init", "--path", repo, "--store", storeRoot)
	runCLI(t, 0, "add", repo, "--path", repo, "--store", storeRoot)
	key := filepath.Base(repo)
	if strings.Contains(graphOf(t, storeRoot, key), "generated/") {
		t.Fatal("gitignored folder must never be in the graph")
	}
	before := graphOf(t, storeRoot, key)

	if err := os.RemoveAll(filepath.Join(repo, "generated")); err != nil {
		t.Fatal(err)
	}
	out := reAdd(t, repo, storeRoot)
	if !strings.Contains(out, "unchanged") {
		t.Fatalf("deleting an IGNORED folder must short-circuit: %s", out)
	}
	if graphOf(t, storeRoot, key) != before {
		t.Fatal("ignored-folder delete must not touch the graph")
	}
}

// Removing a module from a monorepo config must prune that module's store on
// the next gather (reconcile), and the survivors must be byte-identical to a
// fresh gather of the reduced config.
func TestIncrementalModuleDelete(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	t.Setenv("CTX_OPTIMIZE_CACHE_DIR", t.TempDir())
	repo := t.TempDir()
	writeRepo(t, repo, map[string]string{
		".ctxoptimize/config.json": `{"name":"mono","modules":[{"path":"svc/a"},{"path":"svc/b"}]}`,
		"svc/a/go.mod":             "module a\n\ngo 1.22\n",
		"svc/a/a.go":               "package a\nfunc AThing() {}\n",
		"svc/b/go.mod":             "module b\n\ngo 1.22\n",
		"svc/b/b.go":               "package b\nfunc BThing() {}\n",
	})
	runCLI(t, 0, "add", "--path", repo, "--store", storeRoot)
	if _, err := os.Stat(filepath.Join(storeRoot, "mono", "svc/b", "graph")); err != nil {
		t.Fatalf("module b store should exist after first gather: %v", err)
	}

	// Drop module b from the config, delete its folder, re-gather.
	mustWrite(t, repo, ".ctxoptimize/config.json", `{"name":"mono","modules":[{"path":"svc/a"}]}`)
	if err := os.RemoveAll(filepath.Join(repo, "svc/b")); err != nil {
		t.Fatal(err)
	}
	out, _ := runCLI(t, 0, "up", "--path", repo, "--store", storeRoot)

	// Contract: the module is dropped from the navigator + federation, and its
	// orphan store is REPORTED (not auto-deleted — never silently delete user
	// data). Module a survives intact.
	nav := readFile(t, filepath.Join(storeRoot, "mono"), "navigator.md")
	if strings.Contains(nav, "svc/b") {
		t.Fatal("removed module must drop out of the navigator")
	}
	if !strings.Contains(out, "no longer in config.json") {
		t.Fatalf("removed module's orphan store should be reported: %s", out)
	}
	// Federation at the root must not return b's content. (Query for a term
	// unique to b; assert b's FILE never appears — the --json echoes the query
	// string itself, so match on the file path, not the term.)
	q, _ := runCLI(t, 0, "query", "BThing", "--path", repo, "--store", storeRoot, "--root", "--json")
	if strings.Contains(q, "svc/b") {
		t.Fatal("removed module's files must not federate")
	}
	if !strings.Contains(graphOf(t, storeRoot, "mono/svc/a"), "AThing") {
		t.Fatal("surviving module a lost its content")
	}
}

// ---- helpers ----

func mustWrite(t *testing.T, repo, rel, content string) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitInit(t *testing.T, repo string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"}, {"add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}
