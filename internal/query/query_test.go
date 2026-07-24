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
			ID:    strings.Repeat("x", 40) + string(rune('a'+i%26)) + string(rune('a'+i/26)),
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

// Trigram tier: a typo'd identifier still finds its node.
func TestTrigramCatchesTypo(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a", Label: "RefundSerializer", Kind: "class", FileType: "code", Source: "a.py"},
		{ID: "b", Label: "PaymentGateway", Kind: "class", FileType: "code", Source: "b.py"},
	}
	res := Run(nodes, nil, "refund serialzer", 2000) // missing 'i'
	if len(res.Hits) == 0 || res.Hits[0].Node.ID != "a" {
		t.Fatalf("typo not caught: %+v", res.Hits)
	}
}

// Acronym runs survive camelCase splitting.
func TestTokenizeAcronyms(t *testing.T) {
	got := Tokenize("HTTPServer blkMqSubmitBio parseURL")
	want := map[string]bool{"http": true, "server": true, "blk": true, "mq": true,
		"submit": true, "bio": true, "parse": true, "url": true}
	if len(got) != len(want) {
		t.Fatalf("tokens: %v", got)
	}
	for _, tk := range got {
		if !want[tk] {
			t.Fatalf("unexpected token %q in %v", tk, got)
		}
	}
}

// ADR 2026-07-24-answer-quality F1/F2: for a plain symbol question the
// DEFINITION outranks import stubs and test functions (measured flask failure:
// tests + module://url_for sat above app.py:1102). Intent flips the guards off.
func TestDefinitionOutranksStubsAndTests(t *testing.T) {
	nodes := []schema.Node{
		{ID: "module://url_for", Label: "url_for", Kind: "module"},
		{ID: "src/flask/helpers.py::url_for", Label: "url_for", Kind: "function", Source: "src/flask/helpers.py"},
		{ID: "tests/test_basic.py::test_url_for_defaults", Label: "test_url_for_defaults", Kind: "function", Source: "tests/test_basic.py"},
	}
	r := Run(nodes, nil, "where is url_for defined", 2000)
	if len(r.Hits) == 0 || r.Hits[0].Node.ID != "src/flask/helpers.py::url_for" {
		t.Fatalf("definition must be the top hit, got %+v", r.Hits)
	}
	// Import intent: the stub is a legitimate answer again.
	r = Run(nodes, nil, "which files import url_for", 2000)
	found := false
	for _, h := range r.Hits[:min(2, len(r.Hits))] {
		if h.Node.ID == "module://url_for" {
			found = true
		}
	}
	if !found {
		t.Fatalf("import-intent query must keep the stub near the top: %+v", r.Hits)
	}
	// Test intent: test nodes are not demoted.
	r = Run(nodes, nil, "url_for tests", 2000)
	if len(r.Hits) == 0 || !strings.Contains(r.Hits[0].Node.Source, "tests/") {
		t.Fatalf("test-intent query must keep tests on top: %+v", r.Hits)
	}
}

func TestIsTestSource(t *testing.T) {
	yes := []string{"tests/test_basic.py", "pkg/store/store_test.go", "src/a.spec.ts",
		"src/a.test.tsx", "src/test/java/FooTest.java", "test/EFCoreTests.cs", "a/tests/b.c"}
	no := []string{"src/flask/helpers.py", "internal/store/store.go", "docs/testing.md",
		"src/contest.go", "attest.cs"}
	for _, s := range yes {
		if !isTestSource(s) {
			t.Errorf("isTestSource(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isTestSource(s) {
			t.Errorf("isTestSource(%q) = true, want false", s)
		}
	}
}
