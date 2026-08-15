package store

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// The merge replaces a sort, so the property that matters is that it produces
// EXACTLY what the sort produced. Everything below compares against a
// from-scratch sort of the same final set rather than against a hand-written
// expectation, because the hand-written one is the thing most likely to be
// wrong.

func sortedCopy[T any](xs []T, key func(*T) string) []T {
	out := append([]T(nil), xs...)
	sort.Slice(out, func(i, j int) bool { return key(&out[i]) < key(&out[j]) })
	return out
}

func node(id, producer string) schema.Node {
	return schema.Node{
		ID: id, Label: id, Kind: "function", FileType: "code", Source: id + ".go",
		Metadata: map[string]string{"producer": producer},
	}
}

func TestMergeOrderedMatchesSort(t *testing.T) {
	cases := []struct {
		name  string
		old   []string // ids on disk, in file order
		final []string // ids that survive
	}{
		{"unchanged", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"one pruned", []string{"a", "b", "c"}, []string{"a", "c"}},
		{"one added at end", []string{"a", "b"}, []string{"a", "b", "z"}},
		{"one added at start", []string{"m", "n"}, []string{"a", "m", "n"}},
		{"added in middle", []string{"a", "z"}, []string{"a", "m", "z"}},
		{"several interleaved", []string{"b", "d", "f"}, []string{"a", "b", "c", "d", "e", "f", "g"}},
		{"all pruned", []string{"a", "b"}, nil},
		{"empty store", nil, []string{"a", "b"}},
		{"both empty", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var old []schema.Node
			for _, id := range c.old {
				old = append(old, node(id, "p"))
			}
			final := map[string]schema.Node{}
			for _, id := range c.final {
				final[id] = node(id, "p")
			}
			got := mergeOrdered(old, final, nodeKey)

			var all []schema.Node
			for _, n := range final {
				all = append(all, n)
			}
			want := sortedCopy(all, nodeKey)

			if len(got) != len(want) {
				t.Fatalf("length %d, want %d", len(got), len(want))
			}
			for i := range got {
				if got[i].ID != want[i].ID {
					t.Fatalf("position %d: %q, want %q (got %v)", i, got[i].ID, want[i].ID, ids(got))
				}
			}
		})
	}
}

func ids(ns []schema.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.ID
	}
	return out
}

// The merge trusts that the file is sorted. A store written by an older
// version, or hand-edited, must still come out correct — sorted, not merged
// into nonsense.
func TestMergeOrderedFallsBackWhenInputUnsorted(t *testing.T) {
	old := []schema.Node{node("z", "p"), node("a", "p"), node("m", "p")} // NOT sorted
	final := map[string]schema.Node{
		"z": node("z", "p"), "a": node("a", "p"), "m": node("m", "p"), "b": node("b", "p"),
	}
	got := ids(mergeOrdered(old, final, nodeKey))
	want := []string{"a", "b", "m", "z"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unsorted input not repaired: got %v, want %v", got, want)
		}
	}
}

// End-to-end: a store built by repeated ReplaceAll must be byte-identical to
// one built in a single shot. This is the guarantee the whole change rests on
// — the merge may not alter a single byte of the artifact.
func TestReplaceAllIncrementalIsByteIdenticalToFresh(t *testing.T) {
	mk := func(dir string) *Store {
		s, err := Open(dir, "m")
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	codeBatch := func(n int, tag string) *schema.Batch {
		b := &schema.Batch{Producer: "code"}
		for i := 0; i < n; i++ {
			id := string(rune('a'+i%26)) + tag + string(rune('0'+i/26))
			b.Nodes = append(b.Nodes, node(id, ""))
			if i > 0 {
				prev := string(rune('a'+(i-1)%26)) + tag + string(rune('0'+(i-1)/26))
				b.Edges = append(b.Edges, schema.Edge{
					Source: prev, Target: id, Relation: "calls", Confidence: schema.Inferred,
				})
			}
		}
		return b
	}
	docs := &schema.Batch{Producer: "markdown", Nodes: []schema.Node{
		{ID: "README.md", Label: "README", Kind: "document", FileType: "document", Source: "README.md"},
	}}

	// Incremental: land docs, then code, then RE-state code (the re-gather).
	inc := mk(t.TempDir())
	for _, batches := range [][]*schema.Batch{{docs}, {codeBatch(40, "x")}, {codeBatch(40, "x")}} {
		if _, err := inc.ReplaceAll(batches, false); err != nil {
			t.Fatal(err)
		}
	}
	// Fresh: one shot, same final content.
	fresh := mk(t.TempDir())
	if _, err := fresh.ReplaceAll([]*schema.Batch{docs, codeBatch(40, "x")}, false); err != nil {
		t.Fatal(err)
	}

	for _, f := range []string{"nodes.ndjson", "edges.ndjson"} {
		a, err := os.ReadFile(filepath.Join(inc.Dir, "graph", f))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(fresh.Dir, "graph", f))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Errorf("%s: incremental store differs from a fresh one\n--- incremental\n%s\n--- fresh\n%s", f, a, b)
		}
		if len(a) == 0 {
			t.Fatalf("%s is empty — the fixture wrote nothing", f)
		}
	}
}
