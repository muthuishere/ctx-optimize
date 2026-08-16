// SPIKE: where does query's O(N) second actually go, and what would postings buy?
//
//	go run ./scripts/spikes/queryphases --store ~/ctxoptimize/linux --q "elevator hash add request"
//
// ADR 25 slice 1 assumes the cost is the SCAN and that postings fix it. Two
// things have to be true for that: the time must actually be in per-node work,
// and real 2-4 word agent queries must be selective enough that a postings
// intersection leaves few candidates. Neither was measured — this measures both
// before any index is built.
//
// It deliberately mirrors internal/query's phases rather than importing them,
// so the phase boundaries are visible. Any divergence in TOTAL from the real
// verb is itself a finding and is printed.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/query"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

func main() {
	dir := flag.String("store", "", "store dir holding graph/nodes.ndjson")
	qs := flag.String("q", "elevator hash add request", "queries, ; separated")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "need --store")
		os.Exit(2)
	}
	root, key := filepath.Dir(*dir), filepath.Base(*dir)
	s, err := store.Open(root, key)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	t0 := time.Now()
	nodes, err := s.Nodes()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	tRead := time.Since(t0)

	// ---- phase: tokenize every node + document frequency (what postings moves
	// to gather time)
	t0 = time.Now()
	nodeTokens := make([]map[string]bool, len(nodes))
	workers := runtime.GOMAXPROCS(0)
	shard := make([]map[string]int, workers)
	var wg sync.WaitGroup
	chunk := (len(nodes) + workers - 1) / workers
	for w := 0; w < workers; w++ {
		lo, hi := w*chunk, min((w+1)*chunk, len(nodes))
		if lo >= hi {
			shard[w] = map[string]int{}
			continue
		}
		wg.Add(1)
		go func(w, lo, hi int) {
			defer wg.Done()
			local := map[string]int{}
			for i := lo; i < hi; i++ {
				set := map[string]bool{}
				for _, t := range query.Tokenize(nodes[i].Label + " " + nodes[i].Source) {
					set[t] = true
				}
				nodeTokens[i] = set
				for t := range set {
					local[t]++
				}
			}
			shard[w] = local
		}(w, lo, hi)
	}
	wg.Wait()
	df := map[string]int{}
	for _, m := range shard {
		for t, n := range m {
			df[t] += n
		}
	}
	tTok := time.Since(t0)

	// ---- vocabulary shape: what an index would have to hold
	postings := 0
	for _, n := range df {
		postings += n
	}
	var dfs []int
	for _, n := range df {
		dfs = append(dfs, n)
	}
	sort.Ints(dfs)

	fmt.Printf("store            %s\n", *dir)
	fmt.Printf("nodes            %d\n", len(nodes))
	fmt.Printf("vocabulary       %d distinct tokens\n", len(df))
	fmt.Printf("postings total   %d (node,token) pairs  ~%.1f MB as uint32\n",
		postings, float64(postings*4)/(1<<20))
	fmt.Printf("df median        %d   p90 %d   max %d\n",
		dfs[len(dfs)/2], dfs[len(dfs)*9/10], dfs[len(dfs)-1])
	fmt.Println()
	fmt.Printf("PHASE read nodes %8.3fs\n", tRead.Seconds())
	fmt.Printf("PHASE tokenize+df%8.3fs   <- what postings moves to gather\n", tTok.Seconds())

	// ---- the phases postings does NOT touch. Slice 1 assumes the lexical scan
	// is the cost; if the edges and their adjacency dominate, an index over
	// nodes cannot fix the verb.
	t0 = time.Now()
	edges, err := s.Edges()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	tEdgeRead := time.Since(t0)

	t0 = time.Now()
	type nb struct {
		ID, Relation, Dir string
	}
	out := map[string][]nb{}
	for _, e := range edges {
		out[e.Source] = append(out[e.Source], nb{e.Target, e.Relation, "out"})
		out[e.Target] = append(out[e.Target], nb{e.Source, e.Relation, "in"})
	}
	tAdj := time.Since(t0)

	fmt.Printf("PHASE read edges %8.3fs   <- postings does NOT touch this\n", tEdgeRead.Seconds())
	fmt.Printf("PHASE adjacency  %8.3fs   <- nor this (%d edges, %d keys)\n",
		tAdj.Seconds(), len(edges), len(out))
	fmt.Printf("SUBTOTAL         %8.3fs   of which postings addresses %.0f%%\n",
		(tRead + tTok + tEdgeRead + tAdj).Seconds(),
		100*tTok.Seconds()/(tRead+tTok+tEdgeRead+tAdj).Seconds())

	// ---- per query: selectivity, then the scoring phase
	fmt.Println()
	fmt.Printf("%-34s %10s %10s %9s %9s\n", "query", "union", "intersect", "score", "of nodes")
	fmt.Println(strings.Repeat("-", 78))
	for _, q := range strings.Split(*qs, ";") {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		qt := query.Tokenize(q)
		union, inter := map[int]bool{}, map[int]bool{}
		for i := range nodes {
			hitAll, hitAny := len(qt) > 0, false
			for _, t := range qt {
				if nodeTokens[i][t] {
					hitAny = true
				} else {
					hitAll = false
				}
			}
			if hitAny {
				union[i] = true
			}
			if hitAll {
				inter[i] = true
			}
		}
		// scoring cost over the FULL set, which is what today does
		t0 = time.Now()
		total := float64(len(nodes))
		var sink float64
		for i := range nodes {
			for _, t := range qt {
				if nodeTokens[i][t] {
					sink += 0.1 + math.Log(total/(1+float64(df[t])))
				}
			}
		}
		tScore := time.Since(t0)
		_ = sink
		fmt.Printf("%-34s %10d %10d %8.3fs %8.2f%%\n",
			trunc(q, 34), len(union), len(inter), tScore.Seconds(),
			100*float64(len(union))/float64(len(nodes)))
	}
	fmt.Println()
	fmt.Println("union     = candidates a postings OR would produce")
	fmt.Println("intersect = candidates a postings AND would produce")
	fmt.Println("of nodes  = union as a share of the corpus; the smaller, the more postings buys")
	_ = schema.Node{}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
