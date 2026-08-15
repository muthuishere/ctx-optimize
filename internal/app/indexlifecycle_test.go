package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/store"
)

// The lookup index must survive a gather that changed nothing (ADR
// 2026-08-15-index-dies-on-a-noop-gather).
//
// It did not. The header was keyed on the graph file's size+mtime and the
// rebuild guard on whether the node set moved; the store rewrites the graph on
// every gather, so a no-op gather bumped the mtime, the reader declared the
// index stale, and the guard declined to rebuild it. Nothing repaired it after
// that except `add --force`. Measured cost on the linux kernel: `card
// bio_split` 6ms -> 1,629ms, silently, for the rest of the store's life.
//
// This test is written against the SYMPTOM an agent pays for — after the second
// gather, does a lookup still take the fast path — not against the header
// format, so it stays a gate if the keying changes again.
func TestIndexSurvivesNoOpGather(t *testing.T) {
	repo, storeRoot, key := setupIncremental(t, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": baseMain,
	})
	mustIndex(t, storeRoot, key, "after the first gather")

	// A no-op gather: identical bytes, new mtime. That is enough to move the
	// tree signature, so the gather really runs (no short-circuit) and really
	// rewrites the graph — while adding zero nodes.
	mustWrite(t, repo, "main.go", baseMain)
	out := reAdd(t, repo, storeRoot)
	if strings.Contains(out, "unchanged") {
		t.Fatalf("precondition: the gather must actually run, not short-circuit: %s", out)
	}
	if !strings.Contains(out, "added 0 nodes") {
		t.Fatalf("precondition: this gather must add nothing: %s", out)
	}
	mustIndex(t, storeRoot, key, "after a gather that added 0 nodes")

	// And the answer itself must still come out of the store.
	if card, _ := runCLI(t, 0, "card", "Alpha", "--path", repo, "--store", storeRoot); !strings.Contains(card, "Alpha") {
		t.Fatalf("card lost its answer after the no-op gather: %s", card)
	}
}

// A missing or old-format index must be REPAIRED by an ordinary gather. Before
// ADR 18 only `add --force` rebuilt it, so every large store on the machine sat
// unindexed with nothing that would ever fix it.
func TestGatherRepairsAMissingIndex(t *testing.T) {
	repo, storeRoot, key := setupIncremental(t, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": baseMain,
	})
	idx := filepath.Join(storeRoot, key, "graph", "index")
	if err := removeAllIndex(idx); err != nil {
		t.Fatal(err)
	}
	if openStoreAt(t, storeRoot, key).IndexCurrent() {
		t.Fatal("precondition: the index should be gone")
	}
	mustWrite(t, repo, "main.go", baseMain) // no content change, just a gather
	reAdd(t, repo, storeRoot)
	mustIndex(t, storeRoot, key, "after a gather over a deleted index")
}

// The fail-safe property is the one that may never regress: an index that does
// not match the graph must never be used. Content keying must not soften that.
func TestIndexRefusedWhenGraphChangesUnderneath(t *testing.T) {
	_, storeRoot, key := setupIncremental(t, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": baseMain,
	})
	s := openStoreAt(t, storeRoot, key)
	nodes := filepath.Join(storeRoot, key, "graph", "nodes.ndjson")
	appendLine(t, nodes, `{"id":"x://tampered","label":"Tampered","kind":"function"}`)
	if s.IndexCurrent() {
		t.Fatal("an index must never be trusted over a graph it did not index")
	}
	if got := s.IndexState(); got != "stale" {
		t.Fatalf("status must call it stale, got %q", got)
	}
	// ...and the fallback still answers, from the graph as it now is.
	got, err := s.NodesByLabel("Tampered")
	if err != nil || len(got) != 1 {
		t.Fatalf("full-scan fallback must find the new node: %v %d", err, len(got))
	}
}

// `status` says which of the three states the index is in (ADR 18 D3) — a
// silent 270x fallback is what let the bug ship.
func TestStatusReportsIndexState(t *testing.T) {
	repo, storeRoot, key := setupIncremental(t, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": baseMain,
	})
	out, _ := runCLI(t, 0, "status", "--path", repo, "--store", storeRoot)
	if !strings.Contains(out, "index:") || !strings.Contains(out, "lookups use the index") {
		t.Fatalf("status must report a current index: %s", out)
	}
	if err := removeAllIndex(filepath.Join(storeRoot, key, "graph", "index")); err != nil {
		t.Fatal(err)
	}
	out, _ = runCLI(t, 0, "status", "--path", repo, "--store", storeRoot)
	if !strings.Contains(out, "full scan") {
		t.Fatalf("status must say a lookup now falls back: %s", out)
	}
	js, _ := runCLI(t, 0, "status", "--path", repo, "--store", storeRoot, "--json")
	if !strings.Contains(js, `"index": "absent"`) {
		t.Fatalf("--json must carry the index state: %s", js)
	}
}

func removeAllIndex(dir string) error { return os.RemoveAll(dir) }

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

func mustIndex(t *testing.T, storeRoot, key, when string) {
	t.Helper()
	if !openStoreAt(t, storeRoot, key).IndexCurrent() {
		t.Fatalf("the lookup index must still resolve %s — every lookup just fell back to a full scan", when)
	}
}

func openStoreAt(t *testing.T, storeRoot, key string) *store.Store {
	t.Helper()
	s, err := store.Open(storeRoot, key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
