package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupInstalledRepo: a single-project repo with a gathered store and the
// committed .ctxoptimize/ + pointer blocks — the state `remove` tears down.
func setupInstalledRepo(t *testing.T) (repo, storeRoot string) {
	t.Helper()
	repo = t.TempDir()
	storeRoot = t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	writeFiles(t, repo, map[string]string{"main.go": "package main\n\nfunc Boot() {}\n"})
	runCLI(t, 0, "init", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)
	return repo, storeRoot
}

func storeDirOf(t *testing.T, storeRoot, repo string) string {
	t.Helper()
	return filepath.Join(storeRoot, filepath.Base(repo))
}

func TestRemoveDryRunTouchesNothing(t *testing.T) {
	repo, storeRoot := setupInstalledRepo(t)
	sd := storeDirOf(t, storeRoot, repo)

	out, _ := runCLI(t, 0, "remove", "--path", repo) // no --yes
	if !strings.Contains(out, "dry run") {
		t.Fatalf("remove without --yes must be a dry run:\n%s", out)
	}
	if _, err := os.Stat(sd); err != nil {
		t.Fatalf("dry run deleted the store: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".ctxoptimize", "config.json")); err != nil {
		t.Fatalf("dry run deleted the config: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "CLAUDE.md")); !strings.Contains(string(data), "ctx-optimize") {
		t.Fatal("dry run stripped the pointer block")
	}
}

func TestRemoveYesDeletesStoreAndPointerButKeepsConfig(t *testing.T) {
	repo, storeRoot := setupInstalledRepo(t)
	sd := storeDirOf(t, storeRoot, repo)

	out, _ := runCLI(t, 0, "remove", "--yes", "--path", repo)
	if !strings.Contains(out, "removed store data") {
		t.Fatalf("remove --yes should delete store data:\n%s", out)
	}
	if _, err := os.Stat(sd); !os.IsNotExist(err) {
		t.Fatalf("store data still present after remove --yes")
	}
	// Pointer block gone from CLAUDE.md.
	if data, _ := os.ReadFile(filepath.Join(repo, "CLAUDE.md")); strings.Contains(string(data), "ctx-optimize:begin") {
		t.Fatalf("pointer block not stripped:\n%s", data)
	}
	// Committed config KEPT by default — "don't delete unnecessary".
	if _, err := os.Stat(filepath.Join(repo, ".ctxoptimize", "config.json")); err != nil {
		t.Fatalf("remove without --config must KEEP committed .ctxoptimize/: %v", err)
	}
}

func TestRemoveConfigFlagDeletesCommittedDir(t *testing.T) {
	repo, _ := setupInstalledRepo(t)
	runCLI(t, 0, "remove", "--yes", "--config", "--path", repo)
	if _, err := os.Stat(filepath.Join(repo, ".ctxoptimize")); !os.IsNotExist(err) {
		t.Fatalf("remove --config must delete .ctxoptimize/")
	}
}

// The pointer strip must preserve the user's OWN content in the same file.
func TestRemovePreservesSurroundingContent(t *testing.T) {
	repo, _ := setupInstalledRepo(t)
	p := filepath.Join(repo, "CLAUDE.md")
	data, _ := os.ReadFile(p)
	// Prepend and append the user's own guidance around our block.
	custom := "# My project rules\n\nAlways run gofmt.\n\n" + string(data) + "\n## Footer note\n"
	if err := os.WriteFile(p, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, 0, "remove", "--yes", "--path", repo)
	got, _ := os.ReadFile(p)
	s := string(got)
	if strings.Contains(s, "ctx-optimize:begin") {
		t.Fatalf("block not removed:\n%s", s)
	}
	if !strings.Contains(s, "My project rules") || !strings.Contains(s, "Always run gofmt") ||
		!strings.Contains(s, "Footer note") {
		t.Fatalf("user content was damaged:\n%s", s)
	}
}

// Corrupted markers (a lone begin, no end) must be LEFT UNTOUCHED with a
// warning — never guess boundaries and eat the user's text.
func TestRemoveLeavesCorruptedMarkersUntouched(t *testing.T) {
	repo, _ := setupInstalledRepo(t)
	p := filepath.Join(repo, "AGENTS.md")
	corrupt := "# Notes\n\n<!-- ctx-optimize:begin -->\nsome half-deleted block\n\nimportant user text\n"
	if err := os.WriteFile(p, []byte(corrupt), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := runCLI(t, 0, "remove", "--yes", "--path", repo)
	if !strings.Contains(out, "warning:") || !strings.Contains(out, "damaged") {
		t.Fatalf("corrupted markers should warn:\n%s", out)
	}
	got, _ := os.ReadFile(p)
	if string(got) != corrupt {
		t.Fatalf("corrupted-marker file must be left byte-for-byte untouched, got:\n%s", got)
	}
}
