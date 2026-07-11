package query

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func nodes() []schema.Node {
	return []schema.Node{
		{ID: "blk-mq.c::BlkMqSubmitBio", Label: "BlkMqSubmitBio", Kind: "function", FileType: "code", Source: "blk-mq.c", Location: "L3093"},
		{ID: "elevator.c::ElvRegister", Label: "ElvRegister", Kind: "function", FileType: "code", Source: "elevator.c", Location: "L498"},
		{ID: "README.md", Label: "README.md", Kind: "document", FileType: "document", Source: "README.md", Location: "L1"},
	}
}

func TestRunFindsCamelCaseBySpacedQuery(t *testing.T) {
	r := Run(nodes(), nil, "where is submit bio handled", 2000)
	if len(r.Hits) == 0 || r.Hits[0].Node.ID != "blk-mq.c::BlkMqSubmitBio" {
		t.Fatalf("wanted BlkMqSubmitBio first, got %+v", r.Hits)
	}
}

func TestRunIncludesNeighbors(t *testing.T) {
	edges := []schema.Edge{{Source: "blk-mq.c::BlkMqSubmitBio", Target: "elevator.c::ElvRegister", Relation: "calls", Confidence: schema.Extracted}}
	r := Run(nodes(), edges, "submit bio", 2000)
	if len(r.Hits) == 0 || len(r.Hits[0].Neighbors) != 1 {
		t.Fatalf("neighborhood missing: %+v", r.Hits)
	}
	if r.Hits[0].Neighbors[0].Dir != "out" {
		t.Fatalf("direction wrong: %+v", r.Hits[0].Neighbors[0])
	}
}

func TestBudgetCapsOutput(t *testing.T) {
	var many []schema.Node
	for i := 0; i < 500; i++ {
		many = append(many, schema.Node{
			ID: strings.Repeat("x", 40) + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			Label: "submit handler variant", Kind: "function", FileType: "code", Source: "big.c",
		})
	}
	r := Run(many, nil, "submit handler", 200) // tiny budget
	if len(r.Hits) == 0 {
		t.Fatal("budget must still return at least one hit")
	}
	if len(r.Hits) > 10 {
		t.Fatalf("budget did not cap output: %d hits", len(r.Hits))
	}
}

func TestNoMatchesRendersHint(t *testing.T) {
	r := Run(nodes(), nil, "zzz qqq", 2000)
	if len(r.Hits) != 0 {
		t.Fatalf("unexpected hits: %+v", r.Hits)
	}
	if !strings.Contains(Render(r), "no matches") {
		t.Fatal("miss must be informative (S1e: cheap-but-informative misses)")
	}
}
