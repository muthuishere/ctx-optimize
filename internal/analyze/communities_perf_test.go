package analyze

import (
	"fmt"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// dustGraph builds the worst case for the dust-merge phase: n tiny components,
// every one below minCommunity, so every iteration of the merge loop has a
// candidate and the community count starts at n.
//
// This is not a contrived shape. It is what a big repo of mostly-unconnected
// files looks like: 12,000 files each declaring one never-called function.
func dustGraph(n int) ([]schema.Node, []schema.Edge) {
	var nodes []schema.Node
	var edges []schema.Edge
	for i := 0; i < n; i++ {
		f := fmt.Sprintf("f%d.go", i)
		d := fmt.Sprintf("f%d.go::Fn%d", i, i)
		nodes = append(nodes,
			schema.Node{ID: f, Label: f, Kind: "file", FileType: "code", Source: f},
			schema.Node{ID: d, Label: fmt.Sprintf("Fn%d", i), Kind: "function", FileType: "code", Source: f})
		edges = append(edges, schema.Edge{
			Source: f, Target: d, Relation: "contains", Confidence: schema.Extracted, Weight: 1})
	}
	return nodes, edges
}

// The dust-merge loop used to rebuild and re-SORT the whole community list on
// every iteration: O(n² log n). Measured on a 12k-file store it burned 12.8s
// inside Communities — 90% of the wiki's total time — to produce ZERO
// communities, because every component was dust and got dropped. A `gather`
// that looks hung is usually something like this, not I/O.
//
// The ceiling is deliberately loose (a slow CI box may be several times slower
// than a dev laptop) because it is guarding against a return to QUADRATIC, not
// policing constant factors: the old code needed ~13s here, the fixed code
// ~0.1s. Anything in between still fails.
func TestCommunitiesDustMergeIsNotQuadratic(t *testing.T) {
	nodes, edges := dustGraph(12000)

	start := time.Now()
	got := Communities(nodes, edges)
	elapsed := time.Since(start)

	const ceiling = 3 * time.Second
	if elapsed > ceiling {
		t.Errorf("Communities on %d dust components took %v, ceiling %v — the dust-merge loop is quadratic again",
			len(nodes)/2, elapsed.Round(time.Millisecond), ceiling)
	}
	t.Logf("12k dust components: %v (ceiling %v), %d communities", elapsed.Round(time.Millisecond), ceiling, len(got))
}

// Speed is worthless if the clustering changed. Disconnected dust is not a
// subsystem, so a graph made only of dust yields nothing — and a graph with one
// real cluster in it still yields exactly that cluster.
func TestCommunitiesDropsDustAndKeepsRealClusters(t *testing.T) {
	nodes, edges := dustGraph(200)
	if got := Communities(nodes, edges); len(got) != 0 {
		t.Errorf("all-dust graph produced %d communities, want 0 — dust is not a subsystem", len(got))
	}

	// Add one densely connected group, comfortably above minCommunity.
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("core/c%d.go", i)
		nodes = append(nodes, schema.Node{ID: id, Label: id, Kind: "file", FileType: "code", Source: id})
	}
	for i := 0; i < 12; i++ {
		for j := i + 1; j < 12; j++ {
			edges = append(edges, schema.Edge{
				Source: fmt.Sprintf("core/c%d.go", i), Target: fmt.Sprintf("core/c%d.go", j),
				Relation: "calls", Confidence: schema.Inferred, Weight: 1})
		}
	}
	got := Communities(nodes, edges)
	if len(got) != 1 {
		t.Fatalf("want exactly 1 community (the dense group), got %d", len(got))
	}
	if len(got[0].Members) != 12 {
		t.Errorf("community has %d members, want the 12 connected files", len(got[0].Members))
	}
}

// Determinism is the property the heap had to preserve: it replaced a sort, and
// a heap with lazy invalidation is only equivalent if ties break the same way.
func TestCommunitiesDeterministicUnderReorderedInput(t *testing.T) {
	nodes, edges := dustGraph(60)
	for i := 0; i < 10; i++ {
		for j := i + 1; j < 10; j++ {
			edges = append(edges, schema.Edge{
				Source: fmt.Sprintf("f%d.go", i), Target: fmt.Sprintf("f%d.go", j),
				Relation: "calls", Confidence: schema.Inferred, Weight: 1})
		}
	}
	first := fmt.Sprint(Communities(nodes, edges))

	rev := make([]schema.Edge, len(edges))
	for i := range edges {
		rev[i] = edges[len(edges)-1-i]
	}
	revN := make([]schema.Node, len(nodes))
	for i := range nodes {
		revN[i] = nodes[len(nodes)-1-i]
	}
	if second := fmt.Sprint(Communities(revN, rev)); first != second {
		t.Error("Communities is order-dependent — the heap changed tie-breaking")
	}
}
