package analyze

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// ADR 2026-07-25-abstain-out-loud, consumer half. Emitting AMBIGUOUS edges is
// only honest because every traversal verb refuses them: an AMBIGUOUS `calls`
// edge is a SHORTLIST TO GREP, not a fact. Let one into a blast radius and the
// store gives a wrong answer to the question it exists to answer correctly.
//
// These are the tests that would catch someone adding a new traversal verb and
// forgetting the filter — the failure mode that turns a labelled maybe into a
// silent lie.
func ambiguousFixture() ([]schema.Node, []schema.Edge) {
	nodes := []schema.Node{
		{ID: "a.go::Target", Kind: "function", Label: "Target", Source: "a.go", Location: "L1-L3"},
		{ID: "b.go::RealCaller", Kind: "function", Label: "RealCaller", Source: "b.go", Location: "L1-L3"},
		{ID: "c.go::MaybeCaller", Kind: "function", Label: "MaybeCaller", Source: "c.go", Location: "L1-L3"},
		{ID: "d.go::Far", Kind: "function", Label: "Far", Source: "d.go", Location: "L1-L3"},
	}
	edges := []schema.Edge{
		{Source: "b.go::RealCaller", Target: "a.go::Target", Relation: "calls", Confidence: schema.Inferred, Weight: 1},
		{Source: "c.go::MaybeCaller", Target: "a.go::Target", Relation: "calls", Confidence: schema.Ambiguous, Weight: 1},
		{Source: "d.go::Far", Target: "c.go::MaybeCaller", Relation: "calls", Confidence: schema.Inferred, Weight: 1},
	}
	return nodes, edges
}

func TestWithoutAmbiguousDropsOnlyMaybes(t *testing.T) {
	_, edges := ambiguousFixture()
	got := WithoutAmbiguous(edges)
	if len(got) != 2 {
		t.Fatalf("want 2 edges kept, got %d", len(got))
	}
	for _, e := range got {
		if e.Confidence == schema.Ambiguous {
			t.Errorf("AMBIGUOUS edge survived: %+v", e)
		}
	}
}

// A blast radius must contain no maybes. Without the filter, MaybeCaller enters
// at depth 1 and drags Far in at depth 2 — two fabricated impacts.
func TestAffectedExcludesAmbiguous(t *testing.T) {
	nodes, edges := ambiguousFixture()
	_, impacts, err := Affected(nodes, edges, "Target", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, im := range impacts {
		if im.Node.ID == "c.go::MaybeCaller" {
			t.Error("ambiguous caller entered the blast radius")
		}
		if im.Node.ID == "d.go::Far" {
			t.Error("a node reachable ONLY through an ambiguous edge entered the blast radius")
		}
	}
	if len(impacts) == 0 {
		t.Error("the real caller was lost — the filter must not eat confident edges")
	}
}

// God-node ranking is what guessed edges corrupt: a name defined many times
// collects maybes and floats to the top of hubs.
func TestHubsIgnoreAmbiguous(t *testing.T) {
	nodes, edges := ambiguousFixture()
	withAll := Hubs(nodes, edges, 10)
	// Recompute against an explicitly pre-filtered set: identical, proving Hubs
	// filters internally rather than relying on its caller.
	withFiltered := Hubs(nodes, WithoutAmbiguous(edges), 10)
	if len(withAll) != len(withFiltered) {
		t.Fatalf("hub count differs: %d vs %d — Hubs is not filtering internally", len(withAll), len(withFiltered))
	}
	for i := range withAll {
		if withAll[i].Node.ID != withFiltered[i].Node.ID || withAll[i].In != withFiltered[i].In {
			t.Errorf("hub %d differs: %+v vs %+v", i, withAll[i], withFiltered[i])
		}
	}
}

// A path through a maybe is not a path.
func TestShortestPathRefusesAmbiguousHops(t *testing.T) {
	nodes, edges := ambiguousFixture()
	// Far → MaybeCaller → Target exists ONLY if the ambiguous hop is traversed.
	if steps, err := ShortestPath(nodes, edges, "Far", "Target"); err == nil && len(steps) > 0 {
		for _, s := range steps {
			if strings.Contains(s.To, "Target") {
				t.Errorf("path traversed an ambiguous hop: %+v", steps)
			}
		}
	}
}

// The card cites callers directly, so maybes must not be listed — but the COUNT
// must be reported, because a silently-short `called by` is the original defect.
func TestCardReportsButDoesNotListAmbiguous(t *testing.T) {
	nodes, edges := ambiguousFixture()
	c, err := Card(nodes, edges, "Target")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range c.CalledBy {
		if id == "c.go::MaybeCaller" {
			t.Error("ambiguous caller was listed in called_by as if it were a fact")
		}
	}
	if c.AmbiguousCallers != 1 {
		t.Errorf("AmbiguousCallers = %d, want 1 — the abstention must be reported, not hidden", c.AmbiguousCallers)
	}
	out := RenderCard(c)
	if !strings.Contains(out, "unattributed callers: 1") {
		t.Errorf("card did not say no out loud:\n%s", out)
	}
	if !strings.Contains(out, "grep -rn") {
		t.Errorf("card reported an abstention without the grep that settles it:\n%s", out)
	}
}

// A symbol with no ambiguity must produce no noise — the line only appears when
// there is something to admit.
func TestCardSilentWhenNothingAmbiguous(t *testing.T) {
	nodes, edges := ambiguousFixture()
	c, err := Card(nodes, WithoutAmbiguous(edges), "Target")
	if err != nil {
		t.Fatal(err)
	}
	if c.AmbiguousCallers != 0 {
		t.Errorf("AmbiguousCallers = %d with no ambiguous edges", c.AmbiguousCallers)
	}
	if strings.Contains(RenderCard(c), "unattributed") {
		t.Error("card mentioned unattributed callers when there were none")
	}
}
