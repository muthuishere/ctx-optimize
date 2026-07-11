// Package query answers questions from the local store — lexical scoring
// (IDF-weighted token overlap) over node labels/sources, then 1-hop
// neighborhood expansion under a token budget. Deterministic, no embeddings,
// no model — the host agent supplies the semantics; we supply precise recall.
//
// S1e discipline: output is COMPLETE per hit (id, label, kind, source,
// location, neighbors) so the agent doesn't need a follow-up read to cite,
// and it is hard-capped by the budget — verbose stdout is a measured failure
// mode, not politeness.
package query

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

type Hit struct {
	Node      schema.Node `json:"node"`
	Score     float64     `json:"score"`
	Neighbors []Neighbor  `json:"neighbors,omitempty"`
}

type Neighbor struct {
	ID       string `json:"id"`
	Relation string `json:"relation"`
	Dir      string `json:"dir"` // out|in
}

type Result struct {
	Query string `json:"query"`
	Hits  []Hit  `json:"hits"`
}

var tokenRe = regexp.MustCompile(`[A-Za-z0-9]+`)

// Tokenize lower-cases and splits camelCase/snake_case — code identifiers are
// the corpus, so "BlkMqSubmitBio" must match "submit bio".
func Tokenize(s string) []string {
	// Split camelCase boundaries first.
	var sb strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			sb.WriteByte(' ')
		}
		sb.WriteRune(r)
	}
	var out []string
	for _, m := range tokenRe.FindAllString(strings.ToLower(sb.String()), -1) {
		if len(m) > 1 { // single chars are noise
			out = append(out, m)
		}
	}
	return out
}

// Run scores every node against the question and returns the top hits with
// their 1-hop neighborhoods, truncated to ~budget tokens (chars/4).
func Run(nodes []schema.Node, edges []schema.Edge, question string, budget int) *Result {
	if budget <= 0 {
		budget = 2000
	}
	qTokens := Tokenize(question)
	if len(qTokens) == 0 {
		return &Result{Query: question}
	}

	// Document frequency over node token sets → IDF. Rare tokens decide.
	df := map[string]int{}
	nodeTokens := make([]map[string]bool, len(nodes))
	for i, n := range nodes {
		set := map[string]bool{}
		for _, t := range Tokenize(n.Label + " " + n.Source) {
			set[t] = true
		}
		nodeTokens[i] = set
		for t := range set {
			df[t]++
		}
	}
	total := float64(len(nodes)) + 1

	type scored struct {
		idx   int
		score float64
	}
	var candidates []scored
	for i := range nodes {
		var s float64
		for _, qt := range qTokens {
			if nodeTokens[i][qt] {
				// Base weight keeps a match alive even when the token is in
				// every node (IDF→0 on uniform corpora); IDF still ranks.
				s += 0.1 + math.Log(total/(1+float64(df[qt])))
				continue
			}
			// Prefix tier (graphify-proven): "refund" ⇢ "refunds",
			// "serialize" ⇢ "serializer" — weaker than an exact hit.
			for nt := range nodeTokens[i] {
				if len(qt) >= 3 && (strings.HasPrefix(nt, qt) || strings.HasPrefix(qt, nt)) {
					s += 0.7 * (0.1 + math.Log(total/(1+float64(df[nt]))))
					break
				}
			}
		}
		if s > 0 {
			candidates = append(candidates, scored{i, s})
		}
	}
	sort.Slice(candidates, func(a, b int) bool {
		if candidates[a].score != candidates[b].score {
			return candidates[a].score > candidates[b].score
		}
		return nodes[candidates[a].idx].ID < nodes[candidates[b].idx].ID // deterministic ties
	})

	// Adjacency for neighborhoods.
	out := map[string][]Neighbor{}
	for _, e := range edges {
		out[e.Source] = append(out[e.Source], Neighbor{ID: e.Target, Relation: e.Relation, Dir: "out"})
		out[e.Target] = append(out[e.Target], Neighbor{ID: e.Source, Relation: e.Relation, Dir: "in"})
	}

	res := &Result{Query: question}
	spent := 0
	for _, c := range candidates {
		n := nodes[c.idx]
		neighbors := out[n.ID]
		if len(neighbors) > 12 { // hubs: cap, don't dump
			neighbors = neighbors[:12]
		}
		cost := estimateTokens(n, neighbors)
		if spent+cost > budget && len(res.Hits) > 0 {
			break
		}
		res.Hits = append(res.Hits, Hit{Node: n, Score: round2(c.score), Neighbors: neighbors})
		spent += cost
		if len(res.Hits) >= 20 {
			break
		}
	}
	return res
}

// Render is the human-readable form; --json callers marshal Result directly.
func Render(r *Result) string {
	if len(r.Hits) == 0 {
		return fmt.Sprintf("no matches for %q — try different terms, or `ctx-optimize add` more sources\n", r.Query)
	}
	var sb strings.Builder
	for _, h := range r.Hits {
		fmt.Fprintf(&sb, "%s  [%s]  %s %s\n", h.Node.Label, h.Node.Kind, h.Node.Source, h.Node.Location)
		for _, nb := range h.Neighbors {
			arrow := "→"
			if nb.Dir == "in" {
				arrow = "←"
			}
			fmt.Fprintf(&sb, "    %s %s %s\n", arrow, nb.Relation, nb.ID)
		}
	}
	return sb.String()
}

func estimateTokens(n schema.Node, neighbors []Neighbor) int {
	c := len(n.ID) + len(n.Label) + len(n.Source) + len(n.Location) + 16
	for _, nb := range neighbors {
		c += len(nb.ID) + len(nb.Relation) + 8
	}
	return c / 4
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
