package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir(), "test-module")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func batch(producer string, nodeIDs ...string) *schema.Batch {
	b := &schema.Batch{Producer: producer}
	for _, id := range nodeIDs {
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: id, Kind: "function", FileType: "code", Source: "a.go",
		})
	}
	return b
}

func TestMergeUpsertsAndDedupes(t *testing.T) {
	s := testStore(t)
	b := batch("p1", "a", "b")
	b.Edges = []schema.Edge{{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Extracted}}
	na, ea, err := s.Merge(b)
	if err != nil {
		t.Fatal(err)
	}
	if na != 2 || ea != 1 {
		t.Fatalf("first merge: got %d nodes %d edges added", na, ea)
	}
	// Re-merging the identical batch adds nothing (idempotent).
	na, ea, err = s.Merge(b)
	if err != nil {
		t.Fatal(err)
	}
	if na != 0 || ea != 0 {
		t.Fatalf("re-merge should be idempotent, got %d/%d added", na, ea)
	}
	nodes, err := s.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(nodes))
	}
	if nodes[0].Metadata["producer"] != "p1" {
		t.Fatalf("provenance tag missing: %v", nodes[0].Metadata)
	}
}

func TestMergeRejectsInvalid(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.Merge(&schema.Batch{Producer: ""}); err == nil {
		t.Fatal("invalid batch accepted — the door must fail closed")
	}
}

func TestManifestTracksContent(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.Merge(batch("p1", "a")); err != nil {
		t.Fatal(err)
	}
	m1, err := s.UpdateManifest()
	if err != nil {
		t.Fatal(err)
	}
	h1 := m1.Files["graph/nodes.ndjson"].Hash
	if h1 == "" {
		t.Fatal("nodes.ndjson not in manifest")
	}
	if _, _, err := s.Merge(batch("p1", "b")); err != nil {
		t.Fatal(err)
	}
	m2, err := s.UpdateManifest()
	if err != nil {
		t.Fatal(err)
	}
	if m2.Files["graph/nodes.ndjson"].Hash == h1 {
		t.Fatal("content changed but hash did not")
	}
}

func TestModuleKeyIsBasename(t *testing.T) {
	k, err := ModuleKey("/Users/x/proj")
	if err != nil {
		t.Fatal(err)
	}
	if k != "proj" {
		t.Fatalf("got %q", k)
	}
}

func TestSanitizeKey(t *testing.T) {
	if got := SanitizeKey("my module/v2"); got != "my-module-v2" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizeKey("..."); got != "" {
		t.Fatalf("dots-only should sanitize to empty, got %q", got)
	}
}

func TestRootPrecedence(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_STORE", "/env/store")
	r, err := Root("")
	if err != nil {
		t.Fatal(err)
	}
	if r != "/env/store" {
		t.Fatalf("env should win over default, got %q", r)
	}
	r, _ = Root("/flag/store")
	if r != "/flag/store" {
		t.Fatalf("flag should win over env, got %q", r)
	}
}

func TestConfigRoundtrip(t *testing.T) {
	s := testStore(t)
	if err := s.SaveConfig(&Config{Remote: "file:///tmp/r"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if c.Remote != "file:///tmp/r" {
		t.Fatalf("got %q", c.Remote)
	}
	// config.json must be excluded from the manifest (machine-local).
	m, err := s.UpdateManifest()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Files["config.json"]; ok {
		t.Fatal("config.json must not be in the manifest")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "hooks")); err != nil {
		t.Fatal("hooks/ dir must exist in layout")
	}
}

// Replace: producer-scoped truth — stale nodes pruned, other producers kept,
// catastrophic shrink refused without force.
func TestReplacePrunesAndGuards(t *testing.T) {
	s, err := Open(t.TempDir(), "m")
	if err != nil {
		t.Fatal(err)
	}
	node := func(id string) schema.Node {
		return schema.Node{ID: id, Label: id, Kind: "section", FileType: "doc", Source: id}
	}
	// producer A: 4 nodes; producer B: 1 node.
	if _, _, err := s.Merge(&schema.Batch{Producer: "A", Nodes: []schema.Node{node("a1"), node("a2"), node("a3"), node("a4")}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Merge(&schema.Batch{Producer: "B", Nodes: []schema.Node{node("b1")}}); err != nil {
		t.Fatal(err)
	}

	// Re-gather A with 3 nodes (one file deleted) → prunes a4, keeps B.
	added, pruned, err := s.Replace(&schema.Batch{Producer: "A", Nodes: []schema.Node{node("a1"), node("a2"), node("a5")}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 || pruned != 2 { // a5 new; a3+a4 gone
		t.Fatalf("added %d pruned %d", added, pruned)
	}
	nodes, _ := s.Nodes()
	ids := map[string]bool{}
	for _, n := range nodes {
		ids[n.ID] = true
	}
	if !ids["b1"] || ids["a4"] || !ids["a5"] {
		t.Fatalf("wrong survivors: %v", ids)
	}

	// Shrink guard: 3 → 1 nodes (<50%) refused without force.
	if _, _, err := s.Replace(&schema.Batch{Producer: "A", Nodes: []schema.Node{node("a1")}}, false); err == nil {
		t.Fatal("expected shrink guard")
	}
	if _, _, err := s.Replace(&schema.Batch{Producer: "A", Nodes: []schema.Node{node("a1")}}, true); err != nil {
		t.Fatalf("force should override: %v", err)
	}
}

// A --json upsert re-asserting another producer's artifact must not re-own
// it — otherwise the upserter's next Replace prunes the original's data.
func TestMergeDoesNotStealProvenance(t *testing.T) {
	s, err := Open(t.TempDir(), "m")
	if err != nil {
		t.Fatal(err)
	}
	n := schema.Node{ID: "x", Label: "x", Kind: "k", FileType: "f", Source: "x"}
	e := schema.Edge{Source: "x", Target: "x", Relation: "self", Confidence: "EXTRACTED"}
	if _, _, err := s.Merge(&schema.Batch{Producer: "code", Nodes: []schema.Node{n}, Edges: []schema.Edge{e}}); err != nil {
		t.Fatal(err)
	}
	// adapter re-asserts the same node+edge…
	if _, _, err := s.Merge(&schema.Batch{Producer: "adapter", Nodes: []schema.Node{n}, Edges: []schema.Edge{e}}); err != nil {
		t.Fatal(err)
	}
	// …then Replaces with an empty world: code's artifacts must survive.
	if _, _, err := s.Replace(&schema.Batch{Producer: "adapter", Nodes: []schema.Node{{ID: "a", Label: "a", Kind: "k", FileType: "f", Source: "a"}}}, true); err != nil {
		t.Fatal(err)
	}
	nodes, _ := s.Nodes()
	edges, _ := s.Edges()
	foundN, foundE := false, false
	for _, x := range nodes {
		if x.ID == "x" && x.Metadata["producer"] == "code" {
			foundN = true
		}
	}
	for _, x := range edges {
		if x.Source == "x" && x.Metadata["producer"] == "code" {
			foundE = true
		}
	}
	if !foundN || !foundE {
		t.Fatalf("provenance stolen: node kept=%v edge kept=%v", foundN, foundE)
	}
}
