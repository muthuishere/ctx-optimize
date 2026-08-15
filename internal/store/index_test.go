package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// ADR 2026-08-05-query-at-scale. The index exists to make lookups fast; these
// tests exist to prove it never makes them WRONG. Equivalence is the gate —
// speed without it is not shippable.

func seed(t *testing.T, nodes []schema.Node, edges []schema.Edge) *Store {
	t.Helper()
	s, err := Open(t.TempDir(), "idx")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Merge(&schema.Batch{Producer: "test", Nodes: nodes, Edges: edges}); err != nil {
		t.Fatal(err)
	}
	if err := s.BuildIndex(); err != nil {
		t.Fatal(err)
	}
	return s
}

// sameNode compares the identity + citation fields. schema.Node carries a map,
// so it is not directly comparable; these are the fields an answer is built
// from, and a difference in any of them is a changed answer.
func sameNode(a, b schema.Node) bool {
	return a.ID == b.ID && a.Label == b.Label && a.Kind == b.Kind &&
		a.Source == b.Source && a.Location == b.Location && a.FileType == b.FileType
}

func n(id, label string) schema.Node {
	return schema.Node{ID: id, Label: label, Kind: "function", FileType: "code", Source: "a.go", Location: "L1"}
}

// THE gate: for every label in the store, the indexed lookup must return
// exactly what a full scan returns — same count, same records, same order.
// This is the check that caught the escape bug in the spike (76 mismatches in
// 20,031 labels); it stays permanent.
func TestIndexLookupEqualsFullScan(t *testing.T) {
	nodes := []schema.Node{
		n("a", "Alpha"), n("b", "Beta"), n("c", "Alpha"), // duplicate label
		n("d", "with\ttab"),                 // tab: breaks a naive line format
		n("e", `quote"inside`),              // escaped quote: truncates a naive scanner
		n("f", "unicode<URL"),               // \u escape: never decoded by a naive scanner
		n("g", "Zeta"), n("h", "with\ttab"), // duplicate of the tab label
	}
	s := seed(t, nodes, nil)

	all, err := s.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, nd := range all {
		seen[nd.Label] = true
	}
	if len(seen) == 0 {
		t.Fatal("setup: no labels")
	}

	for label := range seen {
		var want []schema.Node
		for _, nd := range all {
			// EqualFold: NodesByLabel is case-insensitive by contract, matching
			// analyze.ResolveVia. The ground truth must use the same rule or the
			// test pins the wrong thing.
			if strings.EqualFold(nd.Label, label) {
				want = append(want, nd)
			}
		}
		got, err := s.NodesByLabel(label)
		if err != nil {
			t.Fatalf("NodesByLabel(%q): %v", label, err)
		}
		if len(got) != len(want) {
			t.Errorf("label %q: indexed returned %d, full scan has %d", label, len(got), len(want))
			continue
		}
		for i := range got {
			if !sameNode(got[i], want[i]) {
				t.Errorf("label %q [%d]: indexed %+v != scan %+v", label, i, got[i], want[i])
			}
		}
	}
}

// Sets, never first hits. 20.3% of linux records share a label; returning one
// of them as the answer is an under-report that reads as complete.
func TestIndexReturnsEveryMatchNotTheFirst(t *testing.T) {
	s := seed(t, []schema.Node{n("a", "Dup"), n("b", "Dup"), n("c", "Dup")}, nil)
	got, err := s.NodesByLabel("Dup")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected all 3 nodes under a shared label, got %d — a first-hit index silently drops callers", len(got))
	}
}

// The escape bug, pinned. A naive extractor stops at the escaped quote or
// returns encoded bytes; either way the key is wrong and the symbol becomes
// unfindable — silently.
func TestIndexHandlesJSONEscapesInLabels(t *testing.T) {
	labels := []string{"with\ttab", `quote"inside`, "unicode<URL", "back\\slash", "new\nline"}
	var nodes []schema.Node
	for i, l := range labels {
		nodes = append(nodes, n(string(rune('a'+i)), l))
	}
	s := seed(t, nodes, nil)
	for _, l := range labels {
		got, err := s.NodesByLabel(l)
		if err != nil {
			t.Fatalf("NodesByLabel(%q): %v", l, err)
		}
		if len(got) != 1 {
			t.Errorf("label %q: got %d nodes, want 1 — escape handling is broken and the symbol is unfindable", l, len(got))
			continue
		}
		if got[0].Label != l {
			t.Errorf("label %q: round-tripped as %q", l, got[0].Label)
		}
	}
}

// Fail safe. A stale index must never answer from a previous tree — it must be
// ignored so the caller falls back to the scan. This is the stale-wiki lesson
// with higher stakes: we may be slow, never wrong.
func TestStaleIndexIsIgnoredNotTrusted(t *testing.T) {
	s := seed(t, []schema.Node{n("a", "Alpha")}, nil)
	if !s.IndexCurrent() {
		t.Fatal("setup: index should be current")
	}

	// Rewrite the graph WITHOUT rebuilding the index — exactly what a gather
	// would do if it forgot, or what an interrupted run leaves behind.
	time.Sleep(10 * time.Millisecond)
	if _, _, err := s.Replace(&schema.Batch{Producer: "test", Nodes: []schema.Node{n("z", "Zulu")}}, true); err != nil {
		t.Fatal(err)
	}
	if s.IndexCurrent() {
		t.Error("index reports current after the graph changed under it")
	}
	// The new node must be findable despite the stale index, via fallback.
	got, err := s.NodesByLabel("Zulu")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("stale index hid a node that exists: got %d, want 1", len(got))
	}
}

// A corrupt or truncated index is a missing index, not a wrong answer.
func TestCorruptIndexFallsBack(t *testing.T) {
	s := seed(t, []schema.Node{n("a", "Alpha")}, nil)
	if err := os.WriteFile(filepath.Join(s.indexDir(), labelsIndex), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.NodesByLabel("Alpha")
	if err != nil {
		t.Fatalf("corrupt index must fall back, not error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("corrupt index lost a node: got %d, want 1", len(got))
	}
}

// No index at all is the normal state for a store gathered by an older binary.
func TestMissingIndexFallsBack(t *testing.T) {
	s, err := Open(t.TempDir(), "noidx")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Merge(&schema.Batch{Producer: "test", Nodes: []schema.Node{n("a", "Alpha")}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.NodesByLabel("Alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("no index: got %d, want 1", len(got))
	}
}

func TestEdgeLookupEqualsFullScan(t *testing.T) {
	nodes := []schema.Node{n("a", "A"), n("b", "B"), n("c", "C")}
	edges := []schema.Edge{
		{Source: "a", Target: "b", Relation: "calls", Confidence: "EXTRACTED"},
		{Source: "a", Target: "c", Relation: "calls", Confidence: "EXTRACTED"},
		{Source: "b", Target: "c", Relation: "calls", Confidence: "INFERRED"},
	}
	s := seed(t, nodes, edges)

	all, err := s.Edges()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c", "missing"} {
		var wantFrom, wantTo []schema.Edge
		for _, e := range all {
			if e.Source == id {
				wantFrom = append(wantFrom, e)
			}
			if e.Target == id {
				wantTo = append(wantTo, e)
			}
		}
		gotFrom, err := s.EdgesFrom(id)
		if err != nil {
			t.Fatal(err)
		}
		gotTo, err := s.EdgesTo(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(gotFrom) != len(wantFrom) {
			t.Errorf("EdgesFrom(%q): got %d, scan has %d", id, len(gotFrom), len(wantFrom))
		}
		if len(gotTo) != len(wantTo) {
			t.Errorf("EdgesTo(%q): got %d, scan has %d", id, len(gotTo), len(wantTo))
		}
	}
}

// An absent label must be absent, not a nearby one — binary search must not
// land on a neighbour.
func TestIndexMissDoesNotReturnNeighbour(t *testing.T) {
	s := seed(t, []schema.Node{n("a", "Alpha"), n("b", "Beta"), n("c", "Gamma")}, nil)
	for _, miss := range []string{"Al", "Alphb", "", "Zzz", "Bet"} {
		got, err := s.NodesByLabel(miss)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("lookup of absent label %q returned %d nodes (%+v)", miss, len(got), got)
		}
	}
}
