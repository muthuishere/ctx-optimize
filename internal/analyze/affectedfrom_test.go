package analyze

import (
	"errors"
	"reflect"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// affectedGraph is built to hit every place the indexed walk could diverge from
// the loaded one: a source reaching the same target through TWO relations (the
// unstable sort plus first-edge-wins decides which relation is reported), an
// AMBIGUOUS edge, a cycle, a self-edge, and an edge from a node that does not
// exist (the full path reports the zero Node for it).
func affectedGraph() ([]schema.Node, []schema.Edge) {
	nodes := []schema.Node{
		{ID: "t", Label: "Target", Kind: "function"},
		{ID: "a", Label: "A", Kind: "function"},
		{ID: "b", Label: "B", Kind: "function"},
		{ID: "c", Label: "C", Kind: "function"},
		{ID: "d", Label: "D", Kind: "function"},
		{ID: "e", Label: "E", Kind: "test"},
	}
	edges := []schema.Edge{
		{Source: "b", Target: "t", Relation: "references", Confidence: "EXTRACTED"},
		{Source: "a", Target: "t", Relation: "calls", Confidence: "INFERRED"},
		{Source: "b", Target: "t", Relation: "calls", Confidence: "INFERRED"},
		{Source: "c", Target: "t", Relation: "calls", Confidence: schema.Ambiguous},
		{Source: "d", Target: "a", Relation: "calls", Confidence: "EXTRACTED"},
		{Source: "t", Target: "d", Relation: "calls", Confidence: "EXTRACTED"}, // cycle
		{Source: "e", Target: "e", Relation: "calls", Confidence: "EXTRACTED"}, // self
		{Source: "e", Target: "d", Relation: "calls", Confidence: "EXTRACTED"},
		{Source: "ghost", Target: "b", Relation: "calls", Confidence: "EXTRACTED"},
		{Source: "a", Target: "b", Relation: "imports", Confidence: "EXTRACTED"},
	}
	return nodes, edges
}

// lookups returns accessors over the edges IN SLICE ORDER, standing in for the
// store index's file-order contract.
func lookups(nodes []schema.Node, edges []schema.Edge) (func(string) ([]schema.Edge, error), func(string) (schema.Node, error)) {
	in := func(id string) ([]schema.Edge, error) {
		var out []schema.Edge
		for _, e := range edges {
			if e.Target == id {
				out = append(out, e)
			}
		}
		return out, nil
	}
	nd := func(id string) (schema.Node, error) {
		for _, n := range nodes {
			if n.ID == id {
				return n, nil
			}
		}
		return schema.Node{}, nil
	}
	return in, nd
}

func TestAffectedFromMatchesAffected(t *testing.T) {
	nodes, edges := affectedGraph()
	in, nd := lookups(nodes, edges)
	target := &nodes[0]
	for _, depth := range []int{0, 1, 2, 3, 5} {
		for _, rel := range [][]string{nil, {"calls"}, {"references"}} {
			for _, opts := range [][]Option{nil, {IncludeAmbiguous()}} {
				_, want, err := Affected(nodes, edges, "t", depth, rel, opts...)
				if err != nil {
					t.Fatal(err)
				}
				got, err := AffectedFrom(target, depth, rel, in, nd, nil, opts...)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(want, got) {
					t.Errorf("depth=%d rel=%v amb=%v\n full : %+v\n index: %+v", depth, rel, len(opts) > 0, want, got)
				}
			}
		}
	}
}

// The fixture must actually exercise what it claims to, or the test above
// passes vacuously.
func TestAffectedGraphCoversTheTraps(t *testing.T) {
	nodes, edges := affectedGraph()
	_, got, err := Affected(nodes, edges, "t", 3, nil, IncludeAmbiguous())
	if err != nil {
		t.Fatal(err)
	}
	var sawGhost, sawAmb, sawB bool
	for _, im := range got {
		switch {
		case im.DependsOn == "b" && im.Node.ID == "":
			sawGhost = true
		case im.Confidence == schema.Ambiguous:
			sawAmb = true
		case im.Node.ID == "b":
			sawB = true
		}
	}
	if !sawGhost || !sawAmb || !sawB {
		t.Fatalf("fixture no longer hits its traps (ghost=%v amb=%v b=%v): %+v", sawGhost, sawAmb, sawB, got)
	}
}

// plan is consulted before a level's node lookups: an error there must abandon
// the walk without looking up a single node of that level.
func TestAffectedFromPlanRefusesBeforePaying(t *testing.T) {
	nodes, edges := affectedGraph()
	in, nd := lookups(nodes, edges)
	nodeCalls := 0
	counting := func(id string) (schema.Node, error) { nodeCalls++; return nd(id) }
	stop := errors.New("too big")
	var sawPending int
	var sawMore bool
	_, err := AffectedFrom(&nodes[0], 2, nil, in, counting, func(pending int, more bool) error {
		sawPending, sawMore = pending, more
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("err = %v, want the plan's error", err)
	}
	if nodeCalls != 0 {
		t.Errorf("looked up %d nodes after the plan refused", nodeCalls)
	}
	if sawPending != 2 || !sawMore {
		t.Errorf("plan saw pending=%d more=%v, want 2 (a, b) and more=true", sawPending, sawMore)
	}
}
