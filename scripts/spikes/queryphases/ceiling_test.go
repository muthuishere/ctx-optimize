package main

// SPIKE: what is the FLOOR for query if every phase used an index that already
// exists? Not "can it be O(1)" — ranking must see every candidate, so it can't
// — but how far below O(N) does the existing machinery already reach?
//
//	go test ./scripts/spikes/queryphases -run TestIndexedCeiling -v -store ~/ctxoptimize/linux

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/store"
)

func TestIndexedCeiling(t *testing.T) {
	if *storeDir == "" {
		t.Skip("no --store")
	}
	s, err := store.Open(filepath.Dir(*storeDir), filepath.Base(*storeDir))
	if err != nil {
		t.Fatal(err)
	}

	// Baseline: the full parse query does today, just to hold the ids.
	t0 := time.Now()
	nodes, err := s.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	fullRead := time.Since(t0)

	picks := make([]string, 0, 20)
	for i := 0; i < len(nodes) && len(picks) < 20; i += len(nodes) / 25 {
		picks = append(picks, nodes[i].ID)
	}

	// Hydrating ONLY the hits, by id, through the index card already uses.
	t0 = time.Now()
	got := 0
	for _, id := range picks {
		n, err := s.NodeByID(id)
		if err != nil {
			t.Fatal(err)
		}
		if n != nil {
			got++
		}
	}
	byID := time.Since(t0)

	t.Logf("nodes in store          %d", len(nodes))
	t.Logf("FULL Nodes() parse      %v      <- phase 1 today", fullRead)
	t.Logf("NodeByID x%d (the hits)  %v      <- phase 1 if hits are hydrated lazily", len(picks), byID)
	t.Logf("ratio                   %.0fx", float64(fullRead)/float64(byID))
}
