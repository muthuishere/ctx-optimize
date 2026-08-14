package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// Two gathers on ONE store used to destroy each other: writeNDJSON wrote a
// fixed "<path>.tmp", so the second writer truncated the first's temp, the
// first rename consumed it, and the loser's rename failed with ENOENT —
// aborting its lane and leaving a partial store (no manifest, no source
// record, no lookup index). Observed on a linux v6.9 gather 2026-08-13.
//
// Unique temp names make each writer's rename its own. Last writer wins; no
// writer errors, and the graph on disk is always one writer's complete output,
// never a torn mix.
func TestConcurrentReplaceNeverLosesItsTemp(t *testing.T) {
	dir := t.TempDir()
	const writers = 8

	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := Open(dir, "k")
			if err != nil {
				errs[i] = err
				return
			}
			b := &schema.Batch{Producer: "p", Nodes: []schema.Node{{
				ID: "n", Label: "n", Kind: "function", FileType: "code", Source: "a.go",
			}}}
			_, _, errs[i] = s.Replace(b, true)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d failed: %v", i, err)
		}
	}

	s, err := Open(dir, "k")
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := s.Nodes()
	if err != nil {
		t.Fatalf("graph unreadable after concurrent writes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("want 1 node after concurrent identical writes, got %d", len(nodes))
	}

	// No temp may survive a successful run — a leaked ".tmp-*" would be
	// fingerprinted-adjacent litter and would grow without bound.
	ents, err := os.ReadDir(filepath.Join(s.Dir, "graph"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}
