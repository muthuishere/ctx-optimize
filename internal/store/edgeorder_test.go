package store

import (
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// The indexed lane exists to give `query` the SAME neighbour order a scan does,
// because query caps a node's neighbours at 12 and order decides which twelve
// survive. Concatenating from-edges then to-edges — what EdgesTouching does —
// silently returns a different twelve.
//
// The graph is built so file order and concatenated order DISAGREE: the hub's
// edges alternate in/out down the file.
func TestEdgesTouchingOrderedMatchesFileOrder(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, "r")
	if err != nil {
		t.Fatal(err)
	}
	edges := []schema.Edge{
		{Source: "hub", Target: "out1", Relation: "calls", Confidence: schema.Extracted},
		{Source: "in1", Target: "hub", Relation: "calls", Confidence: schema.Extracted},
		{Source: "hub", Target: "hub", Relation: "calls", Confidence: schema.Extracted}, // self
		{Source: "in2", Target: "hub", Relation: "calls", Confidence: schema.Extracted},
		{Source: "hub", Target: "out2", Relation: "calls", Confidence: schema.Extracted},
		{Source: "noise", Target: "elsewhere", Relation: "calls", Confidence: schema.Extracted},
	}
	if _, _, err := s.Merge(&schema.Batch{Producer: "t", Edges: edges}); err != nil {
		t.Fatal(err)
	}
	if err := s.BuildIndex(); err != nil {
		t.Fatal(err)
	}

	got, ok, err := s.EdgesTouchingOrdered("hub")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("index reported unavailable right after BuildIndex")
	}

	// what a scan produces: every edge touching hub, in file order, once each
	all, err := s.Edges()
	if err != nil {
		t.Fatal(err)
	}
	var want []schema.Edge
	for _, e := range all {
		if e.Source == "hub" || e.Target == "hub" {
			want = append(want, e)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d edges, scan finds %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Source != want[i].Source || got[i].Target != want[i].Target {
			t.Fatalf("order diverges at %d: indexed %s->%s, scan %s->%s",
				i, got[i].Source, got[i].Target, want[i].Source, want[i].Target)
		}
	}
	// and the self-edge is ONE line, not two — the caller expands it
	self := 0
	for _, e := range got {
		if e.Source == "hub" && e.Target == "hub" {
			self++
		}
	}
	if self != 1 {
		t.Fatalf("self-edge returned %d times, want 1 (it is one line)", self)
	}
}

// No index, or an index stale against the graph, must report ok=false rather
// than answering from a graph that no longer exists (ADR 29 I2).
func TestEdgesTouchingOrderedRefusesWithoutAFreshIndex(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, "r")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Merge(&schema.Batch{Producer: "t", Edges: []schema.Edge{
		{Source: "a", Target: "b", Relation: "calls", Confidence: schema.Extracted},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.EdgesTouchingOrdered("a"); err != nil || ok {
		t.Fatalf("claimed an index before one was built (ok=%v err=%v)", ok, err)
	}
	if err := s.BuildIndex(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.EdgesTouchingOrdered("a"); !ok {
		t.Fatal("index not used after BuildIndex")
	}
	// the graph moves on and the index does not
	if _, _, err := s.Merge(&schema.Batch{Producer: "t2", Edges: []schema.Edge{
		{Source: "c", Target: "d", Relation: "calls", Confidence: schema.Extracted},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.EdgesTouchingOrdered("a"); ok {
		t.Fatal("served a STALE index — the answer would come from a graph that no longer exists")
	}
}
