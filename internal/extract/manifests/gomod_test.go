package manifests

import (
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

const fixtureGoMod = `module github.com/example/svc

go 1.22

require github.com/single/line v0.3.0

require (
	github.com/gorilla/mux v1.8.1
	golang.org/x/sync v0.6.0 // indirect
)

replace github.com/old/thing => github.com/new/thing v1.0.0
`

func TestGoModRequires(t *testing.T) {
	b := extractFixture(t, map[string]string{"go.mod": fixtureGoMod})

	single := mustEdge(t, b, "go.mod", "dep:go/github.com/single/line", "declares", schema.Extracted)
	if single.Metadata["version_spec"] != "v0.3.0" || single.Metadata["scope"] != "require" {
		t.Fatalf("single-line require: %v", single.Metadata)
	}
	block := mustEdge(t, b, "go.mod", "dep:go/github.com/gorilla/mux", "declares", schema.Extracted)
	if block.Metadata["version_spec"] != "v1.8.1" {
		t.Fatalf("block require: %v", block.Metadata)
	}
	ind := mustEdge(t, b, "go.mod", "dep:go/golang.org/x/sync", "declares", schema.Extracted)
	if ind.Metadata["scope"] != "indirect" {
		t.Fatalf("indirect scope: %v", ind.Metadata)
	}
	// replace directives are not declarations.
	for _, e := range b.Edges {
		if e.Target == "dep:go/github.com/new/thing" || e.Target == "dep:go/github.com/old/thing" {
			t.Fatal("replace directive must not declare a dependency")
		}
	}
}
