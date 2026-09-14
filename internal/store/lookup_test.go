package store

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// A session must answer exactly what the per-call lookups answer. The fixture
// includes a hub with thousands of callers: its edges-by-target line runs to
// tens of kilobytes, far past readLineAround's first probe window, so it
// exercises the window doubling on the lookup of the hub AND on keys that sort
// right before and after it (probes that land inside the long line).
func TestLookupMatchesPerCallLookups(t *testing.T) {
	var nodes []schema.Node
	var edges []schema.Edge
	nodes = append(nodes,
		schema.Node{ID: "hub", Label: "Hub", Kind: "function", FileType: "code", Source: "h.go", Location: "L1"},
		schema.Node{ID: "hua", Label: "Before", Kind: "function", FileType: "code", Source: "h.go", Location: "L2"},
		schema.Node{ID: "huc", Label: "After", Kind: "function", FileType: "code", Source: "h.go", Location: "L3"},
	)
	for i := 0; i < 3000; i++ {
		id := fmt.Sprintf("caller/%04d", i)
		nodes = append(nodes, schema.Node{ID: id, Label: fmt.Sprintf("C%d", i), Kind: "function", FileType: "code", Source: "c.go", Location: fmt.Sprintf("L%d", i)})
		edges = append(edges, schema.Edge{Source: id, Target: "hub", Relation: "calls", Confidence: "EXTRACTED"})
	}
	edges = append(edges,
		schema.Edge{Source: "hub", Target: "hua", Relation: "calls", Confidence: "EXTRACTED"},
		schema.Edge{Source: "hub", Target: "huc", Relation: "calls", Confidence: schema.Ambiguous},
		schema.Edge{Source: "hua", Target: "hua", Relation: "calls", Confidence: "EXTRACTED"},
	)
	s := seed(t, nodes, edges)
	lk, ok := s.OpenLookup()
	if !ok {
		t.Fatal("OpenLookup refused a freshly indexed store")
	}
	defer lk.Close()

	ids := []string{"hub", "hua", "huc", "caller/0000", "caller/2999", "missing", "hu", "hub0"}
	for _, n := range nodes[:50] {
		ids = append(ids, n.ID)
	}
	for _, id := range ids {
		wantN, err := s.NodeByID(id)
		if err != nil {
			t.Fatal(err)
		}
		gotN, err := lk.NodeByID(id)
		if err != nil {
			t.Fatal(err)
		}
		if (wantN == nil) != (gotN == nil) || (wantN != nil && !sameNode(*wantN, *gotN)) {
			t.Errorf("NodeByID(%q): per-call %v, session %v", id, wantN, gotN)
		}
		wantE, err := s.EdgesTo(id)
		if err != nil {
			t.Fatal(err)
		}
		gotE, err := lk.EdgesTo(id)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(wantE, gotE) {
			t.Errorf("EdgesTo(%q): per-call %d edges, session %d", id, len(wantE), len(gotE))
		}
	}
	// Ground truth, not just session-vs-per-call: both used to share a bug where
	// readLineAround returned the whole index body as one line, handing the
	// hub's 3,000 callers to "hua" and leaving "hub" with none.
	for id, want := range map[string]int{"hub": 3000, "hua": 2, "huc": 1} {
		if es, _ := lk.EdgesTo(id); len(es) != want {
			t.Errorf("session EdgesTo(%q) = %d edges, want %d", id, len(es), want)
		}
		if es, _ := s.EdgesTo(id); len(es) != want {
			t.Errorf("per-call EdgesTo(%q) = %d edges, want %d", id, len(es), want)
		}
	}
}

// A session must never be opened over an index that no longer matches the
// graph — callers rely on ok=false to take the full-scan path.
func TestLookupRefusesAStaleIndex(t *testing.T) {
	s := seed(t,
		[]schema.Node{{ID: "a", Label: "A", Kind: "function", FileType: "code", Source: "a.go"}},
		[]schema.Edge{{Source: "a", Target: "a", Relation: "calls", Confidence: "EXTRACTED"}},
	)
	if _, _, err := s.Merge(&schema.Batch{Producer: "test2", Nodes: []schema.Node{{ID: "b", Label: "B", Kind: "function", FileType: "code", Source: "b.go"}}}); err != nil {
		t.Fatal(err)
	}
	if s.IndexCurrent() {
		t.Skip("merge rebuilt the index; staleness is not reachable this way")
	}
	if lk, ok := s.OpenLookup(); ok {
		lk.Close()
		t.Fatal("OpenLookup accepted an index older than the graph")
	}
}
