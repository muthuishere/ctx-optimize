package main

// SPIKE: how much would STRUCTURAL EXPANSION (ADR 25 slice 4) actually add?
//
//	go test ./scripts/spikes/queryphases -run TestStructuralHeadroom -v \
//	  -store ~/ctxoptimize/linux -questions internal/golden/testdata/questions/linux-block.json
//
// Slice 4 lets a node become a result because the graph vouches for it. The
// number that justifies it is: how often is the RIGHT answer a 1-hop neighbour
// of something lexical already found, while not being found lexically itself?
//
// If that is near zero, expansion adds nothing the ranking did not already
// have and slice 4 is cost without benefit. If it is high, expansion reaches
// answers lexical search structurally cannot.

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/query"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

var qFile = flag.String("questions", "", "judged questions json")

type judged struct {
	Corpus    string `json:"corpus"`
	Questions []struct {
		ID        string   `json:"id"`
		Text      string   `json:"text"`
		Verb      string   `json:"verb"`
		Args      []string `json:"args"`
		ExpectAny []string `json:"expect_any"`
		K         int      `json:"k"`
	} `json:"questions"`
}

func TestStructuralHeadroom(t *testing.T) {
	if *storeDir == "" || *qFile == "" {
		t.Skip("need --store and --questions")
	}
	raw, err := os.ReadFile(*qFile)
	if err != nil {
		t.Fatal(err)
	}
	var j judged
	if err := json.Unmarshal(raw, &j); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Dir(*storeDir), filepath.Base(*storeDir))
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := s.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	edges, err := s.Edges()
	if err != nil {
		t.Fatal(err)
	}
	adj := map[string][]string{}
	for _, e := range edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
	}

	// Only questions this spike can honestly judge: the ones whose verb IS
	// query and that name an expected answer. The file also holds card, path
	// and affected questions — running query for those and scoring the miss
	// would measure the spike's own mistake, not the engine. First run did
	// exactly that and reported 15% lexical accuracy against a scoreboard
	// whose floor is 16.5/20.
	hitsAt, reachable, unreachable, skipped := 0, 0, 0, 0
	judgedN := 0
	for _, q := range j.Questions {
		if q.Verb != "query" || len(q.ExpectAny) == 0 {
			skipped++
			continue
		}
		judgedN++
		k := q.K
		if k == 0 {
			k = 5
		}
		res := query.Run(nodes, edges, strings.Join(q.Args, " "), 4000)
		top := map[string]bool{}
		var topIDs []string
		for i, h := range res.Hits {
			if i >= k {
				break
			}
			top[h.Node.Label] = true
			topIDs = append(topIDs, h.Node.ID)
		}
		found := false
		for _, want := range q.ExpectAny {
			if top[want] {
				found = true
			}
		}
		if found {
			hitsAt++
			continue
		}
		// Not in top-k lexically. Is it ONE HOP from something that was?
		byLabel := map[string]bool{}
		for _, id := range topIDs {
			for _, n := range adj[id] {
				byLabel[n] = true
			}
		}
		near := false
		for _, want := range q.ExpectAny {
			for id := range byLabel {
				if id == want || strings.HasSuffix(id, "::"+want) || strings.HasSuffix(id, "/"+want) {
					near = true
				}
			}
		}
		if near {
			reachable++
			t.Logf("  %s  MISSED lexically, REACHABLE in 1 hop   %q -> %v", q.ID, q.Text, q.ExpectAny)
		} else {
			unreachable++
			t.Logf("  %s  missed, not within 1 hop               %q -> %v", q.ID, q.Text, q.ExpectAny)
		}
	}
	n := judgedN
	t.Logf("")
	t.Logf("corpus %s — %d query questions judged (%d skipped: card/path/affected)", j.Corpus, n, skipped)
	t.Logf("  lexical top-k already correct : %d  (%.0f%%)", hitsAt, 100*float64(hitsAt)/float64(n))
	t.Logf("  MISSED but 1 hop away         : %d  (%.0f%%)  <- slice 4's ceiling", reachable, 100*float64(reachable)/float64(n))
	t.Logf("  missed and not reachable      : %d  (%.0f%%)", unreachable, 100*float64(unreachable)/float64(n))
}
