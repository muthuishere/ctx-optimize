package schema

import "testing"

func valid() Batch {
	return Batch{
		Producer: "test",
		Nodes: []Node{{
			ID: "pkg/a.go::Foo", Label: "Foo()", Kind: "function",
			FileType: "code", Source: "pkg/a.go", Location: "L10",
		}},
		Edges: []Edge{{
			Source: "pkg/a.go::Foo", Target: "pkg/b.go::Bar",
			Relation: "calls", Confidence: Extracted,
		}},
	}
}

func TestValidateAccepts(t *testing.T) {
	b := valid()
	if err := b.Validate(); err != nil {
		t.Fatalf("valid batch rejected: %v", err)
	}
}

func TestValidateFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Batch)
	}{
		{"missing producer", func(b *Batch) { b.Producer = " " }},
		{"missing node id", func(b *Batch) { b.Nodes[0].ID = "" }},
		{"duplicate node id", func(b *Batch) { b.Nodes = append(b.Nodes, b.Nodes[0]) }},
		{"missing label", func(b *Batch) { b.Nodes[0].Label = "" }},
		{"missing kind", func(b *Batch) { b.Nodes[0].Kind = "" }},
		{"missing file_type", func(b *Batch) { b.Nodes[0].FileType = "" }},
		{"missing source", func(b *Batch) { b.Nodes[0].Source = "" }},
		{"edge missing endpoint", func(b *Batch) { b.Edges[0].Target = "" }},
		{"edge missing relation", func(b *Batch) { b.Edges[0].Relation = "" }},
		{"edge bad confidence", func(b *Batch) { b.Edges[0].Confidence = "MAYBE" }},
	}
	for _, tc := range cases {
		b := valid()
		tc.mutate(&b)
		if err := b.Validate(); err == nil {
			t.Errorf("%s: expected rejection, got nil", tc.name)
		}
	}
}
