package boundaries

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
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

// Hermetic: point the machine dirs somewhere empty so a developer's real
// ~/ctxoptimize/boundaries and ~/ctxoptimize/services never leak into a test.
func hermetic(t *testing.T) {
	t.Helper()
	t.Setenv("CTX_OPTIMIZE_BOUNDARIES", t.TempDir())
	t.Setenv("CTX_OPTIMIZE_SERVICES", t.TempDir())
}

func find(b *schema.Batch, id string) *schema.Node {
	for i := range b.Nodes {
		if b.Nodes[i].ID == id {
			return &b.Nodes[i]
		}
	}
	return nil
}

func TestRepoRuleOverridesEmbeddedById(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	// Narrow the embedded env-go rule to nothing: same ID, impossible ext.
	write(t, root, ".ctxoptimize/boundaries.json", `{"version":1,"boundaries":[
	  {"id":"env-go","transport":"config.env","direction":"consumes","scan":"raw",
	   "when":{"ext":[".nope"]},"scan":"raw",
	   "match":[{"re":"never([A-Z]+)","identifier":1}]}]}`)
	write(t, root, "main.go", `package main
func f() { _ = os.Getenv("SHOULD_NOT_APPEAR") }`)
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if find(b, "port:config.env:>SHOULD_NOT_APPEAR") != nil {
		t.Fatal("embedded rule ran despite repo override with the same id")
	}
}

func TestMalformedConfigFailsLoud(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, ".ctxoptimize/boundaries.json", `{"version":1,"boundaries":[
	  {"id":"bad","transport":"network.http","direction":"sideways","scan":"raw","scan":"raw",
	   "match":[{"re":"x","identifier":0}]}]}`)
	if _, err := Extract(root); err == nil {
		t.Fatal("invalid direction accepted silently")
	}
}

// The routes-* rules are the PROVIDES side of D3 — additive port coverage.
// The AST recognizers in internal/extract/code remain the EXTRACTED route
// truth (kind=route + handles edges); these rules ship INFERRED and never
// touch that surface (pinned by the byte-match evidence in the ADR).
