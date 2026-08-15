package query

import (
	"fmt"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// exactCorpus mirrors the shape that produced the ADR 14 defect: a boundary
// port whose identifier is a dotted host, surrounded by code nodes that share
// ONE of its tokens. Before the exact tier, the host lost to every one of them
// — it splits to api/openai/com, camelCase neighbours matched two of those, and
// the dotted-label downrank then multiplied the real answer by 0.2.
func exactCorpus() []schema.Node {
	return []schema.Node{
		{ID: "port:network.http:>api.openai.com", Label: "api.openai.com", Kind: "port",
			FileType: "boundary", Source: "port://network.http/api.openai.com",
			Metadata: map[string]string{"identifier": "api.openai.com", "transport": "network.http", "direction": "consumes"}},
		{ID: "internal/x/openapi.go::buildOpenAPIBatch", Label: "buildOpenAPIBatch", Kind: "function",
			FileType: "code", Source: "internal/x/openapi.go", Location: "L10-L20"},
		{ID: "internal/x/openapi.go::fetchOpenAPI", Label: "fetchOpenAPI", Kind: "function",
			FileType: "code", Source: "internal/x/openapi.go", Location: "L30-L40"},
		{ID: "internal/y/api.go", Label: "api.go", Kind: "file", FileType: "code", Source: "internal/y/api.go"},
		{ID: "port:config.env:>OPENAI_API_KEY", Label: "OPENAI_API_KEY", Kind: "port",
			FileType: "boundary", Source: "port://config.env/OPENAI_API_KEY",
			Metadata: map[string]string{"identifier": "OPENAI_API_KEY", "transport": "config.env", "direction": "consumes", "sensitive": "true"}},
	}
}

// The defect, pinned: naming a node must put it first. Mutation check — delete
// the tier comparison in Run's sort and this fails with buildOpenAPIBatch (or
// another token-sharer) at rank 1.
func TestExactMatchRanksFirst(t *testing.T) {
	nodes := exactCorpus()
	for _, q := range []string{
		"api.openai.com",  // dotted host: the ADR 14 reproduction
		"API.OpenAI.COM",  // case-folded
		" api.openai.com", // trimmed
	} {
		got := Run(nodes, nil, q, 2000)
		if len(got.Hits) == 0 {
			t.Fatalf("%q: no hits at all", q)
		}
		if id := got.Hits[0].Node.ID; id != "port:network.http:>api.openai.com" {
			t.Errorf("%q: rank 1 = %s, want the port whose identifier IS the query", q, id)
		}
	}
}

// metadata.identifier is the field that reaches the boundary graph — an env var
// is not in any label a code query would produce.
func TestExactMatchOnIdentifierAndLabel(t *testing.T) {
	nodes := exactCorpus()
	for _, tc := range []struct{ q, want string }{
		{"OPENAI_API_KEY", "port:config.env:>OPENAI_API_KEY"},
		{"openai_api_key", "port:config.env:>OPENAI_API_KEY"}, // case-folded
		{"buildOpenAPIBatch", "internal/x/openapi.go::buildOpenAPIBatch"},
		{"internal/x/openapi.go::fetchOpenAPI", "internal/x/openapi.go::fetchOpenAPI"}, // by id
	} {
		got := Run(nodes, nil, tc.q, 2000)
		if len(got.Hits) == 0 {
			t.Fatalf("%q: no hits", tc.q)
		}
		if id := got.Hits[0].Node.ID; id != tc.want {
			t.Errorf("%q: rank 1 = %s, want %s", tc.q, id, tc.want)
		}
	}
}

// A near-miss must NOT be promoted. The tier carries certainty; if a substring
// or a prefix could enter it, it would be a heuristic wearing certainty's
// clothes and the ranking would be a guess again.
func TestNearMissDoesNotEnterTheExactTier(t *testing.T) {
	nodes := exactCorpus()
	for _, q := range []string{
		"api.openai.co",    // one char short
		"api.openai.comm",  // one char long
		"openai.com",       // suffix only
		"api.openai",       // prefix only
		"api openai com",   // tokens, not the string
		"xapi.openai.comx", // contains it
	} {
		got := Run(nodes, nil, q, 2000)
		if len(got.Hits) > 0 && got.Hits[0].Node.ID == "port:network.http:>api.openai.com" {
			// Only a problem if it won BECAUSE of the tier — lexical scoring may
			// legitimately rank it first. Distinguish by checking the runner-up
			// gap is not the synthetic display offset.
			if len(got.Hits) > 1 && got.Hits[0].Score-got.Hits[1].Score > 1 {
				t.Errorf("%q: promoted to the exact tier — near-misses must be scored, not named", q)
			}
		}
	}
}

// Ties were the deeper bug: seven candidates at 1.51 meant the scorer had no
// opinion and sort order decided. Whatever the scores, two runs must agree.
func TestRankingIsDeterministicAcrossRuns(t *testing.T) {
	nodes := exactCorpus()
	for _, q := range []string{"api.openai.com", "api", "openai"} {
		first := Run(nodes, nil, q, 2000)
		for i := 0; i < 5; i++ {
			again := Run(nodes, nil, q, 2000)
			if len(first.Hits) != len(again.Hits) {
				t.Fatalf("%q: hit count varies between runs", q)
			}
			for j := range first.Hits {
				if first.Hits[j].Node.ID != again.Hits[j].Node.ID {
					t.Fatalf("%q: rank %d varies between runs: %s vs %s",
						q, j+1, first.Hits[j].Node.ID, again.Hits[j].Node.ID)
				}
			}
		}
	}
}

// The display offset must not reorder anything — it is cosmetic, applied after
// the sort. Pinned because a future refactor that folds it into scoring would
// reintroduce the "big bonus must beat an unbounded IDF sum" problem the tier
// exists to avoid.
func TestDisplayScoreStaysMonotonicWithRank(t *testing.T) {
	nodes := exactCorpus()
	got := Run(nodes, nil, "api.openai.com", 2000)
	for i := 1; i < len(got.Hits); i++ {
		if got.Hits[i].Score > got.Hits[i-1].Score {
			t.Errorf("rank %d scores %.2f, above rank %d at %.2f — printed score must not contradict rank",
				i+1, got.Hits[i].Score, i, got.Hits[i-1].Score)
		}
	}
}

// An exact match must survive a zero lexical score: naming a node is evidence
// on its own, and the token pass can legitimately zero out (label made only of
// stopwords, or tokens present in every node so IDF collapses).
func TestExactMatchSurvivesZeroLexicalScore(t *testing.T) {
	nodes := []schema.Node{
		{ID: "port:process.exec:>git", Label: "git", Kind: "port", FileType: "boundary",
			Source: "port://process.exec/git", Metadata: map[string]string{"identifier": "git"}},
		{ID: "a.go::Foo", Label: "Foo", Kind: "function", FileType: "code", Source: "a.go"},
	}
	got := Run(nodes, nil, "git", 2000)
	if len(got.Hits) == 0 || got.Hits[0].Node.ID != "port:process.exec:>git" {
		t.Fatalf("exact match on a short label did not rank first: %+v", got.Hits)
	}
}

// The pre-existing non-determinism ADR 14's D1 constraint required fixing: the
// prefix and trigram tiers iterated a MAP and broke on the first match, so when
// a node carried two tokens that both matched, which one contributed its IDF
// was decided by Go's randomized map order.
//
// Measured on the graphify corpus before the fix: the SAME binary, SAME store
// and SAME query returned 17 hits on 8 runs of 20 and 18 on the other 12.
//
// This fixture forces the ambiguity — "conf" prefix-matches BOTH "config" and
// "configure" on the same node, and the two tokens have different df because
// other nodes carry only one of them.
func TestPrefixTieIsDeterministicNotMapOrder(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a.go::configConfigure", Label: "configConfigure", Kind: "function",
			FileType: "code", Source: "a.go", Location: "L1-L5"},
		// Skew df: "config" common, "configure" rare. Whichever token the scorer
		// picks changes the score, so an undefined pick is an undefined ranking.
		{ID: "b.go::config", Label: "config", Kind: "function", FileType: "code", Source: "b.go"},
		{ID: "c.go::config", Label: "config", Kind: "function", FileType: "code", Source: "c.go"},
		{ID: "d.go::config", Label: "config", Kind: "function", FileType: "code", Source: "d.go"},
		{ID: "e.go::other", Label: "other", Kind: "function", FileType: "code", Source: "e.go"},
	}
	var first string
	for i := 0; i < 40; i++ {
		r := Run(nodes, nil, "conf", 2000)
		var sb strings.Builder
		for _, h := range r.Hits {
			fmt.Fprintf(&sb, "%s=%.4f;", h.Node.ID, h.Score)
		}
		if i == 0 {
			first = sb.String()
			continue
		}
		if sb.String() != first {
			t.Fatalf("run %d differs — map iteration order is leaking into scores\n first: %s\n now:   %s", i, first, sb.String())
		}
	}
}
