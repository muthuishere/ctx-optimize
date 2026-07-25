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
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

type Hit struct {
	Node      schema.Node `json:"node"`
	Score     float64     `json:"score"`
	Neighbors []Neighbor  `json:"neighbors,omitempty"`

	// Content/ContentError are populated by the CALLER (internal/app), not
	// Run — the content-hydration spike (openspec/changes/2026-07-24-
	// content-hydration): opt-in `--include-content` reads each hit's
	// verbatim source body from the file at answer time, keeping this
	// engine pure (no file I/O) and the default pointer-only output
	// byte-identical. ContentError is set instead of Content when the file
	// can't be read or the node has no location — never fails the query.
	Content      string `json:"content,omitempty"`
	ContentError string `json:"content_error,omitempty"`
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
// the corpus, so "BlkMqSubmitBio" must match "submit bio" and "HTTPServer"
// must yield both "http" and "server" (acronym runs stay whole).
func Tokenize(s string) []string {
	// Boundary before an uppercase rune when the previous rune is not
	// uppercase, or when it starts a new word after an acronym run
	// (…P│Server). ASCII scan is fine — identifiers are the corpus.
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' && i > 0 {
			prevUpper := s[i-1] >= 'A' && s[i-1] <= 'Z'
			nextLower := i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z'
			if !prevUpper || nextLower {
				sb.WriteByte(' ')
			}
		}
		sb.WriteByte(c)
	}
	var out []string
	for _, m := range tokenRe.FindAllString(strings.ToLower(sb.String()), -1) {
		if len(m) > 1 { // single chars are noise
			out = append(out, m)
		}
	}
	return out
}

// questionStopwords are the grammar of an English question. They must be
// dropped from the QUERY, never from node tokens — IDF makes them actively
// harmful here. A word like "on" is rare as an IDENTIFIER token (df=49 in a
// 3,963-node corpus → idf 4.37, higher than "name" at 4.41), so IDF reads
// question grammar as a strong discriminator: measured, "prune stale nodes on
// add" answered `install.go::OnPath` — matched on the word "on" — while
// store.Nodes sat at rank 9.
//
// Kept deliberately small: only words that cannot be a meaningful search term
// on their own. "get", "set", "new", "run" and friends are NOT here — they are
// real identifier prefixes, and dropping them would break `query "get user"`.
var questionStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true,
	"into": true, "onto": true, "that": true, "this": true, "these": true,
	"those": true, "what": true, "which": true, "where": true, "when": true,
	"how": true, "why": true, "who": true, "does": true, "did": true,
	"can": true, "should": true, "would": true, "there": true, "here": true,
	"about": true, "over": true, "under": true, "than": true, "then": true,
	"its": true, "our": true, "your": true, "their": true,
	"on": true, "in": true, "at": true, "to": true, "of": true, "by": true,
	"is": true, "are": true, "was": true, "be": true, "do": true, "it": true,
	"as": true, "an": true, "or": true, "if": true, "we": true, "my": true,
	"me": true, "you": true, "all": true, "any": true, "not": true,
}

// dropStopwords removes question grammar, but never empties the query: if a
// question is nothing BUT stopwords, the caller still deserves the literal
// search it asked for rather than zero hits.
func dropStopwords(qTokens []string) []string {
	kept := make([]string, 0, len(qTokens))
	for _, t := range qTokens {
		if !questionStopwords[t] {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		return qTokens
	}
	return kept
}

// callableKind marks kinds whose dotted labels are real symbols, not child
// declarations of a parent scope.
var callableKind = map[string]bool{
	"function": true, "method": true, "class": true, "interface": true,
	"file": true, "module": true, "table": true, "document": true,
	"section": true, "topic": true,
}

// Run scores every node against the question and returns the top hits with
// their 1-hop neighborhoods, truncated to ~budget tokens (chars/4).
func Run(nodes []schema.Node, edges []schema.Edge, question string, budget int) *Result {
	if budget <= 0 {
		budget = 2000
	}
	qTokens := dropStopwords(Tokenize(question))
	if len(qTokens) == 0 {
		return &Result{Query: question}
	}

	// Document frequency over node token sets → IDF. Rare tokens decide.
	// Tokenizing 275k nodes single-threaded measured ~500ms — shard it.
	nodeTokens := make([]map[string]bool, len(nodes))
	workers := runtime.NumCPU()
	shardDF := make([]map[string]int, workers)
	var wg sync.WaitGroup
	chunk := (len(nodes) + workers - 1) / workers
	for w := 0; w < workers; w++ {
		lo := w * chunk
		hi := lo + chunk
		if hi > len(nodes) {
			hi = len(nodes)
		}
		if lo >= hi {
			shardDF[w] = map[string]int{}
			continue
		}
		wg.Add(1)
		go func(w, lo, hi int) {
			defer wg.Done()
			local := map[string]int{}
			for i := lo; i < hi; i++ {
				set := map[string]bool{}
				for _, t := range Tokenize(nodes[i].Label + " " + nodes[i].Source) {
					set[t] = true
				}
				nodeTokens[i] = set
				for t := range set {
					local[t]++
				}
			}
			shardDF[w] = local
		}(w, lo, hi)
	}
	wg.Wait()
	df := map[string]int{}
	for _, local := range shardDF {
		for t, c := range local {
			df[t] += c
		}
	}
	total := float64(len(nodes)) + 1

	type scored struct {
		idx   int
		score float64
	}
	wantsTests, wantsImports := testIntent(qTokens), importIntent(qTokens)
	wantsDocs := docIntent(qTokens)
	shardCand := make([][]scored, workers)
	for w := 0; w < workers; w++ {
		lo := w * chunk
		hi := lo + chunk
		if hi > len(nodes) {
			hi = len(nodes)
		}
		if lo >= hi {
			continue
		}
		wg.Add(1)
		go func(w, lo, hi int) {
			defer wg.Done()
			var local []scored
			memo := map[string]map[string]bool{}
			for i := lo; i < hi; i++ {
				s := scoreNode(nodes, nodeTokens, qTokens, df, total, memo, i)
				if s > 0 {
					s = intentAdjust(&nodes[i], s, wantsImports, wantsTests, wantsDocs)
					local = append(local, scored{i, s})
				}
			}
			shardCand[w] = local
		}(w, lo, hi)
	}
	wg.Wait()
	var candidates []scored
	for _, local := range shardCand {
		candidates = append(candidates, local...)
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

// scoreNode applies the three match tiers (exact-IDF, prefix, trigram —
// graphify-proven weights) plus the child-declaration downrank (proof D1:
// dotted-label data nodes inherit their parent's tokens and bury the real
// symbol; callable kinds keep dotted labels — Store.Merge is first-class).
func scoreNode(nodes []schema.Node, nodeTokens []map[string]bool, qTokens []string, df map[string]int, total float64, memo map[string]map[string]bool, i int) float64 {
	var s float64
	for _, qt := range qTokens {
		if nodeTokens[i][qt] {
			// Base weight keeps a match alive even when the token is in
			// every node (IDF→0 on uniform corpora); IDF still ranks.
			s += 0.1 + math.Log(total/(1+float64(df[qt])))
			continue
		}
		// Prefix tier: "refund" ⇢ "refunds" — weaker than an exact hit.
		matched := false
		for nt := range nodeTokens[i] {
			if len(qt) >= 3 && (strings.HasPrefix(nt, qt) || strings.HasPrefix(qt, nt)) {
				s += 0.7 * (0.1 + math.Log(total/(1+float64(df[nt]))))
				matched = true
				break
			}
		}
		if matched || len(qt) < 5 {
			continue
		}
		// Trigram tier: typos and infix matches. Weakest tier.
		qt3 := trigrams(qt, memo)
		for nt := range nodeTokens[i] {
			if len(nt) < 5 {
				continue
			}
			if dice(qt3, trigrams(nt, memo)) >= 0.5 {
				s += 0.4 * (0.1 + math.Log(total/(1+float64(df[nt]))))
				break
			}
		}
	}
	if s > 0 && strings.ContainsRune(nodes[i].Label, '.') && !callableKind[nodes[i].Kind] {
		s *= 0.2
	}
	return s
}

// Intent-aware downranks (ADR 2026-07-24-answer-quality F1/F2), applied after
// scoreNode so the shard loop stays a pure token pass:
//   - module:// import stubs carry no signature/body/file:line — they must
//     never outrank the definition (measured: `card url_for` returned the stub,
//     judge 0.66). Downranked UNLESS the question is about imports/modules.
//   - test files out-token the definition ("url_for" appears more in test
//     names than in helpers.py) — demoted UNLESS the question mentions tests.
// docDemote scales prose nodes when the question is not about prose. Measured,
// not guessed — see the sweep in TestDocDemoteChosenByMeasurement.
var docDemote = 0.5

func intentAdjust(n *schema.Node, s float64, wantsImports, wantsTests, wantsDocs bool) float64 {
	if !wantsImports && strings.HasPrefix(n.ID, "module://") {
		s *= 0.25
	}
	if !wantsTests && isTestSource(n.Source) {
		s *= 0.5
	}
	// Prose repeats a question's words far more often than code does, so on a
	// docs-heavy repo lexical scoring hands "where is X implemented" the ADR
	// ABOUT X. Measured on this repo: doc nodes are 40% of the graph (1,315
	// section + 278 document of 3,963) and took 15 of 30 top-3 slots across 10
	// code-intent queries, holding #1 for 5 of 10 — README.md above the
	// function being asked about. Same shape as the test/module demotes above:
	// a doc node still wins when it is genuinely the best answer, and asking
	// about docs turns the demote off entirely.
	if !wantsDocs && (n.Kind == "section" || n.Kind == "document") {
		s *= docDemote
	}
	return s
}

// isTestSource marks nodes whose source file is test-only, across the six
// bench languages: tests/ dirs, test_*.py, *_test.go, *.spec.ts, *.test.ts,
// *Test.java / *Tests.cs.
func isTestSource(src string) bool {
	if src == "" {
		return false
	}
	s := strings.ToLower(filepath.ToSlash(src))
	if strings.HasPrefix(s, "tests/") || strings.Contains(s, "/tests/") ||
		strings.HasPrefix(s, "test/") || strings.Contains(s, "/test/") {
		return true
	}
	base := s[strings.LastIndexByte(s, '/')+1:]
	if strings.HasPrefix(base, "test_") || strings.Contains(base, ".spec.") || strings.Contains(base, ".test.") {
		return true
	}
	if strings.HasSuffix(strings.TrimSuffix(base, filepath.Ext(base)), "_test") {
		return true
	}
	// Java/C# convention is a CamelCase class ending in Test/Tests
	// (FooTest.java) — match on the ORIGINAL case so "attest.cs" stays clean.
	orig := filepath.ToSlash(src)
	origBase := orig[strings.LastIndexByte(orig, '/')+1:]
	origStem := strings.TrimSuffix(origBase, filepath.Ext(origBase))
	if strings.HasSuffix(base, ".java") || strings.HasSuffix(base, ".cs") {
		return strings.HasSuffix(origStem, "Test") || strings.HasSuffix(origStem, "Tests")
	}
	return false
}

// testIntent / importIntent: does the question itself ask about tests or
// imports? Then the demote must not fire (Q2/Q3 decisions).
func testIntent(qTokens []string) bool {
	for _, t := range qTokens {
		if t == "test" || t == "tests" || t == "testing" || t == "spec" {
			return true
		}
	}
	return false
}

// docIntent: is the question ABOUT prose — a doc, spec, ADR, changelog? Then
// the demote must not fire, exactly as testIntent guards the test demote.
func docIntent(qTokens []string) bool {
	for _, t := range qTokens {
		switch t {
		case "doc", "docs", "documentation", "documented", "readme", "changelog",
			"adr", "spec", "specs", "proposal", "design", "guide", "wiki",
			"rationale", "decision", "openspec":
			return true
		}
	}
	return false
}

func importIntent(qTokens []string) bool {
	for _, t := range qTokens {
		if t == "import" || t == "imports" || t == "imported" || t == "module" {
			return true
		}
	}
	return false
}

// Render is the human-readable form; --json callers marshal Result directly.
func Render(r *Result) string {
	if len(r.Hits) == 0 {
		return fmt.Sprintf("no matches for %q — try different terms, or `ctx-optimize add` more sources\n", r.Query)
	}
	var sb strings.Builder
	for _, h := range r.Hits {
		fmt.Fprintf(&sb, "%s  [%s]  %s %s\n", h.Node.Label, h.Node.Kind, h.Node.Source, h.Node.Location)
		if sig := h.Node.Metadata["signature"]; sig != "" {
			fmt.Fprintf(&sb, "    sig: %s\n", sig)
		}
		for _, nb := range h.Neighbors {
			arrow := "→"
			if nb.Dir == "in" {
				arrow = "←"
			}
			fmt.Fprintf(&sb, "    %s %s %s\n", arrow, nb.Relation, nb.ID)
		}
		if h.Content != "" {
			sb.WriteString("    content:\n")
			for _, line := range strings.Split(h.Content, "\n") {
				fmt.Fprintf(&sb, "      %s\n", line)
			}
		} else if h.ContentError != "" {
			fmt.Fprintf(&sb, "    content: <unavailable — %s>\n", h.ContentError)
		}
	}
	return sb.String()
}

// trigrams memoizes per query run (memo local to Run — the dashboard serves
// concurrent queries, so no shared mutable state).
func trigrams(s string, memo map[string]map[string]bool) map[string]bool {
	if t, ok := memo[s]; ok {
		return t
	}
	t := map[string]bool{}
	for i := 0; i+3 <= len(s); i++ {
		t[s[i:i+3]] = true
	}
	memo[s] = t
	return t
}

// dice is the Sørensen–Dice coefficient over trigram sets.
func dice(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	small, big := a, b
	if len(b) < len(a) {
		small, big = b, a
	}
	inter := 0
	for t := range small {
		if big[t] {
			inter++
		}
	}
	return 2 * float64(inter) / float64(len(a)+len(b))
}

func estimateTokens(n schema.Node, neighbors []Neighbor) int {
	c := len(n.ID) + len(n.Label) + len(n.Source) + len(n.Location) + len(n.Metadata["signature"]) + 16
	for _, nb := range neighbors {
		c += len(nb.ID) + len(nb.Relation) + 8
	}
	return c / 4
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
