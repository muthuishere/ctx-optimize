package analyze

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// ADR 2026-07-26-include-ambiguous. Abstaining (2026-07-25-abstain-out-loud,
// 2026-07-25-method-call-resolution) made the traversal verbs answer with facts
// only — which means a method's blast radius is a FLOOR, and until now there was
// no way to ask the CLI for the rest. IncludeAmbiguous is that door.
//
// The door is only safe while two things hold, and both are pinned here:
//  1. it is OFF unless asked, so nothing widens by accident;
//  2. when ON, every widened result is MARKED, so a maybe can never be read
//     (or copied out of the terminal) as a fact.
func ambFixture() ([]schema.Node, []schema.Edge) {
	nodes := []schema.Node{
		{ID: "t", Label: "Target", Kind: "method"},
		{ID: "sure", Label: "Sure", Kind: "function"},
		{ID: "maybe", Label: "Maybe", Kind: "function"},
	}
	edges := []schema.Edge{
		{Source: "sure", Target: "t", Relation: "calls", Confidence: schema.Inferred, Weight: 1},
		{Source: "maybe", Target: "t", Relation: "calls", Confidence: schema.Ambiguous, Weight: 1,
			Metadata: map[string]string{"ambiguous_reason": schema.AmbiguousUnresolvedReceiver}},
	}
	return nodes, edges
}

func TestAffectedExcludesMaybesUnlessAsked(t *testing.T) {
	nodes, edges := ambFixture()

	_, impacts, err := Affected(nodes, edges, "Target", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(impacts) != 1 || impacts[0].Node.ID != "sure" {
		t.Fatalf("default blast radius must be facts only, got %+v", impacts)
	}

	_, wide, err := Affected(nodes, edges, "Target", 2, nil, IncludeAmbiguous())
	if err != nil {
		t.Fatal(err)
	}
	if len(wide) != 2 {
		t.Fatalf("IncludeAmbiguous must reach the shortlist too, got %+v", wide)
	}
	for _, im := range wide {
		if im.Node.ID == "maybe" && im.Confidence != schema.Ambiguous {
			t.Error("a widened row lost its confidence — the renderer has nothing left to mark it with, and an unmarked maybe is a wrong answer")
		}
		if im.Node.ID == "sure" && im.Confidence == schema.Ambiguous {
			t.Error("a fact was labelled AMBIGUOUS")
		}
	}
}

// CalledBy/Calls must stay facts-only under EVERY option: code that reads those
// fields cannot be widened by a flag it never looked at.
func TestCardKeepsFactListsPureAndNamesTheMaybesSeparately(t *testing.T) {
	nodes, edges := ambFixture()

	c, err := Card(nodes, edges, "Target", IncludeAmbiguous())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.CalledBy) != 1 || c.CalledBy[0] != "sure" {
		t.Fatalf("CalledBy must stay facts-only even under IncludeAmbiguous, got %v", c.CalledBy)
	}
	if len(c.AmbiguousCalledBy) != 1 || c.AmbiguousCalledBy[0] != "maybe" {
		t.Fatalf("the shortlist must be named in its own field, got %v", c.AmbiguousCalledBy)
	}
	if c.AmbiguousCallers != 1 {
		t.Errorf("AmbiguousCallers = %d, want 1", c.AmbiguousCallers)
	}

	plain, err := Card(nodes, edges, "Target")
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.AmbiguousCalledBy) != 0 {
		t.Error("the shortlist must not be named unless asked for")
	}
}

// The rendered card is what a human reads and an agent quotes. A maybe in it
// must be visibly a maybe.
func TestRenderedCardMarksTheMaybes(t *testing.T) {
	nodes, edges := ambFixture()
	c, err := Card(nodes, edges, "Target", IncludeAmbiguous())
	if err != nil {
		t.Fatal(err)
	}
	out := RenderCard(c)
	if !strings.Contains(out, "MAYBE called by") || !strings.Contains(out, "AMBIGUOUS") {
		t.Errorf("rendered card does not separate the shortlist:\n%s", out)
	}
	factIdx := strings.Index(out, "called by")
	maybeIdx := strings.Index(out, "MAYBE called by")
	if factIdx < 0 || maybeIdx < 0 || factIdx >= maybeIdx {
		t.Error("facts must be listed before, and separately from, the maybes")
	}
}

func TestExplainAndHubsHonorTheOption(t *testing.T) {
	nodes, edges := ambFixture()

	ex, err := Explain(nodes, edges, "Target")
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.IncomingAmbiguous) != 0 {
		t.Error("explain must not report maybes unless asked")
	}
	ex, err = Explain(nodes, edges, "Target", IncludeAmbiguous())
	if err != nil {
		t.Fatal(err)
	}
	if got := ex.IncomingAmbiguous["calls"]; len(got) != 1 || got[0] != "maybe" {
		t.Errorf("IncomingAmbiguous = %v, want [maybe]", got)
	}
	if got := ex.Incoming["calls"]; len(got) != 1 || got[0] != "sure" {
		t.Errorf("Incoming must stay facts-only, got %v", got)
	}

	degree := func(hs []Hub) int {
		for _, h := range hs {
			if h.Node.ID == "t" {
				return h.In + h.Out
			}
		}
		return -1
	}
	if d := degree(Hubs(nodes, edges, 10)); d != 1 {
		t.Errorf("default hub degree = %d, want 1 — guessed edges are exactly what corrupts god-node ranking", d)
	}
	if d := degree(Hubs(nodes, edges, 10, IncludeAmbiguous())); d != 2 {
		t.Errorf("widened hub degree = %d, want 2", d)
	}
}

// A path is only as good as its weakest hop.
func TestPathRefusesMaybesUnlessAskedAndLabelsTheHop(t *testing.T) {
	nodes, edges := ambFixture()

	if _, err := ShortestPath(nodes, edges, "Maybe", "Target"); err == nil {
		t.Error("a path through an AMBIGUOUS edge is not a path — it must not be found by default")
	}
	steps, err := ShortestPath(nodes, edges, "Maybe", "Target", IncludeAmbiguous())
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Confidence != schema.Ambiguous {
		t.Errorf("the widened hop must carry its confidence, got %+v", steps)
	}
}
