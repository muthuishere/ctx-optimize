package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

// fixtureStore builds a small graph through the real store: two code files
// with decls, a doc referencing one file, an imported module, and a call edge.
func fixtureStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir(), "mymod")
	if err != nil {
		t.Fatal(err)
	}
	b := &schema.Batch{
		Producer: "test",
		Nodes: []schema.Node{
			{ID: "pkg/main.go", Label: "main.go", Kind: "file", FileType: "code", Source: "pkg/main.go", Location: "L1"},
			{ID: "pkg/main.go::main", Label: "main()", Kind: "function", FileType: "code", Source: "pkg/main.go", Location: "L5-L20"},
			{ID: "pkg/util.go", Label: "util.go", Kind: "file", FileType: "code", Source: "pkg/util.go", Location: "L1"},
			{ID: "pkg/util.go::Helper", Label: "Helper()", Kind: "function", FileType: "code", Source: "pkg/util.go", Location: "L3-L9"},
			{ID: "docs/design.md", Label: "design.md", Kind: "document", FileType: "document", Source: "docs/design.md", Location: "L1"},
			{ID: "mod://fmt", Label: "fmt", Kind: "module", FileType: "code", Source: "mod://fmt"},
		},
		Edges: []schema.Edge{
			{Source: "pkg/main.go", Target: "pkg/main.go::main", Relation: "contains", Confidence: schema.Extracted},
			{Source: "pkg/util.go", Target: "pkg/util.go::Helper", Relation: "contains", Confidence: schema.Extracted},
			{Source: "pkg/main.go", Target: "mod://fmt", Relation: "imports", Confidence: schema.Extracted},
			{Source: "pkg/main.go::main", Target: "pkg/util.go::Helper", Relation: "calls", Confidence: schema.Inferred},
			{Source: "docs/design.md", Target: "pkg/main.go", Relation: "references", Confidence: schema.Inferred},
		},
	}
	if _, _, err := s.Merge(b); err != nil {
		t.Fatal(err)
	}
	return s
}

func readPage(t *testing.T, s *store.Store, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.Dir, "wiki", name))
	if err != nil {
		t.Fatalf("page %s: %v", name, err)
	}
	return string(data)
}

// index, hub, and file pages exist with the expected facts and relative links.
func TestGeneratePages(t *testing.T) {
	s := fixtureStore(t)
	files, err := Generate(s)
	if err != nil {
		t.Fatal(err)
	}
	// index + 6 hub pages (every node has degree ≥ 1) + 3 file/doc pages.
	if files != 10 {
		t.Fatalf("files = %d, want 10", files)
	}

	idx := readPage(t, s, "index.md")
	for _, want := range []string{
		"# mymod",
		"- nodes: 6", "- edges: 5",
		"| file | 2 |", "| document | 1 |", // nodes by kind
		"| contains | 2 |", "| references | 1 |", // edges by relation
		"(file-main-go.md)", "(file-design-md.md)", // file table links
		"(hub-main-go.md)", // hub table link
	} {
		if !strings.Contains(idx, want) {
			t.Fatalf("index.md missing %q:\n%s", want, idx)
		}
	}

	// Hub page: facts + grouped outgoing/incoming, neighbors linked to their
	// pages (main() has no file page → its hub page is the link target).
	hub := readPage(t, s, "hub-main-go.md")
	for _, want := range []string{
		"# main.go",
		"- kind: file (code)",
		"- producer: test",
		"- degree: 1 in / 2 out",
		"## Outgoing", "### contains (1)", "[main()](hub-main.md)",
		"### imports (1)",
		"## Incoming", "### references (1)", "[design.md](file-design-md.md)",
	} {
		if !strings.Contains(hub, want) {
			t.Fatalf("hub-main-go.md missing %q:\n%s", want, hub)
		}
	}

	// File page: decls with kind+location, imports, incoming references.
	fp := readPage(t, s, "file-main-go.md")
	for _, want := range []string{
		"# main.go",
		"- source: pkg/main.go",
		"## Contains (1)", "(function) L5-L20",
		"## Imports (1)", "fmt",
		"## Referenced by", "### references (1)", "[design.md](file-design-md.md)",
		"[index](index.md)",
	} {
		if !strings.Contains(fp, want) {
			t.Fatalf("file-main-go.md missing %q:\n%s", want, fp)
		}
	}

	// The markdown document gets a file page too.
	if !strings.Contains(readPage(t, s, "file-design-md.md"), "- kind: document") {
		t.Fatal("file-design-md.md missing document facts")
	}
}

// The index carries a Subsystems section (community detection) and it is
// byte-stable across two generations.
func TestIndexSubsystems(t *testing.T) {
	s := fixtureStore(t)
	if _, err := Generate(s); err != nil {
		t.Fatal(err)
	}
	idx := readPage(t, s, "index.md")
	for _, want := range []string{
		"## Subsystems",
		"| subsystem | size | top hubs | dirs |",
		// One connected component of 6 nodes → one community, labelled by
		// its highest-degree member (main.go) + dominant dir (pkg).
		"| main.go (pkg) | 6 |",
		"pkg, docs",
	} {
		if !strings.Contains(idx, want) {
			t.Fatalf("index.md missing %q:\n%s", want, idx)
		}
	}
	if _, err := Generate(s); err != nil {
		t.Fatal(err)
	}
	if again := readPage(t, s, "index.md"); again != idx {
		t.Fatal("Subsystems index not byte-stable across two generations")
	}
}

// Two runs over the same graph must be byte-identical — the wiki is a pure
// function of nodes+edges.
func TestByteIdenticalRegeneration(t *testing.T) {
	s := fixtureStore(t)
	snapshot := func() map[string]string {
		t.Helper()
		if _, err := Generate(s); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(filepath.Join(s.Dir, "wiki"))
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, e := range entries {
			out[e.Name()] = readPage(t, s, e.Name())
		}
		return out
	}
	first, second := snapshot(), snapshot()
	if len(first) != len(second) {
		t.Fatalf("page sets differ: %d vs %d", len(first), len(second))
	}
	for name, body := range first {
		if second[name] != body {
			t.Fatalf("%s not byte-identical across runs", name)
		}
	}
}

// The wiki owns wiki/: stale .md pages from an earlier graph are removed,
// non-markdown files are left alone.
func TestStaleRemoval(t *testing.T) {
	s := fixtureStore(t)
	wikiDir := filepath.Join(s.Dir, "wiki")
	os.WriteFile(filepath.Join(wikiDir, "hub-old.md"), []byte("stale"), 0o644)
	os.WriteFile(filepath.Join(wikiDir, "notes.txt"), []byte("keep"), 0o644)
	if _, err := Generate(s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wikiDir, "hub-old.md")); !os.IsNotExist(err) {
		t.Fatalf("stale page not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wikiDir, "notes.txt")); err != nil {
		t.Fatalf("non-markdown file touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wikiDir, "index.md")); err != nil {
		t.Fatalf("index missing after regen: %v", err)
	}
}

// Same label in two directories → slugs dedupe -2 in sorted-id order.
func TestSlugDedupe(t *testing.T) {
	s, err := store.Open(t.TempDir(), "dupes")
	if err != nil {
		t.Fatal(err)
	}
	b := &schema.Batch{Producer: "test", Nodes: []schema.Node{
		{ID: "a/x.go", Label: "x.go", Kind: "file", FileType: "code", Source: "a/x.go"},
		{ID: "b/x.go", Label: "x.go", Kind: "file", FileType: "code", Source: "b/x.go"},
	}}
	if _, _, err := s.Merge(b); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(s); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readPage(t, s, "file-x-go.md"), "a/x.go") {
		t.Fatal("file-x-go.md should be the id-sorted first node (a/x.go)")
	}
	if !strings.Contains(readPage(t, s, "file-x-go-2.md"), "b/x.go") {
		t.Fatal("file-x-go-2.md should be the second node (b/x.go)")
	}
}

// Per-relation lists on hub pages cap at 30 with an explicit remainder.
func TestRelationCap(t *testing.T) {
	s, err := store.Open(t.TempDir(), "capped")
	if err != nil {
		t.Fatal(err)
	}
	b := &schema.Batch{Producer: "test"}
	b.Nodes = append(b.Nodes, schema.Node{ID: "big", Label: "Big()", Kind: "function", FileType: "code", Source: "big.go"})
	for i := 0; i < 35; i++ {
		id := fmt.Sprintf("caller-%02d", i)
		b.Nodes = append(b.Nodes, schema.Node{ID: id, Label: id, Kind: "function", FileType: "code", Source: id + ".go"})
		b.Edges = append(b.Edges, schema.Edge{Source: id, Target: "big", Relation: "calls", Confidence: schema.Inferred})
	}
	if _, _, err := s.Merge(b); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(s); err != nil {
		t.Fatal(err)
	}
	hub := readPage(t, s, "hub-big.md")
	if !strings.Contains(hub, "### calls (35)") {
		t.Fatalf("relation count missing:\n%s", hub)
	}
	if !strings.Contains(hub, "… 5 more") {
		t.Fatalf("cap remainder missing:\n%s", hub)
	}
	if strings.Contains(hub, "caller-31") { // index 31 is past the 30-item cap
		t.Fatalf("list not capped at %d:\n%s", relationCap, hub)
	}
}
