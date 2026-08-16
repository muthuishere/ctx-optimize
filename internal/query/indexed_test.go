package query

import (
	"encoding/json"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// fakeNB serves EdgesTouchingOrdered from an in-memory edge list, in the same
// file order a real store's merged offset lists produce.
type fakeNB struct {
	edges []schema.Edge
	ok    bool
	calls int
}

func (f *fakeNB) EdgesTouchingOrdered(id string) ([]schema.Edge, bool, error) {
	f.calls++
	if !f.ok {
		return nil, false, nil
	}
	var out []schema.Edge
	for _, e := range f.edges {
		if e.Source == id || e.Target == id {
			out = append(out, e)
		}
	}
	return out, true, nil
}

// graph builds a shape with the two cases that decide neighbour ORDER, which
// decides WHICH twelve survive query's cap: a self-edge (both directions from
// one line) and a node whose in- and out-edges interleave in the file.
func graph() ([]schema.Node, []schema.Edge) {
	nodes := []schema.Node{
		{ID: "a/refund.go", Label: "refund.go", Kind: "file", FileType: "code", Source: "a/refund.go"},
		{ID: "a/refund.go::Refund", Label: "Refund", Kind: "function", FileType: "code", Source: "a/refund.go", Location: "L1-L9"},
		{ID: "b/pay.go", Label: "pay.go", Kind: "file", FileType: "code", Source: "b/pay.go"},
		{ID: "b/pay.go::Pay", Label: "Pay", Kind: "function", FileType: "code", Source: "b/pay.go", Location: "L1-L9"},
		{ID: "c/log.go::Log", Label: "Log", Kind: "function", FileType: "code", Source: "c/log.go", Location: "L1-L4"},
	}
	edges := []schema.Edge{
		{Source: "a/refund.go", Target: "a/refund.go::Refund", Relation: "contains", Confidence: schema.Extracted},
		{Source: "a/refund.go::Refund", Target: "b/pay.go::Pay", Relation: "calls", Confidence: schema.Inferred},
		// a self-edge: ONE line, and the scan appends both directions from it
		{Source: "a/refund.go::Refund", Target: "a/refund.go::Refund", Relation: "calls", Confidence: schema.Inferred},
		{Source: "c/log.go::Log", Target: "a/refund.go::Refund", Relation: "calls", Confidence: schema.Inferred},
		{Source: "b/pay.go", Target: "b/pay.go::Pay", Relation: "contains", Confidence: schema.Extracted},
	}
	return nodes, edges
}

// I1: the index is an ACCESS PATH, not a new scorer. Same hits, same order,
// same neighbours in the same order — byte for byte on the JSON.
func TestIndexedAnswersAreByteIdenticalToTheScan(t *testing.T) {
	nodes, edges := graph()
	for _, q := range []string{"refund", "pay", "refund payment", "log", "Refund"} {
		want, err := json.Marshal(Run(nodes, edges, q, 4000))
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(RunIndexed(nodes, edges, &fakeNB{edges: edges, ok: true}, q, 4000))
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(got) {
			t.Errorf("query %q diverged\n scan    %s\n indexed %s", q, want, got)
		}
	}
}

// A self-edge is ONE line in the file, and the scan turns it into TWO
// neighbours (out then in). The indexed lane reads one edge and must do the
// same, or a recursive function loses a neighbour.
func TestSelfEdgeYieldsBothDirections(t *testing.T) {
	nodes, edges := graph()
	res := RunIndexed(nodes, edges, &fakeNB{edges: edges, ok: true}, "Refund", 4000)
	if len(res.Hits) == 0 {
		t.Fatal("no hits")
	}
	var self int
	for _, n := range res.Hits[0].Neighbors {
		if n.ID == "a/refund.go::Refund" {
			self++
		}
	}
	if self != 2 {
		t.Fatalf("self-edge produced %d neighbours, want 2 (out and in)", self)
	}
}

// I2/I3: no index, or a stale one, must fall back to the scan and still answer
// correctly. A silent WRONG answer is the failure; a silent SLOW answer is the
// regression the disclosure field exists for.
func TestFallsBackWhenTheIndexIsAbsent(t *testing.T) {
	nodes, edges := graph()
	want, _ := json.Marshal(Run(nodes, edges, "refund", 4000))

	nb := &fakeNB{edges: edges, ok: false} // index missing or stale
	got, _ := json.Marshal(RunIndexed(nodes, edges, nb, "refund", 4000))
	if string(want) != string(got) {
		t.Errorf("fallback diverged\n scan    %s\n fallback %s", want, got)
	}
	// It must decide ONCE, up front — not per hit, and not halfway through.
	if nb.calls != 1 {
		t.Errorf("probed the index %d times; a fallback decided per-hit can answer half from each lane", nb.calls)
	}

	// nil is the same story: the scan, unchanged.
	got2, _ := json.Marshal(RunIndexed(nodes, edges, nil, "refund", 4000))
	if string(want) != string(got2) {
		t.Errorf("nil Neighbors diverged from the scan")
	}
}

// The point of the slice: the indexed lane must not need the edge slice at all.
// If this passes with nil edges, the caller can stop reading 5.5M of them.
func TestIndexedLaneDoesNotNeedTheEdgeSlice(t *testing.T) {
	nodes, edges := graph()
	want, _ := json.Marshal(Run(nodes, edges, "refund", 4000))
	got, _ := json.Marshal(RunIndexed(nodes, nil, &fakeNB{edges: edges, ok: true}, "refund", 4000))
	if string(want) != string(got) {
		t.Errorf("indexed lane needs the edges after all\n scan    %s\n indexed %s", want, got)
	}
}
