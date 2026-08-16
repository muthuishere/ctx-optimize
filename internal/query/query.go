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
//
// `port` belongs here for exactly that reason: a boundary identifier is
// `api.openai.com` or `CONFIG_ENCRYPTION_KEY` — a whole external NAME, never a
// child of a parent scope. Omitting it meant every hostname (dots are what a
// hostname IS) took scoreNode's 5x child-declaration downrank, so a partial
// query like "openai" returned nine hits with the host among none of them. The
// exact-match tier (ADR 14 D1) rescued verbatim queries; this rescues the
// partial ones the tier cannot see.
var callableKind = map[string]bool{
	"function": true, "method": true, "class": true, "interface": true,
	"file": true, "module": true, "table": true, "document": true,
	"section": true, "topic": true, "port": true,
}

// normalizeExact is the ONLY normalization an exact match gets: trim and
// case-fold. Deliberately not more — no punctuation stripping, no stemming, no
// distance. An "exact" match that needed cleverness to be exact is a guess, and
// this tier exists precisely because it is the one case carrying certainty.
func normalizeExact(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// exactMatch reports whether the query names this node outright — its id, its
// label, or the external name a boundary port binds to.
//
// metadata.identifier is what makes this reach the boundary graph: a port's
// identifier IS the host / env var / binary being named (ADR 2026-08-13 D1),
// so `query "api.openai.com"` and `query "OPENAI_API_KEY"` become exact hits
// rather than token soup.
//
// Node ids are module-prefixed in a federated store (apps/api/port:…), so id
// equality alone would miss in a monorepo — label and identifier stay clean,
// which is why all three are checked.
// EqualFold, not ToLower: this runs once per node, and on the linux store that
// is 2.85M nodes x 3 fields. Lower-casing each field would allocate ~8.5M
// strings to answer a boolean — measured at +3.6% query latency before this
// was folded. EqualFold compares in place.
func exactMatch(n *schema.Node, normQ string) bool {
	if normQ == "" {
		return false
	}
	if strings.EqualFold(n.Label, normQ) || strings.EqualFold(n.ID, normQ) {
		return true
	}
	return strings.EqualFold(n.Metadata["identifier"], normQ)
}

// Run scores every node against the question and returns the top hits with
// their 1-hop neighborhoods, truncated to ~budget tokens (chars/4).
// Neighbors fetches the edges touching one node, in the order edges.ndjson
// lists them. Returning ok=false means "no index" and the caller falls back to
// the scan — which must be SAID, not silently absorbed.
//
// It exists because `query` read all 5.5M of linux's edges and built a
// whole-graph adjacency map on every call, 11M appends, to attach at most 12
// neighbours to at most 20 hits: measured 2.37s of a 3.93s verb. The same 37
// neighbours through the index card already uses cost 39.5ms.
type Neighbors interface {
	EdgesTouchingOrdered(id string) ([]schema.Edge, bool, error)
}

// Run answers from a fully-read graph. Kept as the fallback path and as the
// definition of the answer: RunIndexed must agree with it byte for byte.
func Run(nodes []schema.Node, edges []schema.Edge, question string, budget int) *Result {
	return run(nodes, edges, nil, question, budget)
}

// RunIndexed answers without reading the edges, fetching only the neighbours of
// the hits it is about to return. The ANSWER is identical — same hits, same
// order, same neighbours in the same order — because the index lane preserves
// file order (store.EdgesTouchingOrdered) and this is the only thing it
// changes. ADR 25 slice 0, invariant I1.
//
// nb == nil, or a store whose index is missing or stale, falls back to `edges`.
func RunIndexed(nodes []schema.Node, edges []schema.Edge, nb Neighbors, question string, budget int) *Result {
	return run(nodes, edges, nb, question, budget)
}

func run(nodes []schema.Node, edges []schema.Edge, nb Neighbors, question string, budget int) *Result {
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
	workers := runtime.GOMAXPROCS(0) // container-aware; NumCPU ignores cgroup quotas
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

	// tier 0 = the query NAMES this node (id/label/identifier); tier 1 = scored
	// lexically. A separate tier rather than a big score bonus because the bonus
	// has to out-weigh an unbounded sum of IDF terms to be correct, and "provably
	// larger than any future corpus" is not a thing you can assert — whereas an
	// ordering key is correct by construction. See ADR 2026-08-15-exact-match.
	type scored struct {
		idx   int
		tier  int
		score float64
	}
	normQ := normalizeExact(question)
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
				exact := exactMatch(&nodes[i], normQ)
				if s > 0 {
					s = intentAdjust(&nodes[i], s, wantsImports, wantsTests, wantsDocs)
				}
				// An exact match is a candidate even at score 0: the lexical pass
				// can zero it out (a label of nothing but stopwords, a node whose
				// tokens all sit in every document). Naming a node is evidence on
				// its own and must not depend on the token pass agreeing.
				if s > 0 || exact {
					t := 1
					if exact {
						t = 0
					}
					local = append(local, scored{i, t, s})
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
		if candidates[a].tier != candidates[b].tier {
			return candidates[a].tier < candidates[b].tier // named beats scored
		}
		if candidates[a].score != candidates[b].score {
			return candidates[a].score > candidates[b].score
		}
		return nodes[candidates[a].idx].ID < nodes[candidates[b].idx].ID // deterministic ties
	})
	// Lift exact matches above the lexical range for DISPLAY, so the printed
	// score stays monotonic with rank. Without this a reader sees a 0.30 sitting
	// above a 7.35 and reasonably concludes the ranking is broken. The offset is
	// derived from the data (max observed + 1), never a constant, and it is
	// applied after sorting so it cannot affect the order.
	if len(candidates) > 0 && candidates[0].tier == 0 {
		maxLex := 0.0
		for _, c := range candidates {
			if c.tier == 1 && c.score > maxLex {
				maxLex = c.score
			}
		}
		for i := range candidates {
			if candidates[i].tier == 0 {
				candidates[i].score += maxLex + 1
			}
		}
	}

	// Neighbourhoods. The indexed lane fetches only what the hits need; the
	// scanning lane builds the whole-graph map it always did. Both produce the
	// same list for the same node, in the same order.
	var out map[string][]Neighbor
	indexed := nb != nil
	if indexed {
		// Probe once on a node we will certainly ask about, so a missing or
		// stale index falls back BEFORE we start answering, not halfway.
		if len(candidates) > 0 {
			if _, ok, err := nb.EdgesTouchingOrdered(nodes[candidates[0].idx].ID); err != nil || !ok {
				indexed = false
			}
		} else {
			indexed = false
		}
	}
	if !indexed {
		out = map[string][]Neighbor{}
		for _, e := range edges {
			out[e.Source] = append(out[e.Source], Neighbor{ID: e.Target, Relation: e.Relation, Dir: "out"})
			out[e.Target] = append(out[e.Target], Neighbor{ID: e.Source, Relation: e.Relation, Dir: "in"})
		}
	}
	// neighborsOf reproduces exactly what the scan's map holds for one id: every
	// edge touching it in FILE order, a self-edge contributing both directions
	// with out first — which is the order the scan's two appends produce.
	neighborsOf := func(id string) []Neighbor {
		if !indexed {
			return out[id]
		}
		es, ok, err := nb.EdgesTouchingOrdered(id)
		if err != nil || !ok {
			return nil
		}
		list := make([]Neighbor, 0, len(es))
		for _, e := range es {
			if e.Source == id {
				list = append(list, Neighbor{ID: e.Target, Relation: e.Relation, Dir: "out"})
			}
			if e.Target == id {
				list = append(list, Neighbor{ID: e.Source, Relation: e.Relation, Dir: "in"})
			}
		}
		return list
	}

	res := &Result{Query: question}
	spent := 0
	for _, c := range candidates {
		n := nodes[c.idx]
		neighbors := neighborsOf(n.ID)
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
		//
		// Scan ALL matches and take the rarest; do NOT break on the first.
		// `for nt := range map` is randomized in Go, so breaking early made the
		// score depend on map iteration order whenever a node had two matching
		// tokens — measured: identical query, identical store, 17 hits on 8 runs
		// of 20 and 18 on the other 12. That is the ADR 5 lesson (an undefined
		// tie-break is a silent non-determinism) applied to ranking.
		//
		// Rarest = lowest df = the token that discriminates most, so the
		// deterministic choice is also the better answer. Ties on df fall back
		// to the token string, which is unique within the set.
		best, haveBest := "", false
		if len(qt) >= 3 {
			for nt := range nodeTokens[i] {
				if !strings.HasPrefix(nt, qt) && !strings.HasPrefix(qt, nt) {
					continue
				}
				if !haveBest || df[nt] < df[best] || (df[nt] == df[best] && nt < best) {
					best, haveBest = nt, true
				}
			}
		}
		if haveBest {
			s += 0.7 * (0.1 + math.Log(total/(1+float64(df[best]))))
			continue
		}
		if len(qt) < 5 {
			continue
		}
		// Trigram tier: typos and infix matches. Weakest tier. Same rule —
		// rarest match wins, never "whichever the map yielded first".
		qt3 := trigrams(qt, memo)
		best, haveBest = "", false
		for nt := range nodeTokens[i] {
			if len(nt) < 5 || dice(qt3, trigrams(nt, memo)) < 0.5 {
				continue
			}
			if !haveBest || df[nt] < df[best] || (df[nt] == df[best] && nt < best) {
				best, haveBest = nt, true
			}
		}
		if haveBest {
			s += 0.4 * (0.1 + math.Log(total/(1+float64(df[best]))))
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
//
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
	// ABOUT X. Measured on this repo: doc nodes are 39% of the graph (1,502
	// section + 304 document of 4,650) and took 15 of 30 top-3 slots across 10
	// code-intent queries, holding #1 for 5 of 10 — README.md above the
	// function being asked about. (Re-measured 2026-08-14 after ADR 4 swapped
	// the markdown producer to a goldmark AST; the earlier 1,315/278 of 3,963
	// counted 66 phantom sections parsed out of code fences, so the ratio was
	// right but the numbers were not reproducible.) Same shape as the
	// test/module demotes above:
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
