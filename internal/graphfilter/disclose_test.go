package graphfilter

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func discFixture() ([]schema.Node, []schema.Edge) {
	nodes := []schema.Node{
		{ID: "f:a.go", Label: "a.go", Kind: "file", FileType: "code", Source: "a.go"},
		{ID: "fn:Alpha", Label: "Alpha", Kind: "function", FileType: "code", Source: "a.go"},
		{ID: "port:http", Label: "api.example.com", Kind: "port", FileType: "boundary",
			Metadata: map[string]string{"direction": "provides", "transport": "network.http"}},
		{ID: "port:env", Label: "TOKEN", Kind: "port", FileType: "boundary",
			Metadata: map[string]string{"direction": "consumes", "transport": "config.env"}},
		{ID: "dep:left-pad", Label: "left-pad", Kind: "dependency", Scope: "runtime"},
	}
	edges := []schema.Edge{
		{Source: "f:a.go", Target: "fn:Alpha", Relation: "contains", Confidence: schema.Extracted},
		{Source: "fn:Alpha", Target: "fn:Alpha", Relation: "calls", Confidence: schema.Inferred},
	}
	return nodes, edges
}

func pred(t *testing.T, kv map[string]string) Pred {
	t.Helper()
	p, err := ParsePred(kv)
	if err != nil {
		t.Fatalf("ParsePred(%v): %v", kv, err)
	}
	return p
}

// THE defect: `nodes --kind route` had no way to say "there is no such kind".
func TestExplainNamesTheKindsPresent(t *testing.T) {
	nodes, _ := discFixture()
	d := Explain(nodes, nil, pred(t, map[string]string{"kind": "route"}), true, false)
	if d == nil {
		t.Fatal("no disclosure for an impossible --kind")
	}
	if len(d.Misses) != 1 {
		t.Fatalf("want 1 miss, got %+v", d.Misses)
	}
	m := d.Misses[0]
	if m.Dimension != DimKind || m.Value != "route" || m.Stream != "node" {
		t.Fatalf("wrong miss: %+v", m)
	}
	want := []string{"dependency", "file", "function", "port"} // sorted, deterministic
	if strings.Join(m.Present, ",") != strings.Join(want, ",") {
		t.Fatalf("present kinds %v, want %v", m.Present, want)
	}
	if !strings.Contains(m.Message, `kind "route"`) {
		t.Fatalf("message does not name the value: %q", m.Message)
	}
}

// The noise guard: a REAL kind narrowed to nothing is a legitimate empty answer
// and must never be decorated. If this ever fires, the feature has become the
// kind of chatter readers learn to skip.
func TestExplainStaysSilentOnLegitimateEmpty(t *testing.T) {
	nodes, edges := discFixture()
	cases := []map[string]string{
		{"kind": "file", "label": "nothing-matches-this"},
		{"kind": "port", "where": "direction=provides,transport=config.env"},
		{"relation": "calls", "from": "nope"},
		{"kind": "function", "file-type": "code"},
	}
	for _, kv := range cases {
		if d := Explain(nodes, edges, pred(t, kv), true, true); d != nil {
			t.Fatalf("%v: decorated a legitimate empty result: %+v", kv, d.Misses)
		}
	}
}

func TestExplainRelationAndWhere(t *testing.T) {
	nodes, edges := discFixture()

	d := Explain(nil, edges, pred(t, map[string]string{"relation": "serves"}), false, true)
	if d == nil || d.Misses[0].Dimension != DimRelation {
		t.Fatalf("no relation disclosure: %+v", d)
	}
	if strings.Join(d.Misses[0].Present, ",") != "calls,contains" {
		t.Fatalf("relations present %v", d.Misses[0].Present)
	}

	// An absent KEY names the keys that exist.
	d = Explain(nodes, nil, pred(t, map[string]string{"where": "transprt=network.http"}), true, false)
	if d == nil || d.Misses[0].Dimension != DimWhereKey {
		t.Fatalf("no where-key disclosure: %+v", d)
	}
	if !contains(d.Misses[0].Present, "transport") {
		t.Fatalf("keys present %v does not offer transport", d.Misses[0].Present)
	}

	// A real key with an absent VALUE names the values that exist.
	d = Explain(nodes, nil, pred(t, map[string]string{"where": "direction=provded"}), true, false)
	if d == nil || d.Misses[0].Dimension != DimWhereValue {
		t.Fatalf("no where-value disclosure: %+v", d)
	}
	if strings.Join(d.Misses[0].Present, ",") != "consumes,provides" {
		t.Fatalf("values present %v", d.Misses[0].Present)
	}
}

// The `nodes` verb reads only the node stream, so it must never volunteer
// relations — and `edges` must never volunteer kinds.
func TestExplainRespectsTheStreamTheVerbRead(t *testing.T) {
	nodes, edges := discFixture()
	if d := Explain(nodes, edges, pred(t, map[string]string{"relation": "serves"}), true, false); d != nil {
		t.Fatalf("node-only verb reported about relations: %+v", d.Misses)
	}
	if d := Explain(nodes, edges, pred(t, map[string]string{"kind": "route"}), false, true); d != nil {
		t.Fatalf("edge-only verb reported about kinds: %+v", d.Misses)
	}
}

// Bounded + deterministic: sorted, capped at showCap, with the omitted count.
func TestExplainSuggestionIsSortedCappedAndCounted(t *testing.T) {
	var nodes []schema.Node
	for i := 0; i < showCap+7; i++ {
		nodes = append(nodes, schema.Node{ID: string(rune('a'+i)) + "1", Kind: "k" + string(rune('A'+i))})
	}
	d := Explain(nodes, nil, pred(t, map[string]string{"kind": "nope"}), true, false)
	if d == nil {
		t.Fatal("no disclosure")
	}
	m := d.Misses[0]
	if len(m.Present) != showCap {
		t.Fatalf("present list not capped: %d", len(m.Present))
	}
	if m.Omitted != 7 {
		t.Fatalf("omitted %d, want 7", m.Omitted)
	}
	for i := 1; i < len(m.Present); i++ {
		if m.Present[i-1] >= m.Present[i] {
			t.Fatalf("present list not sorted: %v", m.Present)
		}
	}
}

// A high-cardinality key must suggest NOTHING rather than an arbitrary prefix.
func TestExplainRefusesToGuessPastCollectCap(t *testing.T) {
	var nodes []schema.Node
	for i := 0; i < collectCap+50; i++ {
		nodes = append(nodes, schema.Node{ID: "n" + itoa(i), Kind: "k" + itoa(i)})
	}
	d := Explain(nodes, nil, pred(t, map[string]string{"kind": "nope"}), true, false)
	if d == nil {
		t.Fatal("no disclosure")
	}
	if len(d.Misses[0].Present) != 0 || d.Misses[0].Omitted != 0 {
		t.Fatalf("guessed from a capped set: %+v", d.Misses[0])
	}
}

func TestExplainScopeContains(t *testing.T) {
	nodes, _ := discFixture()
	if d := Explain(nodes, nil, pred(t, map[string]string{"scope": "nosuch"}), true, false); d == nil ||
		d.Misses[0].Dimension != DimScope {
		t.Fatalf("no scope disclosure: %+v", d)
	}
	if d := Explain(nodes, nil, pred(t, map[string]string{"scope": "runtime"}), true, false); d != nil {
		t.Fatalf("decorated a real scope: %+v", d.Misses)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
