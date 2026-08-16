package analyze

import (
	"sort"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// bport builds a port node the way the boundaries producer emits one.
func bport(id, dir, transport, ident string, meta map[string]string) schema.Node {
	m := map[string]string{"direction": dir, "transport": transport, "identifier": ident}
	for k, v := range meta {
		m[k] = v
	}
	return schema.Node{ID: id, Label: ident, Kind: "port", FileType: "boundary",
		Source: "port://" + transport + "/" + ident, Metadata: m}
}

func bnedge(src, dst, rel, conf, site string) schema.Edge {
	return schema.Edge{Source: src, Target: dst, Relation: rel, Confidence: conf,
		Metadata: map[string]string{"site": site, "rule": "r"}}
}

// The same logical boundary appears once PER MODULE in a federated store, with
// module-prefixed ids. It must fold to ONE entry that names both modules —
// otherwise a monorepo reports its egress N times and the count lies.
func TestFederatedPortsFoldByIdentifierNotID(t *testing.T) {
	nodes := []schema.Node{
		bport("apps/api/port:network.http:>api.openai.com", "consumes", "network.http", "api.openai.com", nil),
		bport("apps/ui/port:network.http:>api.openai.com", "consumes", "network.http", "api.openai.com", nil),
	}
	edges := []schema.Edge{
		bnedge("apps/api/openai.go", "apps/api/port:network.http:>api.openai.com", "consumes", schema.Extracted, "openai.go:L42"),
		bnedge("apps/ui/http.ts", "apps/ui/port:network.http:>api.openai.com", "consumes", schema.Inferred, "http.ts:L9"),
	}
	r := Boundaries(nodes, edges, BoundaryOptions{})
	if r.Ports != 2 {
		t.Fatalf("raw port count = %d, want 2 (the report counts NODES)", r.Ports)
	}
	if len(r.Consumes) != 1 || len(r.Consumes[0].Entries) != 1 {
		t.Fatalf("want one folded entry, got %+v", r.Consumes)
	}
	e := r.Consumes[0].Entries[0]
	if e.Sites != 2 {
		t.Errorf("sites = %d, want 2 (both modules bind it)", e.Sites)
	}
	if got := strings.Join(e.Modules, ","); got != "apps/api,apps/ui" {
		t.Errorf("modules = %q, want both attributed", got)
	}
	// Strongest evidence wins, but the disagreement stays visible: an
	// EXTRACTED call site and an INFERRED one are different claims.
	if e.Tier != schema.Extracted || !e.MixedTiers {
		t.Errorf("tier = %q mixed = %v, want EXTRACTED + mixed", e.Tier, e.MixedTiers)
	}
}

// Dynamic identifiers are ~30% of a real repo. They must be COUNTED (never
// hidden — that would be the lie ADR 7 exists to prevent) but not enumerated
// inline, because alphabetically they crowd out every real answer.
func TestDynamicPortsSummarisedNotEnumerated(t *testing.T) {
	nodes := []schema.Node{
		bport("p1", "consumes", "config.env", "REAL_KEY", nil),
		bport("p2", "consumes", "config.env", "${varName}", map[string]string{"resolved": "dynamic"}),
	}
	edges := []schema.Edge{
		bnedge("a.go", "p1", "consumes", schema.Inferred, "a.go:L1"),
		bnedge("b.go", "p2", "consumes", schema.Ambiguous, "b.go:L2"),
	}
	r := Boundaries(nodes, edges, BoundaryOptions{})
	g := r.Consumes[0]
	if g.Total != 2 || g.Dynamic != 1 {
		t.Fatalf("total=%d dynamic=%d, want 2/1 — dynamic must be COUNTED", g.Total, g.Dynamic)
	}
	if len(g.Entries) != 1 || g.Entries[0].Identifier != "REAL_KEY" {
		t.Errorf("dynamic leaked into entries: %+v", g.Entries)
	}
	if r.DynamicTotal != 1 {
		t.Errorf("DynamicTotal = %d, want 1", r.DynamicTotal)
	}
	// --all makes the sites reachable, which is the other half of "never hidden".
	all := Boundaries(nodes, edges, BoundaryOptions{All: true})
	if len(all.Consumes[0].Entries) != 2 {
		t.Errorf("--all must list dynamic entries, got %d", len(all.Consumes[0].Entries))
	}
}

// Silent truncation reads as "that is everything". Whatever is withheld must
// be counted in the report itself, not left to the renderer.
func TestBudgetAlwaysReportsWhatItWithheld(t *testing.T) {
	var nodes []schema.Node
	var edges []schema.Edge
	for _, id := range []string{"A", "B", "C", "D", "E"} {
		nodes = append(nodes, bport("p"+id, "consumes", "config.env", id, nil))
		edges = append(edges, bnedge("f.go", "p"+id, "consumes", schema.Inferred, "f.go:L1"))
	}
	r := Boundaries(nodes, edges, BoundaryOptions{PerGroup: 2})
	g := r.Consumes[0]
	if len(g.Entries) != 2 || g.Withheld != 3 || g.Total != 5 {
		t.Fatalf("entries=%d withheld=%d total=%d, want 2/3/5", len(g.Entries), g.Withheld, g.Total)
	}
	if !r.Truncated {
		t.Error("report must flag that something was withheld")
	}
	if all := Boundaries(nodes, edges, BoundaryOptions{All: true}); all.Consumes[0].Withheld != 0 {
		t.Error("--all must withhold nothing")
	}
}

// Secrets lead, then egress, then alphabetical — a reader scanning for "what
// leaks" should not have to page. Also pins determinism.
//
// "Egress first" is now expressed as "NOT proven internal first" (ADR
// 2026-08-15-scope-join-broken): scope carries only `internal`, so MMM_INT
// sorts BEHIND the alphabetically-later ZZZ_PLAIN precisely because it is the
// one port the join proved stays inside.
func TestEntryOrderSecretsThenExternalThenName(t *testing.T) {
	nodes := []schema.Node{
		bport("p1", "consumes", "config.env", "ZZZ_PLAIN", nil),
		bport("p2", "consumes", "config.env", "AAA_SECRET", map[string]string{"sensitive": "true"}),
		bport("p3", "consumes", "config.env", "MMM_INT", map[string]string{"scope": "internal"}),
	}
	var edges []schema.Edge
	for _, id := range []string{"p1", "p2", "p3"} {
		edges = append(edges, bnedge("f.go", id, "consumes", schema.Inferred, "f.go:L1"))
	}
	want := []string{"AAA_SECRET", "ZZZ_PLAIN", "MMM_INT"}
	for i := 0; i < 3; i++ { // repeat: map iteration must not leak into order
		r := Boundaries(nodes, edges, BoundaryOptions{})
		for j, e := range r.Consumes[0].Entries {
			if e.Identifier != want[j] {
				t.Fatalf("run %d position %d = %q, want %q", i, j, e.Identifier, want[j])
			}
		}
	}
}

// Direction is the whole point of the split: "what we call" and "what we
// expose" are different questions.
func TestDirectionSplitAndFilters(t *testing.T) {
	nodes := []schema.Node{
		bport("p1", "consumes", "network.http", "api.example.com", nil),
		bport("p2", "provides", "network.http", "/health", nil),
		bport("p3", "consumes", "process.exec", "git", nil),
		// The only port PROVEN internal: the join found /health provided here.
		// Nothing else carries a scope, because a miss is undecidable rather
		// than external (ADR 2026-08-15-scope-join-broken).
		bport("p4", "consumes", "network.http", "/health", map[string]string{"scope": "internal"}),
	}
	var edges []schema.Edge
	for _, id := range []string{"p1", "p2", "p3", "p4"} {
		edges = append(edges, bnedge("f.go", id, "consumes", schema.Extracted, "f.go:L1"))
	}
	edges[1].Relation = "provides"

	r := Boundaries(nodes, edges, BoundaryOptions{})
	if len(r.Consumes) != 2 || len(r.Provides) != 1 {
		t.Fatalf("consumes groups=%d provides=%d, want 2/1", len(r.Consumes), len(r.Provides))
	}
	only := Boundaries(nodes, edges, BoundaryOptions{Direction: "provides"})
	if len(only.Consumes) != 0 || len(only.Provides) != 1 {
		t.Errorf("--direction provides leaked consumes: %+v", only.Consumes)
	}
	// A dotted prefix selects a family: network matches network.http.
	fam := Boundaries(nodes, edges, BoundaryOptions{Transport: "network"})
	if len(fam.Consumes) != 1 || fam.Consumes[0].Transport != "network.http" {
		t.Errorf("--transport network should match network.http, got %+v", fam.Consumes)
	}
	// `internal` is the only value the join can prove, so the group's Internal
	// count is 1 of 2 and there is no External counter to disagree with it.
	if g := fam.Consumes[0]; g.Total != 2 || g.Internal != 1 {
		t.Errorf("network.http group should be 2 total / 1 internal, got %+v", g)
	}
	// --external is "everything not PROVEN internal": it drops /health and
	// keeps both the unjoined host and the unjoinable process port. Asserting
	// scope=="external" would be asserting a value nothing emits.
	ext := Boundaries(nodes, edges, BoundaryOptions{OnlyExt: true})
	var got []string
	for _, g := range ext.Consumes {
		for _, e := range g.Entries {
			got = append(got, e.Identifier)
		}
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "api.example.com" || got[1] != "git" {
		t.Errorf("--external should keep everything not proven internal, got %v", got)
	}
}

// A port with no edges still exists as a fact — it must be counted, not
// dropped, or the summary silently under-reports the surface.
func TestPortWithoutEdgesStillCounted(t *testing.T) {
	nodes := []schema.Node{bport("p1", "consumes", "config.env", "ORPHAN", nil)}
	r := Boundaries(nodes, nil, BoundaryOptions{})
	if len(r.Consumes) != 1 || r.Consumes[0].Total != 1 {
		t.Fatalf("orphan port dropped: %+v", r.Consumes)
	}
	if e := r.Consumes[0].Entries[0]; e.Sites != 0 || e.Cite != "" {
		t.Errorf("orphan should report zero sites, got %+v", e)
	}
}
