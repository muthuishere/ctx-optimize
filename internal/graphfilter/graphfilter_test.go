package graphfilter

import (
	"reflect"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func nodes() []schema.Node {
	return []schema.Node{
		{ID: "dep:npm/react", Label: "react", Kind: "dependency", FileType: "manifest", Metadata: map[string]string{"producer": "manifests", "scopes": "runtime"}},
		{ID: "dep:npm/vitest", Label: "vitest", Kind: "dependency", FileType: "manifest", Metadata: map[string]string{"producer": "manifests", "scopes": "dev"}},
		{ID: "k8s://prod/service/api", Label: "api", Kind: "service", FileType: "infra", Metadata: map[string]string{"namespace": "prod"}},
		{ID: "src/app.tsx", Label: "app.tsx", Kind: "file", FileType: "code"},
		{ID: "src/app.tsx::App", Label: "App", Kind: "function", FileType: "code", Metadata: map[string]string{"lang": "typescript"}},
	}
}

func edges() []schema.Edge {
	return []schema.Edge{
		{Source: "module://react", Target: "dep:npm/react", Relation: "resolves_to", Confidence: "INFERRED", Metadata: map[string]string{"producer": "deplink"}},
		{Source: "src/app.tsx", Target: "module://react", Relation: "imports", Confidence: "EXTRACTED"},
		{Source: "ingress", Target: "k8s://prod/service/api", Relation: "routes_to", Confidence: "EXTRACTED"},
	}
}

func ids(ns []schema.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.ID
	}
	return out
}

func TestNodeKindAndScope(t *testing.T) {
	p, _ := ParsePred(map[string]string{"kind": "dependency", "scope": "dev"})
	got, edgesOut := Apply(nodes(), edges(), p)
	if !reflect.DeepEqual(ids(got), []string{"dep:npm/vitest"}) {
		t.Fatalf("kind+scope = %v", ids(got))
	}
	// node-only predicate leaves edges untouched
	if len(edgesOut) != len(edges()) {
		t.Fatalf("node-only pred must not touch edges: %d", len(edgesOut))
	}
}

func TestKindOrSet(t *testing.T) {
	p, _ := ParsePred(map[string]string{"kind": "service,file"})
	got, _ := Apply(nodes(), edges(), p)
	if !reflect.DeepEqual(ids(got), []string{"k8s://prod/service/api", "src/app.tsx"}) {
		t.Fatalf("OR-set = %v", ids(got))
	}
}

func TestEdgeRelationOnly(t *testing.T) {
	p, _ := ParsePred(map[string]string{"relation": "resolves_to"})
	nodesOut, got := Apply(nodes(), edges(), p)
	if len(got) != 1 || got[0].Relation != "resolves_to" {
		t.Fatalf("edge filter = %v", got)
	}
	if len(nodesOut) != len(nodes()) {
		t.Fatalf("edge-only pred must not touch nodes: %d", len(nodesOut))
	}
}

func TestWhereExactAndContainsAndMissing(t *testing.T) {
	// exact metadata match
	p, _ := ParsePred(map[string]string{"where": "namespace=prod"})
	got, _ := Apply(nodes(), edges(), p)
	if !reflect.DeepEqual(ids(got), []string{"k8s://prod/service/api"}) {
		t.Fatalf("where exact = %v", ids(got))
	}
	// contains on top-level field
	p, _ = ParsePred(map[string]string{"where": "id~app.tsx"})
	got, _ = Apply(nodes(), edges(), p)
	if !reflect.DeepEqual(ids(got), []string{"src/app.tsx", "src/app.tsx::App"}) {
		t.Fatalf("where contains = %v", ids(got))
	}
	// missing key = no-match, never crash
	p, _ = ParsePred(map[string]string{"where": "nope=1"})
	got, _ = Apply(nodes(), edges(), p)
	if len(got) != 0 {
		t.Fatalf("missing key must match nothing: %v", ids(got))
	}
}

func TestIDPrefixSharedDim(t *testing.T) {
	p, _ := ParsePred(map[string]string{"id-prefix": "dep:"})
	gotN, gotE := Apply(nodes(), edges(), p)
	if !reflect.DeepEqual(ids(gotN), []string{"dep:npm/react", "dep:npm/vitest"}) {
		t.Fatalf("id-prefix nodes = %v", ids(gotN))
	}
	// shared dim also filters edges (endpoint prefix)
	if len(gotE) != 1 || gotE[0].Target != "dep:npm/react" {
		t.Fatalf("id-prefix edges = %v", gotE)
	}
}

func TestProducerAndConfidence(t *testing.T) {
	p, _ := ParsePred(map[string]string{"producer": "deplink", "confidence": "INFERRED"})
	_, gotE := Apply(nodes(), edges(), p)
	if len(gotE) != 1 || gotE[0].Relation != "resolves_to" {
		t.Fatalf("producer+confidence = %v", gotE)
	}
}

func TestEmptyPredPassthrough(t *testing.T) {
	p, _ := ParsePred(map[string]string{})
	if !p.Empty() {
		t.Fatal("no flags = empty pred")
	}
	gotN, gotE := Apply(nodes(), edges(), p)
	if len(gotN) != len(nodes()) || len(gotE) != len(edges()) {
		t.Fatal("empty pred must pass everything through")
	}
}

func TestProjection(t *testing.T) {
	n := nodes()[0]
	n.Scope = "runtime" // F1: top-level scope now populated on dep nodes
	m := ProjectNode(n, Fields("id,scope,metadata.scopes"))
	if m["id"] != "dep:npm/react" || m["metadata.scopes"] != "runtime" {
		t.Fatalf("projection = %v", m)
	}
	// 'scope' is now a first-class field (F1) — projects the top-level value.
	if m["scope"] != "runtime" {
		t.Fatalf("scope should project the top-level field: %v", m["scope"])
	}
}

func TestBadWhere(t *testing.T) {
	if _, err := ParsePred(map[string]string{"where": "noseparator"}); err == nil {
		t.Fatal("bad where must error")
	}
}
