package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Lever 1 (ADR 2026-07-24-lazy-autosync): a re-gather on an unchanged tree
// short-circuits (no extraction), and the skip is byte-identical to a full
// rebuild; a real edit is never skipped. Determinism is the whole point.
func TestGatherShortCircuit(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()
	writeRepo(t, repo, map[string]string{
		"go.mod":  "module ex\n\ngo 1.22\n",
		"main.go": "package main\nfunc main() {}\nfunc helper() {}\n",
	})
	runCLI(t, 0, "init", "--path", repo, "--store", storeRoot)

	// Cold gather records the tree signature.
	out, _ := runCLI(t, 0, "add", repo, "--path", repo, "--store", storeRoot)
	if strings.Contains(out, "unchanged") {
		t.Fatal("first gather must not short-circuit")
	}
	key := filepath.Base(repo)
	graph := filepath.Join(storeRoot, key, "graph")
	cold := readFile(t, graph, "nodes.ndjson") + readFile(t, graph, "edges.ndjson")

	// 0-change re-gather short-circuits, store untouched.
	out, _ = runCLI(t, 0, "add", repo, "--path", repo, "--store", storeRoot)
	if !strings.Contains(out, "unchanged") {
		t.Fatalf("0-change gather must short-circuit: %s", out)
	}
	if got := readFile(t, graph, "nodes.ndjson") + readFile(t, graph, "edges.ndjson"); got != cold {
		t.Fatal("short-circuit changed the store — must be a no-op")
	}

	// --force always rebuilds and lands byte-identical (determinism).
	out, _ = runCLI(t, 0, "add", repo, "--path", repo, "--store", storeRoot, "--force")
	if strings.Contains(out, "unchanged") {
		t.Fatal("--force must never short-circuit")
	}
	if got := readFile(t, graph, "nodes.ndjson") + readFile(t, graph, "edges.ndjson"); got != cold {
		t.Fatal("force rebuild diverged from cold — non-deterministic")
	}

	// A real edit is NOT skipped.
	if err := os.WriteFile(filepath.Join(repo, "main.go"),
		[]byte("package main\nfunc main() {}\nfunc helper() {}\nfunc added() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ = runCLI(t, 0, "add", repo, "--path", repo, "--store", storeRoot)
	if strings.Contains(out, "unchanged") {
		t.Fatalf("edited tree must re-gather, not skip: %s", out)
	}
	if !strings.Contains(readFile(t, graph, "nodes.ndjson"), "added") {
		t.Fatal("new decl must appear after re-gather")
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
