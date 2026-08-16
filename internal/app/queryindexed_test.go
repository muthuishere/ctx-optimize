package app

import (
	"encoding/json"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/query"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

// THE I1 GATE, against a real store rather than a fake: the indexed lane is an
// access path, so `query` must return the same answer with the index and
// without it — same hits, same order, same neighbours in the same order.
//
// This is the test that catches an index lane returning a node's edges in the
// wrong order. `query` caps neighbours at 12, so order decides which twelve
// survive; the store-level unit fixture does not discriminate (a mutation
// removing the file-order sort survives it), and this does.
func TestIndexedQueryMatchesScanOnARealStore(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(root, "r")
	if err != nil {
		t.Fatal(err)
	}

	// A hub with more than 12 neighbours, so the cap BINDS and order matters,
	// with in- and out-edges interleaved down the file.
	var nodes []schema.Node
	var edges []schema.Edge
	nodes = append(nodes, schema.Node{
		ID: "src/hub.go::Refund", Label: "Refund", Kind: "function",
		FileType: "code", Source: "src/hub.go", Location: "L1-L9",
	})
	for i := 0; i < 20; i++ {
		id := "src/n" + string(rune('a'+i)) + ".go::Fn"
		nodes = append(nodes, schema.Node{
			ID: id, Label: "Fn" + string(rune('a'+i)), Kind: "function",
			FileType: "code", Source: "src/n" + string(rune('a'+i)) + ".go", Location: "L1-L3",
		})
		if i%2 == 0 {
			edges = append(edges, schema.Edge{Source: "src/hub.go::Refund", Target: id, Relation: "calls", Confidence: schema.Inferred})
		} else {
			edges = append(edges, schema.Edge{Source: id, Target: "src/hub.go::Refund", Relation: "calls", Confidence: schema.Inferred})
		}
	}
	edges = append(edges, schema.Edge{
		Source: "src/hub.go::Refund", Target: "src/hub.go::Refund",
		Relation: "calls", Confidence: schema.Inferred,
	})
	if _, _, err := s.Merge(&schema.Batch{Producer: "t", Nodes: nodes, Edges: edges}); err != nil {
		t.Fatal(err)
	}
	if err := s.BuildIndex(); err != nil {
		t.Fatal(err)
	}

	gotNodes, err := s.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	gotEdges, err := s.Edges()
	if err != nil {
		t.Fatal(err)
	}
	// The indexed lane must actually be in use, or this proves nothing.
	if _, ok, err := s.EdgesTouchingOrdered("src/hub.go::Refund"); err != nil || !ok {
		t.Fatalf("index not usable after BuildIndex (ok=%v err=%v) — the comparison would be scan vs scan", ok, err)
	}

	for _, q := range []string{"refund", "Refund", "fn", "refund calls"} {
		want, err := json.Marshal(query.Run(gotNodes, gotEdges, q, 4000))
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(query.RunIndexed(gotNodes, gotEdges, s, q, 4000))
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(got) {
			t.Errorf("query %q: indexed lane disagrees with the scan\n scan    %s\n indexed %s", q, want, got)
		}
	}
}
