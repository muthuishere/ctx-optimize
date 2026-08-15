package store

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// THE gate for ADR 2026-08-05-query-at-scale.
//
// The index makes lookups ~2,500x faster. This file exists to prove it changes
// no answer. It compares the index-backed resolution and edge fetch against the
// full-scan path that they replace, for EVERY node in the store — same node,
// same provenance string, same edge set.
//
// Not a smoke test. The spike's first run failed this comparison with 76
// mismatches in 20,031 labels (JSON escapes in keys), and the first
// implementation silently diverged on case (the index lowercased, the fallback
// did not). Neither was visible in any other test, because both paths were
// individually self-consistent. Only a differential check finds these.
//
// Against a real large store, run:
//
//	CTX_OPTIMIZE_TEST_BIGSTORE=<store> go test ./internal/store/ -run Equivalence -v

// The tie-break rules encoded in buildGroundTruth are copied from
// analyze.ResolveVia. store must not import analyze (analyze sits above it), so
// the rules live in both places — and this differential test is what keeps them
// honest: change ResolveVia's tie-break without changing ResolveExact and the
// corpus comparison below fails.

func sameEdgeSet(a, b []schema.Edge) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(e schema.Edge) string {
		return e.Source + "\x00" + e.Target + "\x00" + e.Relation + "\x00" + e.Confidence
	}
	count := map[string]int{}
	for _, e := range a {
		count[key(e)]++
	}
	for _, e := range b {
		count[key(e)]--
	}
	for _, v := range count {
		if v != 0 {
			return false
		}
	}
	return true
}

// groundTruth precomputes the full-scan answers ONCE.
//
// The first version of this test called a linear resolveViaScan per sample:
// 20,000 samples x 2.85M nodes is 57 billion comparisons and never finished.
// The comparison being O(n*m) does not make it more rigorous, just unrunnable —
// and a gate that cannot run is not a gate. Same answers, computed once.
type groundTruth struct {
	byID        map[string]*schema.Node
	byLowerLbl  map[string]*schema.Node // after ResolveVia's tie-break
	edgesByNode map[string][]schema.Edge
}

func buildGroundTruth(nodes []schema.Node, edges []schema.Edge) *groundTruth {
	g := &groundTruth{
		byID:        make(map[string]*schema.Node, len(nodes)),
		byLowerLbl:  make(map[string]*schema.Node, len(nodes)),
		edgesByNode: make(map[string][]schema.Edge, len(nodes)),
	}
	for i := range nodes {
		n := &nodes[i]
		if _, seen := g.byID[n.ID]; !seen {
			g.byID[n.ID] = n
		}
		if n.Label == "" {
			continue
		}
		k := strings.ToLower(n.Label)
		// The label tiebreak, written out INDEPENDENTLY of the implementation
		// — that independence is the whole value of this file. A definition
		// beats a mention; among equals, smallest ID. Mirrors
		// store.labelRankNode and analyze.labelRank, which is three copies of
		// one rule: store must not import analyze, and a ground truth that
		// called the code it audits would prove nothing.
		hit, seen := g.byLowerLbl[k]
		rank := func(x *schema.Node) int {
			switch {
			case isImportStubID(x.ID), x.Kind == "dependency", strings.HasPrefix(x.ID, "dep://"):
				return 2
			case x.Kind == "section" || x.Kind == "document":
				return 1
			default:
				return 0
			}
		}
		switch {
		case !seen:
			g.byLowerLbl[k] = n
		case rank(n) < rank(hit):
			g.byLowerLbl[k] = n
		case rank(n) == rank(hit) && n.ID < hit.ID:
			g.byLowerLbl[k] = n
		}
	}
	for _, e := range edges {
		g.edgesByNode[e.Source] = append(g.edgesByNode[e.Source], e)
		if e.Target != e.Source { // self-edge counted once, as EdgesTouching does
			g.edgesByNode[e.Target] = append(g.edgesByNode[e.Target], e)
		}
	}
	return g
}

func (g *groundTruth) resolve(name string) (*schema.Node, string, bool) {
	if n, ok := g.byID[name]; ok {
		return n, "exact-id", true
	}
	if n, ok := g.byLowerLbl[strings.ToLower(name)]; ok {
		return n, "exact-label", true
	}
	return nil, "", false
}

// runEquivalence: for every sampled node, resolve and fetch edges both ways and
// demand they agree exactly.
func runEquivalence(t *testing.T, s *Store, limit int) {
	t.Helper()
	nodes, err := s.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	edges, err := s.Edges()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Skip("empty store")
	}
	if !s.IndexCurrent() {
		if err := s.BuildIndex(); err != nil {
			t.Fatal(err)
		}
	}
	g := buildGroundTruth(nodes, edges)

	step := 1
	if limit > 0 && len(nodes) > limit {
		step = len(nodes) / limit
	}

	checkedResolve, checkedEdges := 0, 0
	badVia, badNode, badEdges := 0, 0, 0
	var firstBad string

	for i := 0; i < len(nodes); i += step {
		name := nodes[i].Label
		if name == "" {
			continue
		}
		wantN, wantVia, wantOK := g.resolve(name)
		gotN, gotVia, gotOK := s.ResolveExact(name)
		checkedResolve++

		if wantOK != gotOK {
			badNode++
			if firstBad == "" {
				firstBad = "resolve ok mismatch for " + name
			}
			continue
		}
		if !wantOK {
			continue
		}
		if wantVia != gotVia {
			badVia++
			if firstBad == "" {
				firstBad = "provenance for " + name + ": scan=" + wantVia + " index=" + gotVia
			}
		}
		if gotN == nil || wantN.ID != gotN.ID {
			badNode++
			if firstBad == "" {
				firstBad = "resolved to a DIFFERENT node for " + name
			}
			continue
		}
		gotE, err := s.EdgesTouching(gotN.ID)
		if err != nil {
			t.Fatal(err)
		}
		checkedEdges++
		if !sameEdgeSet(g.edgesByNode[wantN.ID], gotE) {
			badEdges++
			if firstBad == "" {
				firstBad = "edge set for " + gotN.ID
			}
		}
	}

	t.Logf("checked %d resolutions, %d edge sets", checkedResolve, checkedEdges)
	if badVia+badNode+badEdges > 0 {
		t.Fatalf("INDEX DIVERGES FROM FULL SCAN — %d wrong provenance, %d wrong node, %d wrong edge set. first: %s",
			badVia, badNode, badEdges, firstBad)
	}
}

// Hermetic tier: a small synthetic store exercising every trap found so far —
// duplicate labels, import stubs, case differences, JSON escapes, self-edges.
func TestEquivalenceHermetic(t *testing.T) {
	nodes := []schema.Node{
		{ID: "pkg/a.go::Alpha", Label: "Alpha", Kind: "function", FileType: "code", Source: "a.go", Location: "L1"},
		{ID: "module://Alpha", Label: "Alpha", Kind: "import", FileType: "code", Source: "a.go", Location: "L2"},
		{ID: "pkg/b.go::Alpha", Label: "Alpha", Kind: "function", FileType: "code", Source: "b.go", Location: "L3"},
		{ID: "pkg/c.go::beta", Label: "beta", Kind: "function", FileType: "code", Source: "c.go", Location: "L4"},
		{ID: "pkg/d.go::BETA", Label: "BETA", Kind: "function", FileType: "code", Source: "d.go", Location: "L5"},
		{ID: "cfg#tabbed", Label: "with\ttab", Kind: "config_key", FileType: "config", Source: "Makefile", Location: "L6"},
		{ID: "cfg#quoted", Label: `has"quote`, Kind: "config_key", FileType: "config", Source: "Makefile", Location: "L7"},
		{ID: "cfg#uni", Label: "uni<URL", Kind: "config_key", FileType: "config", Source: "Makefile", Location: "L8"},
		{ID: "pkg/e.go::Self", Label: "Self", Kind: "function", FileType: "code", Source: "e.go", Location: "L9"},
		// Prose vs declaration on one label — the case that slipped through.
		// The section's ID sorts BEFORE the class's, so a smallest-ID tiebreak
		// answers `card Gamma` with a README heading while the class sits in
		// the store. Both resolvers must prefer the declaration, and this pair
		// is what makes the equivalence test able to say so.
		{ID: "README.md::gamma", Label: "Gamma", Kind: "section", FileType: "document", Source: "README.md", Location: "L1-L9"},
		{ID: "pkg/f.go::Gamma", Label: "Gamma", Kind: "class", FileType: "code", Source: "f.go", Location: "L10-L40"},
	}
	edges := []schema.Edge{
		{Source: "pkg/a.go::Alpha", Target: "pkg/c.go::beta", Relation: "calls", Confidence: "EXTRACTED"},
		{Source: "pkg/b.go::Alpha", Target: "pkg/a.go::Alpha", Relation: "calls", Confidence: "EXTRACTED"},
		{Source: "pkg/c.go::beta", Target: "pkg/a.go::Alpha", Relation: "calls", Confidence: "AMBIGUOUS"},
		{Source: "pkg/e.go::Self", Target: "pkg/e.go::Self", Relation: "calls", Confidence: "EXTRACTED"}, // self-edge: must appear ONCE
		{Source: "a.go", Target: "pkg/a.go::Alpha", Relation: "contains", Confidence: "EXTRACTED"},
	}
	s := seed(t, nodes, edges)
	runEquivalence(t, s, 0)

	// The self-edge is the specific trap in EdgesTouching: it is returned by
	// both EdgesFrom and EdgesTo, so without dedup the card counts it twice.
	got, err := s.EdgesTouching("pkg/e.go::Self")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("self-edge returned %d times, want 1 — a card would double-count it", len(got))
	}

	// An import stub must never win a label tie against a real declaration.
	n, via, ok := s.ResolveExact("Alpha")
	if !ok || via != "exact-label" {
		t.Fatalf("ResolveExact(Alpha) = %v %q", ok, via)
	}
	if strings.HasPrefix(n.ID, "module://") {
		t.Errorf("import stub won a label tie: %s", n.ID)
	}
	if n.ID != "pkg/a.go::Alpha" {
		t.Errorf("tie-break picked %s, want the smallest non-stub id pkg/a.go::Alpha", n.ID)
	}
}

// Corpus tier: the same differential check against a real store, where the
// escape and case bugs actually lived. Env-gated runtime skip, per house rules.
func TestEquivalenceBigStore(t *testing.T) {
	dir := os.Getenv("CTX_OPTIMIZE_TEST_BIGSTORE")
	if dir == "" {
		t.Skip("set CTX_OPTIMIZE_TEST_BIGSTORE=<store dir> to run the differential check on a real store")
	}
	s := &Store{Dir: dir}
	if _, err := os.Stat(s.nodesPath()); err != nil {
		t.Skipf("no graph at %s", s.nodesPath())
	}
	// Sample count is tunable because the check is not cheap: each sample costs
	// four index lookups, and hub nodes drag in thousands of edges. 2,000
	// samples spread across the store run in minutes and would have caught both
	// bugs found so far (escapes hit 0.414% of labels, case affects all of
	// them). Raise it with CTX_OPTIMIZE_TEST_SAMPLES for a deeper sweep.
	samples := 2000
	if v := os.Getenv("CTX_OPTIMIZE_TEST_SAMPLES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			samples = n
		}
	}
	runEquivalence(t, s, samples)
}
