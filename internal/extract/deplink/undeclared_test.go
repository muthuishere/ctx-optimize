package deplink

import (
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// A scoped import with no matching dep is flagged undeclared, with a file
// edge; a scoped import that DOES resolve is not; an unscoped bare unresolved
// import is not flagged (scoped-only keeps false positives near zero).
func TestUndeclaredScopedDrift(t *testing.T) {
	code := &schema.Batch{Producer: "code",
		Nodes: []schema.Node{
			modNode("@mastra/core"),    // scoped, undeclared → flagged
			modNode("@scope/declared"), // scoped, declared → not flagged
			modNode("lodash"),          // bare, undeclared → NOT flagged (scoped-only)
		},
		Edges: []schema.Edge{
			{Source: "src/a.ts", Target: modulePrefix + "@mastra/core", Relation: "imports", Confidence: "EXTRACTED"},
			{Source: "src/b.ts", Target: modulePrefix + "@mastra/core", Relation: "imports", Confidence: "EXTRACTED"},
			{Source: "src/a.ts", Target: modulePrefix + "@scope/declared", Relation: "imports", Confidence: "EXTRACTED"},
			{Source: "src/a.ts", Target: modulePrefix + "lodash", Relation: "imports", Confidence: "EXTRACTED"},
		},
	}
	man := &schema.Batch{Producer: "manifests", Nodes: []schema.Node{
		depNode("dep:npm/@scope/declared"),
	}}
	b := Link(code, man, nil)

	var undeclaredNode *schema.Node
	for i := range b.Nodes {
		if b.Nodes[i].Kind == "undeclared_dependency" {
			undeclaredNode = &b.Nodes[i]
		}
	}
	if undeclaredNode == nil || undeclaredNode.ID != "undeclared:npm/@mastra/core" {
		t.Fatalf("want one undeclared node for @mastra/core, got %+v", b.Nodes)
	}
	// exactly the two importing files, no declared/bare leakage
	files := map[string]bool{}
	for _, e := range b.Edges {
		if e.Relation == "undeclared_dependency" {
			if e.Target != "undeclared:npm/@mastra/core" {
				t.Fatalf("undeclared edge to wrong target: %s", e.Target)
			}
			files[e.Source] = true
		}
	}
	if len(files) != 2 || !files["src/a.ts"] || !files["src/b.ts"] {
		t.Fatalf("undeclared edges = %v (want a.ts+b.ts)", files)
	}
	// @scope/declared resolved → a resolves_to edge, never undeclared
	for _, n := range b.Nodes {
		if n.ID == "undeclared:npm/@scope/declared" {
			t.Fatal("declared scoped import must not be flagged")
		}
	}
}

// No npm deps at all → npm context absent → no undeclared flagging (avoids
// firing on pure-go repos).
func TestUndeclaredNeedsNpmContext(t *testing.T) {
	code := &schema.Batch{Producer: "code", Nodes: []schema.Node{modNode("@x/y")},
		Edges: []schema.Edge{{Source: "a.ts", Target: modulePrefix + "@x/y", Relation: "imports", Confidence: "EXTRACTED"}}}
	man := &schema.Batch{Producer: "manifests"} // no npm deps
	b := Link(code, man, nil)
	for _, n := range b.Nodes {
		if n.Kind == "undeclared_dependency" {
			t.Fatal("no npm context → no undeclared flagging")
		}
	}
}
