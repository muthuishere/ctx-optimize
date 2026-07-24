package analyze

import (
	"errors"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func fixture() ([]schema.Node, []schema.Edge) {
	n := func(id, label, kind string) schema.Node {
		return schema.Node{ID: id, Label: label, Kind: kind, FileType: "code", Source: id}
	}
	e := func(src, tgt, rel string) schema.Edge {
		return schema.Edge{Source: src, Target: tgt, Relation: rel, Confidence: "EXTRACTED"}
	}
	// api -> service -> db ; worker -> service ; orphan
	return []schema.Node{
			n("api", "ApiHandler", "function"),
			n("service", "RefundService", "class"),
			n("db", "refunds_table", "table"),
			n("worker", "RetryWorker", "function"),
			n("orphan", "Orphan", "function"),
		}, []schema.Edge{
			e("api", "service", "calls"),
			e("service", "db", "references"),
			e("worker", "service", "calls"),
		}
}

func TestResolve(t *testing.T) {
	nodes, _ := fixture()
	if n, err := Resolve(nodes, "db"); err != nil || n.ID != "db" {
		t.Fatalf("by id: %v %v", n, err)
	}
	if n, err := Resolve(nodes, "refundservice"); err != nil || n.ID != "service" {
		t.Fatalf("by label ci: %v %v", n, err)
	}
	if n, err := Resolve(nodes, "refund service"); err != nil || n.ID != "service" {
		t.Fatalf("fuzzy tokens: %v %v", n, err)
	}
	if _, err := Resolve(nodes, "zzz-nothing"); err == nil {
		t.Fatal("expected miss")
	}
}

func TestShortestPath(t *testing.T) {
	nodes, edges := fixture()
	steps, err := ShortestPath(nodes, edges, "ApiHandler", "refunds_table")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].To != "service" || steps[1].To != "db" {
		t.Fatalf("steps: %+v", steps)
	}
	// Undirected: works backward too.
	if _, err := ShortestPath(nodes, edges, "refunds_table", "RetryWorker"); err != nil {
		t.Fatalf("reverse path: %v", err)
	}
	if _, err := ShortestPath(nodes, edges, "ApiHandler", "Orphan"); err == nil {
		t.Fatal("expected no-path error")
	}
}

func TestAffected(t *testing.T) {
	nodes, edges := fixture()
	target, impacts, err := Affected(nodes, edges, "refunds_table", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if target.ID != "db" {
		t.Fatalf("target: %s", target.ID)
	}
	// depth1: service (references db); depth2: api + worker (call service)
	if len(impacts) != 3 {
		t.Fatalf("impacts: %+v", impacts)
	}
	if impacts[0].Node.ID != "service" || impacts[0].Depth != 1 {
		t.Fatalf("first impact: %+v", impacts[0])
	}
	// relation filter: only 'calls' edges traversed → db has none incoming.
	_, impacts, _ = Affected(nodes, edges, "refunds_table", 2, []string{"calls"})
	if len(impacts) != 0 {
		t.Fatalf("relation filter leaked: %+v", impacts)
	}
	// depth 1 stops early
	_, impacts, _ = Affected(nodes, edges, "refunds_table", 1, nil)
	if len(impacts) != 1 {
		t.Fatalf("depth cap: %+v", impacts)
	}
}

func TestExplain(t *testing.T) {
	nodes, edges := fixture()
	ex, err := Explain(nodes, edges, "RefundService")
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.Outgoing["references"]) != 1 || len(ex.Incoming["calls"]) != 2 {
		t.Fatalf("explanation: %+v", ex)
	}
	text := RenderExplanation(ex)
	for _, want := range []string{"RefundService is a class", "references (1)", "calls (2)", "→ db", "← api"} {
		if !strings.Contains(text, want) {
			t.Fatalf("render missing %q:\n%s", want, text)
		}
	}
}

func TestHubs(t *testing.T) {
	nodes, edges := fixture()
	hubs := Hubs(nodes, edges, 10)
	if len(hubs) != 4 { // orphan excluded (degree 0)
		t.Fatalf("hubs: %+v", hubs)
	}
	if hubs[0].Node.ID != "service" || hubs[0].In != 2 || hubs[0].Out != 1 {
		t.Fatalf("top hub: %+v", hubs[0])
	}
	if got := Hubs(nodes, edges, 1); len(got) != 1 {
		t.Fatalf("top cap: %+v", got)
	}
}

func TestCard(t *testing.T) {
	nodes, edges := fixture()
	nodes[1].Metadata = map[string]string{"signature": "class RefundService:", "doc": "# refunds money"}
	c, err := Card(nodes, edges, "RefundService")
	if err != nil {
		t.Fatal(err)
	}
	if c.Signature != "class RefundService:" || c.Doc != "# refunds money" {
		t.Fatalf("card metadata: %+v", c)
	}
	if len(c.CalledBy) != 2 || c.CalledBy[0] != "api" || c.CalledBy[1] != "worker" {
		t.Fatalf("called_by: %v", c.CalledBy)
	}
	if got := c.Other["references →"]; len(got) != 1 || got[0] != "db" {
		t.Fatalf("other relations: %v", c.Other)
	}
	text := RenderCard(c)
	for _, want := range []string{"sig: class RefundService:", "doc: # refunds money", "called by (2):", "references → (1):"} {
		if !strings.Contains(text, want) {
			t.Fatalf("render missing %q:\n%s", want, text)
		}
	}
}

// Agents invent qualified id formats (chromium forensics: `content.Foo.Bar`,
// `Ns::Class::Method`) — the resolver must strip qualifiers, and a total miss
// must suggest nearest labels instead of dead-ending.
func TestResolveQualifiersAndDidYouMean(t *testing.T) {
	nodes, _ := fixture()
	for _, guess := range []string{
		"acme::billing::RefundService",
		"acme.billing.RefundService",
		"src/billing/RefundService",
	} {
		n, err := Resolve(nodes, guess)
		if err != nil || n.ID != "service" {
			t.Fatalf("qualified guess %q: %v %v", guess, n, err)
		}
	}
	// Total miss (no token overlap) but trigram-close: must suggest, not dead-end.
	_, err := Resolve(nodes, "RefndServce")
	if err == nil {
		t.Fatal("expected a miss for RefndServce")
	}
	if !strings.Contains(err.Error(), "did you mean") || !strings.Contains(err.Error(), "RefundService") {
		t.Fatalf("miss should suggest nearest labels, got: %v", err)
	}
	// Nothing near: plain miss, no noise suggestions.
	if _, err := Resolve(nodes, "qqqqqq"); err == nil || strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("far miss must not suggest: %v", err)
	}
}

func TestLastSegment(t *testing.T) {
	for in, want := range map[string]string{
		"A::B::C":      "C",
		"pkg.Cls.meth": "meth",
		"a/b/c.go":     "go",
		"plain":        "plain",
	} {
		if got := lastSegment(in); got != want {
			t.Fatalf("lastSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

// Ambiguity guard (ADR 2026-07-16-verify-verb): a fuzzy TIE refuses with
// ranked candidates — graphify's silent matches[0] is the anti-pattern —
// while a clear fuzzy winner still resolves, labeled as fuzzy.
func TestResolveViaAmbiguityAndHonesty(t *testing.T) {
	mk := func(id, label string) schema.Node {
		return schema.Node{ID: id, Label: label, Kind: "function", FileType: "code", Source: "a.go"}
	}
	// Two near names score alike for "PayInvoice" → refuse with both.
	nodes := []schema.Node{mk("a.go::PayInvoiceRetry", "PayInvoiceRetry"), mk("a.go::PayInvoiceOnce", "PayInvoiceOnce")}
	_, _, err := ResolveVia(nodes, "PayInvoice")
	var amb *AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("tie must refuse with AmbiguousError, got %v", err)
	}
	if len(amb.Candidates) != 2 || amb.Candidates[0].ID != "a.go::PayInvoiceOnce" {
		t.Fatalf("candidates wrong: %+v", amb.Candidates)
	}

	// Only one near name → resolves, honestly labeled fuzzy.
	n, via, err := ResolveVia(nodes[:1], "PayInvoice")
	if err != nil || n.Label != "PayInvoiceRetry" || via != "fuzzy" {
		t.Fatalf("clear winner must resolve as fuzzy: %v %q %v", n, via, err)
	}

	// Exact tiers report themselves.
	if _, via, _ := ResolveVia(nodes, "a.go::PayInvoiceOnce"); via != "exact-id" {
		t.Fatalf("via = %q, want exact-id", via)
	}
	if _, via, _ := ResolveVia(nodes, "payinvoiceretry"); via != "exact-label" {
		t.Fatalf("via = %q, want exact-label", via)
	}
	if _, via, _ := ResolveVia(nodes, "pkg.Class.PayInvoiceOnce"); via != "last-segment" {
		t.Fatalf("via = %q, want last-segment", via)
	}
}

// ADR 2026-07-24-answer-quality F1: a real declaration beats a module://
// import stub on any label tie — the measured `card url_for` failure (judge
// 0.66) returned the stub because it sorted first by ID.
func TestResolveDeclBeatsImportStub(t *testing.T) {
	nodes := []schema.Node{
		{ID: "module://url_for", Label: "url_for", Kind: "module"},
		{ID: "src/flask/helpers.py::url_for", Label: "url_for", Kind: "function",
			Source: "src/flask/helpers.py", Location: "L200-L251"},
	}
	n, via, err := ResolveVia(nodes, "url_for")
	if err != nil || n.ID != "src/flask/helpers.py::url_for" {
		t.Fatalf("decl must beat stub on label tie: got %v via %s err %v", n, via, err)
	}
	// Fuzzy tier: same preference (query tokens tie both).
	n, _, err = ResolveVia(nodes, "url for helpers")
	if err != nil || strings.HasPrefix(n.ID, "module://") {
		t.Fatalf("fuzzy must not return the stub when a decl ties: got %v err %v", n, err)
	}
	// Stub-only store: the stub is still resolvable (never orphaned).
	only := nodes[:1]
	if n, _, err := ResolveVia(only, "url_for"); err != nil || n.ID != "module://url_for" {
		t.Fatalf("stub-only resolve broke: %v %v", n, err)
	}
}
