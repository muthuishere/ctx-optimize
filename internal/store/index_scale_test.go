package store

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
)

// Scalability guard for the lookup index (ADR 2026-08-05-query-at-scale).
//
// Env-gated like every integration test here — a runtime skip, never a build
// tag. Point it at a real large store and it reports build time, peak heap and
// lookup latency:
//
//	CTX_OPTIMIZE_TEST_BIGSTORE=~/ctxoptimize/linux go test ./internal/store/ -run Scale -v
//
// Why this exists: BuildIndex reads the whole ndjson file and holds a
// key -> []offset map while building. Both grow with the graph, so the honest
// question is not "is it fast" but "what does it cost at 2.85M nodes, and does
// anything here grow in a way that stops working at 10x that". A design that is
// 32,000x faster and OOMs on chromium is not an improvement.
func TestScaleIndexBuildCost(t *testing.T) {
	dir := os.Getenv("CTX_OPTIMIZE_TEST_BIGSTORE")
	if dir == "" {
		t.Skip("set CTX_OPTIMIZE_TEST_BIGSTORE=<store dir> to measure index cost on a real large store")
	}
	s := &Store{Dir: dir}

	if _, err := os.Stat(s.nodesPath()); err != nil {
		t.Skipf("no graph at %s: %v", s.nodesPath(), err)
	}
	nst, _ := os.Stat(s.nodesPath())
	est, err := os.Stat(s.edgesPath())
	var edgeBytes int64
	if err == nil {
		edgeBytes = est.Size()
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	t0 := time.Now()
	if err := s.BuildIndex(); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	build := time.Since(t0)

	runtime.ReadMemStats(&after)
	peakHeap := float64(after.TotalAlloc-before.TotalAlloc) / 1e9
	sysPeak := float64(after.Sys) / 1e9

	var idxBytes int64
	for _, n := range []string{labelsIndex, edgesBySrc, edgesByTgt} {
		if st, err := os.Stat(dirJoin(s.indexDir(), n)); err == nil {
			idxBytes += st.Size()
		}
	}
	graphBytes := nst.Size() + edgeBytes

	t.Logf("graph      %.2f GB", float64(graphBytes)/1e9)
	t.Logf("index      %.2f GB (%.0f%% of graph)", float64(idxBytes)/1e9, 100*float64(idxBytes)/float64(graphBytes))
	t.Logf("build      %.2fs", build.Seconds())
	t.Logf("allocated  %.2f GB total during build", peakHeap)
	t.Logf("heap sys   %.2f GB peak", sysPeak)

	if !s.IndexCurrent() {
		t.Error("index reports stale immediately after a build — the fail-safe header is wrong")
	}

	// The index must not approach the size of the thing it indexes; past that
	// the trade stops being worth it and we are just CodeGraph with extra steps
	// (their index is 54% of a 4.1GB DB).
	if idxBytes > graphBytes {
		t.Errorf("index (%.2f GB) is LARGER than the graph (%.2f GB) — the trade has inverted", float64(idxBytes)/1e9, float64(graphBytes)/1e9)
	}
}

// Lookup latency must be flat in the size of the graph — that is the whole
// claim. If it tracks graph size, the binary search is not doing its job.
func TestScaleLookupLatency(t *testing.T) {
	dir := os.Getenv("CTX_OPTIMIZE_TEST_BIGSTORE")
	if dir == "" {
		t.Skip("set CTX_OPTIMIZE_TEST_BIGSTORE=<store dir> to measure lookup latency")
	}
	s := &Store{Dir: dir}
	if !s.IndexCurrent() {
		t.Skip("index not current for this store — run TestScaleIndexBuildCost first")
	}

	nodes, err := s.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Skip("empty store")
	}

	// Sample labels spread across the whole store so the search cannot be
	// flattered by locality.
	const samples = 200
	step := len(nodes) / samples
	if step < 1 {
		step = 1
	}
	var picked []string
	for i := 0; i < len(nodes) && len(picked) < samples; i += step {
		if nodes[i].Label != "" {
			picked = append(picked, nodes[i].Label)
		}
	}

	t0 := time.Now()
	misses := 0
	for _, l := range picked {
		got, err := s.NodesByLabel(l)
		if err != nil {
			t.Fatalf("NodesByLabel(%q): %v", l, err)
		}
		if len(got) == 0 {
			misses++
		}
	}
	per := time.Since(t0) / time.Duration(len(picked))

	t.Logf("%d nodes in store", len(nodes))
	t.Logf("%d lookups, %v each", len(picked), per)

	// Every sampled label came OUT of the store, so every one must be found.
	// A miss here is the silent under-report this whole design exists to avoid.
	if misses > 0 {
		t.Errorf("%d of %d labels taken FROM the store were not found via the index — silent data loss", misses, len(picked))
	}
	if per > 50*time.Millisecond {
		t.Errorf("lookup averaged %v — an indexed lookup should be sub-millisecond; it is scanning", per)
	}
}

func dirJoin(a, b string) string { return fmt.Sprintf("%s/%s", a, b) }
