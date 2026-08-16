package deplink

import (
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// A module importing its OWN npm package name must not resolve to a dependency
// on itself. goSelf covers this for Go from a caller-supplied list; npm had no
// equivalent, and ADR 22 D0 makes the module's own name a dep node — the same
// global id a consumer declares, which is the point — so without an exclusion
// the self-import finds it.
func TestSelfNpmImportIsNotADependency(t *testing.T) {
	manifest := &schema.Batch{
		Producer: "manifests",
		Nodes: []schema.Node{
			{ID: "dep:npm/@acme/ui", Label: "@acme/ui", Kind: "dependency", Source: "dep://npm/@acme/ui"},
			{ID: "dep:npm/react", Label: "react", Kind: "dependency", Source: "dep://npm/react"},
		},
		Edges: []schema.Edge{
			{Source: "package.json", Target: "dep:npm/@acme/ui", Relation: "publishes", Confidence: schema.Extracted},
			{Source: "package.json", Target: "dep:npm/react", Relation: "declares", Confidence: schema.Extracted},
		},
	}
	code := &schema.Batch{
		Producer: "code",
		Nodes: []schema.Node{
			{ID: "module://@acme/ui/button", Label: "@acme/ui/button", Kind: "module", Source: "module://@acme/ui/button"},
			{ID: "module://react", Label: "react", Kind: "module", Source: "module://react"},
		},
	}
	b := Link(code, manifest, nil)
	for _, e := range b.Edges {
		if e.Target == "dep:npm/@acme/ui" {
			t.Errorf("self npm import resolved to a dependency on itself: %+v", e)
		}
	}
	// a real dependency still resolves
	var react bool
	for _, e := range b.Edges {
		if e.Target == "dep:npm/react" {
			react = true
		}
	}
	if !react {
		t.Error("react no longer resolves — the exclusion is too wide")
	}
}
