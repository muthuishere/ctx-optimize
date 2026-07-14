package analyze

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// clusterFixture builds two obvious 5-node clusters (a*, b*) bridged by one
// node, plus one isolated node that must not appear in any community.
func clusterFixture() ([]schema.Node, []schema.Edge) {
	var nodes []schema.Node
	var edges []schema.Edge
	mk := func(prefix, dir string) []string {
		ids := make([]string, 5)
		for i := range ids {
			id := fmt.Sprintf("%s/%s%d", dir, prefix, i)
			ids[i] = id
			nodes = append(nodes, schema.Node{
				ID: id, Label: fmt.Sprintf("%s%d()", prefix, i), Kind: "function",
				FileType: "code", Source: id + ".go",
			})
		}
		// dense: every pair connected
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				edges = append(edges, schema.Edge{Source: ids[i], Target: ids[j], Relation: "calls", Confidence: schema.Inferred})
			}
		}
		return ids
	}
	as := mk("a", "internal/alpha")
	bs := mk("b", "internal/beta")
	nodes = append(nodes,
		schema.Node{ID: "cmd/bridge", Label: "bridge()", Kind: "function", FileType: "code", Source: "cmd/bridge.go"},
		schema.Node{ID: "orphan", Label: "orphan", Kind: "file", FileType: "code", Source: "orphan.go"},
	)
	edges = append(edges,
		schema.Edge{Source: "cmd/bridge", Target: as[0], Relation: "calls", Confidence: schema.Inferred},
		schema.Edge{Source: "cmd/bridge", Target: bs[0], Relation: "calls", Confidence: schema.Inferred},
	)
	return nodes, edges
}

func TestCommunitiesTwoClusters(t *testing.T) {
	nodes, edges := clusterFixture()
	comms := Communities(nodes, edges)
	if len(comms) != 2 {
		t.Fatalf("communities = %d, want 2: %+v", len(comms), comms)
	}
	memberOf := map[string]int{}
	for ci, c := range comms {
		for _, m := range c.Members {
			memberOf[m] = ci
		}
	}
	// Each cluster stays whole.
	for _, prefix := range []string{"internal/alpha/a", "internal/beta/b"} {
		want := memberOf[prefix+"0"]
		for i := 1; i < 5; i++ {
			id := fmt.Sprintf("%s%d", prefix, i)
			if memberOf[id] != want {
				t.Fatalf("%s split from its cluster: %+v", id, comms)
			}
		}
	}
	if memberOf["internal/alpha/a0"] == memberOf["internal/beta/b0"] {
		t.Fatalf("clusters merged: %+v", comms)
	}
	// The bridge lands in exactly one community; the isolated node in none.
	if _, ok := memberOf["cmd/bridge"]; !ok {
		t.Fatalf("bridge node missing from communities: %+v", comms)
	}
	if _, ok := memberOf["orphan"]; ok {
		t.Fatal("degree-0 node must not join a community")
	}
	// Largest first (the bridge's community has 6 members).
	if len(comms[0].Members) < len(comms[1].Members) {
		t.Fatalf("not ordered largest-first: %d then %d", len(comms[0].Members), len(comms[1].Members))
	}
	// Labels are derived: hub label + dominant dir.
	for _, c := range comms {
		if len(c.Hubs) == 0 || len(c.Dirs) == 0 {
			t.Fatalf("community missing hubs/dirs: %+v", c)
		}
		if c.Label == "" {
			t.Fatalf("community missing label: %+v", c)
		}
	}
	found := false
	for _, c := range comms {
		if c.Label == "b0() (internal/beta)" || c.Label == "a0() (internal/alpha)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no derived '<hub> (<dir>)' label: %q / %q", comms[0].Label, comms[1].Label)
	}
}

// Identical input — including shuffled input order — must yield identical
// output across runs.
func TestCommunitiesDeterministic(t *testing.T) {
	nodes, edges := clusterFixture()
	first := Communities(nodes, edges)
	for run := 0; run < 3; run++ {
		if got := Communities(nodes, edges); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs:\n%+v\nvs\n%+v", run, got, first)
		}
	}
	// Reverse the input slices: order must not matter.
	rn := make([]schema.Node, len(nodes))
	for i, n := range nodes {
		rn[len(nodes)-1-i] = n
	}
	re := make([]schema.Edge, len(edges))
	for i, e := range edges {
		re[len(edges)-1-i] = e
	}
	if got := Communities(rn, re); !reflect.DeepEqual(got, first) {
		t.Fatalf("shuffled input differs:\n%+v\nvs\n%+v", got, first)
	}
}

// Granularity: many small linked clusters agglomerate to a useful count
// (tens, not one-per-cluster), and dust (< minCommunity) is absorbed.
func TestCommunitiesGranularity(t *testing.T) {
	var nodes []schema.Node
	var edges []schema.Edge
	const clusters, size = 100, 8
	for c := 0; c < clusters; c++ {
		for i := 0; i < size; i++ {
			id := fmt.Sprintf("pkg%03d/n%d", c, i)
			nodes = append(nodes, schema.Node{ID: id, Label: id, Kind: "function", FileType: "code", Source: id + ".go"})
		}
		for i := 1; i < size; i++ { // star on the cluster's first node
			edges = append(edges, schema.Edge{
				Source: fmt.Sprintf("pkg%03d/n0", c), Target: fmt.Sprintf("pkg%03d/n%d", c, i),
				Relation: "contains", Confidence: schema.Extracted,
			})
		}
		if c > 0 { // chain the clusters
			edges = append(edges, schema.Edge{
				Source: fmt.Sprintf("pkg%03d/n0", c-1), Target: fmt.Sprintf("pkg%03d/n0", c),
				Relation: "imports", Confidence: schema.Extracted,
			})
		}
	}
	comms := Communities(nodes, edges)
	if len(comms) == 0 || len(comms) > 40 {
		t.Fatalf("communities = %d, want 1..40 (modularity should merge chained mini-clusters)", len(comms))
	}
	total := 0
	for _, c := range comms {
		if len(c.Members) < minCommunity {
			t.Fatalf("dust community survived (%d members): %+v", len(c.Members), c)
		}
		total += len(c.Members)
	}
	if total != clusters*size {
		t.Fatalf("members = %d, want %d (every connected node clustered exactly once)", total, clusters*size)
	}
}

// Performance guard: ~50k nodes / ~125k edges must complete well under a
// second — the chromium store (1.5M nodes) is real and the algorithm must
// stay near-linear.
func TestCommunities50kUnderASecond(t *testing.T) {
	var nodes []schema.Node
	var edges []schema.Edge
	const clusters, size = 500, 100 // 50k nodes
	for c := 0; c < clusters; c++ {
		for i := 0; i < size; i++ {
			id := fmt.Sprintf("mod%04d/f%03d", c, i)
			nodes = append(nodes, schema.Node{ID: id, Label: id, Kind: "function", FileType: "code", Source: id + ".go"})
		}
		for i := 0; i < size; i++ {
			// ring + star: ~2 edges per node
			edges = append(edges, schema.Edge{
				Source: fmt.Sprintf("mod%04d/f%03d", c, i), Target: fmt.Sprintf("mod%04d/f%03d", c, (i+1)%size),
				Relation: "calls", Confidence: schema.Inferred,
			})
			if i > 0 {
				edges = append(edges, schema.Edge{
					Source: fmt.Sprintf("mod%04d/f000", c), Target: fmt.Sprintf("mod%04d/f%03d", c, i),
					Relation: "contains", Confidence: schema.Extracted,
				})
			}
		}
		if c > 0 {
			edges = append(edges, schema.Edge{
				Source: fmt.Sprintf("mod%04d/f000", c-1), Target: fmt.Sprintf("mod%04d/f000", c),
				Relation: "imports", Confidence: schema.Extracted,
			})
		}
	}
	start := time.Now()
	comms := Communities(nodes, edges)
	elapsed := time.Since(start)
	t.Logf("Communities on %d nodes / %d edges: %v → %d communities", len(nodes), len(edges), elapsed, len(comms))
	if elapsed > time.Second {
		t.Fatalf("took %v, want well under 1s", elapsed)
	}
	if len(comms) == 0 || len(comms) >= clusters {
		t.Fatalf("communities = %d, want agglomerated well below %d clusters", len(comms), clusters)
	}
}
