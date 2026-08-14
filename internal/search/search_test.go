package search

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSweepFindsMatchesSortedWithLines(t *testing.T) {
	root := t.TempDir()
	write(t, root, "b.go", "package b\nfunc B() { exec.Command(\"git\") }\n")
	write(t, root, "a.go", "package a\nx := exec.Command(\"npm\")\ny := exec.Command(\"sh\")\n")

	m, err := Run(root, regexp.MustCompile(`exec\.Command\(`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 3 {
		t.Fatalf("want 3 matches, got %d: %v", len(m), m)
	}
	// Deterministic: a.go before b.go, lines ascending.
	if m[0].File != "a.go" || m[0].Line != 2 || m[1].Line != 3 || m[2].File != "b.go" {
		t.Fatalf("order wrong: %v", m)
	}
}

func TestExtAndPathFilters(t *testing.T) {
	root := t.TempDir()
	write(t, root, "x.go", "hit\n")
	write(t, root, "x.ts", "hit\n")
	write(t, root, "sub/y.go", "hit\n")

	re := regexp.MustCompile(`hit`)
	if m, _ := Run(root, re, Options{Exts: []string{".go"}}); len(m) != 2 {
		t.Fatalf("ext filter: want 2, got %d", len(m))
	}
	if m, _ := Run(root, re, Options{Path: "sub/"}); len(m) != 1 || m[0].File != "sub/y.go" {
		t.Fatalf("path filter: got %v", m)
	}
}

// The point of the verb (ADR 1 D4): the sweep sees the SAME file set as the
// extractor — vendored and skip-dir trees never pollute a ground-truth count.
func TestSkipsExtractorSkippedTrees(t *testing.T) {
	root := t.TempDir()
	write(t, root, "real.go", "hit\n")
	write(t, root, "node_modules/dep/index.js", "hit\n")
	write(t, root, "vendor/lib/lib.go", "hit\n")
	write(t, root, "dist/out.js", "hit\n")
	write(t, root, ".hidden/h.go", "hit\n")

	m, err := Run(root, regexp.MustCompile(`hit`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m[0].File != "real.go" {
		t.Fatalf("want only real.go, got %v", m)
	}
}

func TestBinaryFilesSkipped(t *testing.T) {
	root := t.TempDir()
	write(t, root, "bin.dat", "hit\x00hit\n")
	write(t, root, "ok.txt", "hit\n")
	m, err := Run(root, regexp.MustCompile(`hit`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m[0].File != "ok.txt" {
		t.Fatalf("binary leaked into sweep: %v", m)
	}
}
