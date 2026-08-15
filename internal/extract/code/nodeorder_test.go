package code

import (
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// ADR 5: distinct declarations can collapse to one id (C function-locals and
// type references, C# overloads, several Go init()s in one file). sortBatch
// keeps the first of each id run, so the ORDER among colliding copies decides
// which location a citation reports. It must be a total order — otherwise
// sort.Slice, which is not stable, picks a different survivor per run.
func TestNodeLessIsTotalAndPrefersTheDefinition(t *testing.T) {
	// The kernel case: one real `struct bfq_queue {…}` and two field
	// declarations naming the same type. Shuffled input must always dedup to
	// the definition, never to a field.
	def := schema.Node{ID: "h::bfq_queue", Kind: "struct", Location: "L246-L412"}
	ref1 := schema.Node{ID: "h::bfq_queue", Kind: "struct", Location: "L1049-L1049"}
	ref2 := schema.Node{ID: "h::bfq_queue", Kind: "struct", Location: "L213-L213"}

	for _, order := range [][]schema.Node{
		{def, ref1, ref2}, {ref1, ref2, def}, {ref2, def, ref1}, {ref1, def, ref2},
	} {
		b := &schema.Batch{Producer: "code", Nodes: append([]schema.Node(nil), order...)}
		sortBatch(b)
		if len(b.Nodes) != 1 {
			t.Fatalf("expected the colliding ids to dedup to one node, got %d", len(b.Nodes))
		}
		if got := b.Nodes[0].Location; got != "L246-L412" {
			// A one-line field beating the definition is the bug: `card
			// bfq_queue` would cite a struct member instead of the type.
			t.Errorf("dedup kept %s, want the widest span L246-L412", got)
		}
	}
}

func TestNodeLessBreaksEveryTie(t *testing.T) {
	// Two nodes identical in every field the old comparator looked at, and in
	// every field the new one looks at except metadata: the richer copy (the
	// one that captured a doc comment) must win, deterministically.
	rich := schema.Node{ID: "a.c::f.v", Kind: "struct", Location: "L10-L10",
		Metadata: map[string]string{"lang": "c", "doc": "/** the doc */", "signature": "struct bio"}}
	poor := schema.Node{ID: "a.c::f.v", Kind: "struct", Location: "L10-L10",
		Metadata: map[string]string{"lang": "c", "signature": "struct bio"}}

	for _, order := range [][]schema.Node{{rich, poor}, {poor, rich}} {
		b := &schema.Batch{Producer: "code", Nodes: append([]schema.Node(nil), order...)}
		sortBatch(b)
		if len(b.Nodes) != 1 {
			t.Fatalf("want 1 node after dedup, got %d", len(b.Nodes))
		}
		if _, ok := b.Nodes[0].Metadata["doc"]; !ok {
			t.Error("dedup dropped the copy carrying the doc comment")
		}
	}

	// Antisymmetry: nodeLess must never report both a<b and b<a, or sort's
	// contract is violated and the result is undefined.
	all := []schema.Node{rich, poor,
		{ID: "a.c::f.v", Kind: "struct", Location: "L11-L11"},
		{ID: "b.c::g", Kind: "function", Location: "L1-L9"},
	}
	for i := range all {
		for j := range all {
			if nodeLess(&all[i], &all[j]) && nodeLess(&all[j], &all[i]) {
				t.Fatalf("nodeLess is not antisymmetric for %d/%d", i, j)
			}
		}
	}
}

// The property that actually matters: whatever the input order, the sorted
// output is identical. That is what makes a citation stable between gathers.
func TestSortBatchIsOrderIndependent(t *testing.T) {
	nodes := []schema.Node{
		{ID: "f.c::x", Kind: "struct", Location: "L5-L5", Metadata: map[string]string{"a": "1"}},
		{ID: "f.c::x", Kind: "struct", Location: "L100-L200"},
		{ID: "f.c::x", Kind: "struct", Location: "L5-L5"},
		{ID: "f.c::y", Kind: "function", Location: "L1-L4"},
		{ID: "a.c::z", Kind: "function", Location: "L9-L20"},
	}
	want := ""
	for shift := 0; shift < len(nodes); shift++ {
		rotated := append(append([]schema.Node(nil), nodes[shift:]...), nodes[:shift]...)
		b := &schema.Batch{Producer: "code", Nodes: rotated}
		sortBatch(b)
		got := ""
		for _, n := range b.Nodes {
			got += n.ID + "|" + n.Location + ";"
		}
		if shift == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("rotation %d produced %q, want %q", shift, got, want)
		}
	}
}

// Guard the parser: a location this producer did not mint must not panic or
// silently compare as a span.
func TestSpanOfRejectsForeignLocations(t *testing.T) {
	for _, loc := range []string{"", "L", "L12", "12-14", "Lx-Ly", "pg://t", "L1-L"} {
		if _, _, ok := spanOf(loc); ok {
			t.Errorf("spanOf(%q) claimed to parse a span", loc)
		}
	}
	s, e, ok := spanOf("L246-L412")
	if !ok || s != 246 || e != 412 {
		t.Errorf("spanOf(L246-L412) = %d,%d,%v", s, e, ok)
	}
}
