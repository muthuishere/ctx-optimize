package main

// SPIKE: what does neighbour attachment cost through the EXISTING edge index,
// versus the full read + adjacency map query.go builds today?
//
//	go test ./scripts/spikes/queryphases -run TestNeighborCost -v -store ~/ctxoptimize/linux
//
// query.go currently reads every edge and builds an adjacency map over the
// whole graph — 5.5M edges, 11M appends — to attach at most 12 neighbours to
// at most 20 hits. `card` has used a binary-searched edge index for this since
// the lookup ADR; query never picked it up.

import (
	"flag"
	"path/filepath"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/store"
)

var storeDir = flag.String("store", "", "store dir")

func TestNeighborCost(t *testing.T) {
	if *storeDir == "" {
		t.Skip("no --store")
	}
	s, err := store.Open(filepath.Dir(*storeDir), filepath.Base(*storeDir))
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := s.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	// 20 hits is query's ceiling — the real workload, not a microbenchmark.
	picks := make([]string, 0, 20)
	for i := 0; i < len(nodes) && len(picks) < 20; i += len(nodes) / 25 {
		picks = append(picks, nodes[i].ID)
	}

	t0 := time.Now()
	got := 0
	for _, id := range picks {
		from, err := s.EdgesFrom(id)
		if err != nil {
			t.Fatal(err)
		}
		to, err := s.EdgesTo(id)
		if err != nil {
			t.Fatal(err)
		}
		got += len(from) + len(to)
	}
	indexed := time.Since(t0)

	t0 = time.Now()
	edges, err := s.Edges()
	if err != nil {
		t.Fatal(err)
	}
	type nb struct{ id, rel, dir string }
	adj := map[string][]nb{}
	for _, e := range edges {
		adj[e.Source] = append(adj[e.Source], nb{e.Target, e.Relation, "out"})
		adj[e.Target] = append(adj[e.Target], nb{e.Source, e.Relation, "in"})
	}
	full := 0
	for _, id := range picks {
		full += len(adj[id])
	}
	scan := time.Since(t0)

	t.Logf("nodes %d  edges %d", len(nodes), len(edges))
	t.Logf("INDEXED  %d lookups -> %d neighbours in %v", len(picks)*2, got, indexed)
	t.Logf("SCAN     full read + adjacency        in %v  (%d neighbours for the same ids)", scan, full)
	t.Logf("ratio    %.0fx", float64(scan)/float64(max(indexed, 1)))
}

func max(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
