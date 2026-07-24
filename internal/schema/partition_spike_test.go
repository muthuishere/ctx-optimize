package schema

import (
	"fmt"
	"reflect"
	"testing"
)

// goodNode builds a valid node with a unique id.
func goodNode(i int) Node {
	return Node{
		ID:       fmt.Sprintf("pkg/f%d.go::Fn%d", i, i),
		Label:    fmt.Sprintf("Fn%d()", i),
		Kind:     "function",
		FileType: "code",
		Source:   fmt.Sprintf("pkg/f%d.go", i),
	}
}

func edge(src, tgt string) Edge {
	return Edge{Source: src, Target: tgt, Relation: "calls", Confidence: Extracted}
}

// S1 — the Linux case: one empty-label node among many must NOT discard the rest.
func TestPartition_OneBadNode_CommitsRest(t *testing.T) {
	b := &Batch{Producer: "code"}
	for i := 0; i < 1000; i++ {
		b.Nodes = append(b.Nodes, goodNode(i))
	}
	// the udev.txt analogue: empty label
	bad := Node{ID: "Documentation/udev.txt:::", Label: "", Kind: "section", FileType: "document", Source: "Documentation/udev.txt"}
	b.Nodes = append(b.Nodes, bad)

	acc, q, err := b.PartitionValidate(nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(acc.Nodes) != 1000 {
		t.Fatalf("accepted=%d, want 1000 (the good nodes)", len(acc.Nodes))
	}
	if len(q) != 1 || q[0].ID != bad.ID || q[0].Reason != "label is required" {
		t.Fatalf("quarantine=%+v, want exactly the empty-label node", q)
	}
}

// S2 — edge cascade: an edge into a quarantined node must be dropped (no dangling).
func TestPartition_EdgeCascade_NoDangling(t *testing.T) {
	b := &Batch{Producer: "code"}
	b.Nodes = append(b.Nodes, goodNode(1)) // valid
	bad := Node{ID: "bad", Label: "", Kind: "function", FileType: "code", Source: "x.go"}
	b.Nodes = append(b.Nodes, bad)
	// edge from a valid node to the quarantined bad node -> must be dropped
	b.Edges = append(b.Edges, edge(goodNode(1).ID, "bad"))
	// edge between two valid nodes -> must survive (target already in store)
	b.Edges = append(b.Edges, edge(goodNode(1).ID, "already/in/store"))

	acc, q, err := b.PartitionValidate(map[string]bool{"already/in/store": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(acc.Edges) != 1 {
		t.Fatalf("accepted edges=%d, want 1 (the non-dangling one)", len(acc.Edges))
	}
	// verify NO accepted edge references a non-accepted, non-existing node
	valid := map[string]bool{"already/in/store": true}
	for _, n := range acc.Nodes {
		valid[n.ID] = true
	}
	for _, e := range acc.Edges {
		if !valid[e.Source] || !valid[e.Target] {
			t.Fatalf("DANGLING edge survived: %s->%s", e.Source, e.Target)
		}
	}
	foundCascade := false
	for _, x := range q {
		if x.Reason == "dangling: endpoint quarantined this batch" {
			foundCascade = true
		}
	}
	if !foundCascade {
		t.Fatalf("expected a cascade-quarantined edge, got %+v", q)
	}
}

// S3 — duplicate id: first wins, later dup quarantined (dedup integrity kept).
func TestPartition_DuplicateID(t *testing.T) {
	b := &Batch{Producer: "code", Nodes: []Node{goodNode(1), goodNode(1)}}
	acc, q, _ := b.PartitionValidate(nil)
	if len(acc.Nodes) != 1 {
		t.Fatalf("accepted=%d, want 1 (first wins)", len(acc.Nodes))
	}
	if len(q) != 1 || q[0].Reason != "duplicate id in batch" {
		t.Fatalf("want 1 duplicate quarantined, got %+v", q)
	}
}

// S4 — determinism: identical input yields byte-identical accepted+quarantine.
func TestPartition_Deterministic(t *testing.T) {
	build := func() (*Batch, []Quarantine) {
		b := &Batch{Producer: "code"}
		for i := 0; i < 500; i++ {
			b.Nodes = append(b.Nodes, goodNode(i))
			if i%97 == 0 { // scatter some bad nodes
				b.Nodes = append(b.Nodes, Node{ID: fmt.Sprintf("bad%d", i), Kind: "function", FileType: "code", Source: "x"})
			}
		}
		acc, q, _ := b.PartitionValidate(nil)
		return acc, q
	}
	a1, q1 := build()
	a2, q2 := build()
	if !reflect.DeepEqual(a1, a2) || !reflect.DeepEqual(q1, q2) {
		t.Fatal("non-deterministic partition output")
	}
}

// S5 — valid batch: parity with Validate (nothing quarantined, Validate passes).
func TestPartition_AllValid_ParityWithValidate(t *testing.T) {
	b := &Batch{Producer: "code"}
	for i := 0; i < 300; i++ {
		b.Nodes = append(b.Nodes, goodNode(i))
	}
	b.Edges = append(b.Edges, edge(goodNode(0).ID, goodNode(1).ID))
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate should pass a clean batch: %v", err)
	}
	acc, q, _ := b.PartitionValidate(nil)
	if len(q) != 0 || len(acc.Nodes) != 300 || len(acc.Edges) != 1 {
		t.Fatalf("clean batch must pass whole: acc=%d/%d q=%d", len(acc.Nodes), len(acc.Edges), len(q))
	}
}

// S6 — abort ratio (call-site policy): quarantine >5% signals a broken producer.
func quarantineRatio(total, q int) float64 {
	if total == 0 {
		return 0
	}
	return float64(q) / float64(total)
}

func TestPartition_AbortRatio(t *testing.T) {
	// 1 bad in 1000 -> 0.1% -> proceed
	if r := quarantineRatio(1000, 1); r > 0.05 {
		t.Fatalf("one bad apple should NOT abort, ratio=%.4f", r)
	}
	// 200 bad in 1000 -> 20% -> abort (broken extractor)
	if r := quarantineRatio(1000, 200); r <= 0.05 {
		t.Fatalf("broken extractor should abort, ratio=%.4f", r)
	}
}
