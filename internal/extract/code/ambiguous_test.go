package code

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// ADR 2026-07-25-abstain-out-loud. Before this, a call site whose callee name
// was defined more than once was DISCARDED SILENTLY: nothing wrong entered the
// graph, but the graph looked complete, and an agent reading `called by` had no
// way to learn that other call sites existed. The motto is "say no instead of
// being wrong" — and saying nothing is not saying no.
//
// So the candidates are emitted as AMBIGUOUS (a shortlist to grep, never a
// claim) and every traversal verb filters them out by default. These tests pin
// both halves; the filter half lives in internal/analyze.
func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func callEdges(b *schema.Batch, conf string) []schema.Edge {
	var out []schema.Edge
	for _, e := range b.Edges {
		if e.Relation == "calls" && e.Confidence == conf {
			out = append(out, e)
		}
	}
	return out
}

// Two definitions of the same name → the call site is undecidable, so BOTH
// candidates are shortlisted as AMBIGUOUS and neither is promoted to INFERRED.
func TestAmbiguousCalleeIsShortlistedNotGuessed(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"a/a.go":   "package a\n\nfunc Handle() {}\n",
		"b/b.go":   "package b\n\nfunc Handle() {}\n",
		"c/use.go": "package c\n\nfunc Caller() {\n\tHandle()\n}\n",
	})
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	amb := callEdges(b, schema.Ambiguous)
	if len(amb) != 2 {
		t.Fatalf("want 2 AMBIGUOUS candidates, got %d: %+v", len(amb), amb)
	}
	for _, e := range amb {
		if e.Confidence != "AMBIGUOUS" {
			t.Errorf("edge is not labelled AMBIGUOUS: %+v", e)
		}
	}
	// Critically: an undecidable call must NOT also appear as a confident edge.
	for _, e := range callEdges(b, "INFERRED") {
		if filepath.Base(e.Target) != "" && (e.Target == amb[0].Target || e.Target == amb[1].Target) {
			t.Errorf("undecidable call was ALSO emitted as INFERRED: %+v", e)
		}
	}
}

// A unique name still resolves confidently — the feature must not turn certain
// answers into maybes.
func TestUniqueCalleeStaysInferred(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"a/a.go":   "package a\n\nfunc OnlyOne() {}\n",
		"c/use.go": "package c\n\nfunc Caller() {\n\tOnlyOne()\n}\n",
	})
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(callEdges(b, "INFERRED")); n != 1 {
		t.Errorf("want 1 INFERRED call, got %d", n)
	}
	if n := len(callEdges(b, schema.Ambiguous)); n != 0 {
		t.Errorf("unique callee produced %d AMBIGUOUS edges", n)
	}
}

// An unknown callee (stdlib, a dependency) has NOTHING in this repo to point
// at. There is no shortlist to offer, so nothing is emitted — this is the case
// that must stay silent, and conflating it with ambiguity would inflate the
// "unattributed" number with things that are not gaps.
func TestUnknownCalleeEmitsNothing(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"c/use.go": "package c\n\nimport \"fmt\"\n\nfunc Caller() {\n\tfmt.Println(\"x\")\n}\n",
	})
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(callEdges(b, schema.Ambiguous)); n != 0 {
		t.Errorf("unknown callee produced %d AMBIGUOUS edges — external is not ambiguous", n)
	}
}

// Above the cap we shortlist NOTHING. A name like `get`/`new` has dozens of
// definitions; 40 maybes is worse than grep, and it is exactly the god-node
// pollution docs/VISION.md:284 measured.
func TestAboveCapShortlistsNothing(t *testing.T) {
	files := map[string]string{"z/use.go": "package z\n\nfunc Caller() {\n\tGet()\n}\n"}
	for _, p := range []string{"a", "b", "c", "d", "e", "f"} {
		files[p+"/"+p+".go"] = "package " + p + "\n\nfunc Get() {}\n"
	}
	dir := writeRepo(t, files)
	orig := ambiguousCap
	defer func() { ambiguousCap = orig }()

	ambiguousCap = 4 // 6 candidates > cap
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(callEdges(b, schema.Ambiguous)); n != 0 {
		t.Errorf("6 candidates with cap 4 shortlisted %d edges — should refuse", n)
	}

	ambiguousCap = 6 // now within the cap
	b, err = Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(callEdges(b, schema.Ambiguous)); n != 6 {
		t.Errorf("6 candidates with cap 6 shortlisted %d edges, want 6", n)
	}
}

// The load-bearing guarantee: this feature is PURELY ADDITIVE. Measured on this
// repo across caps 0/2/3/4/6/10/100, INFERRED stayed 2561 and EXTRACTED stayed
// 2956 at every value — only the AMBIGUOUS count moved. If a future change to
// resolution starts converting confident edges into maybes (or vice versa),
// this fails.
func TestAmbiguousShortlistIsPurelyAdditive(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"a/a.go":   "package a\n\nfunc Dup() {}\nfunc Uniq() {}\n",
		"b/b.go":   "package b\n\nfunc Dup() {}\n",
		"c/use.go": "package c\n\nfunc Caller() {\n\tDup()\n\tUniq()\n}\n",
	})
	orig := ambiguousCap
	defer func() { ambiguousCap = orig }()

	ambiguousCap = 0 // pre-change behaviour: shortlist nothing
	base, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	ambiguousCap = orig
	got, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, conf := range []string{"EXTRACTED", "INFERRED"} {
		if a, b := len(callEdges(base, conf)), len(callEdges(got, conf)); a != b {
			t.Errorf("%s call edges changed from %d to %d — the shortlist must be additive only", conf, a, b)
		}
	}
	if len(callEdges(base, schema.Ambiguous)) != 0 {
		t.Fatal("baseline should have no AMBIGUOUS edges")
	}
	if len(callEdges(got, schema.Ambiguous)) == 0 {
		t.Error("no AMBIGUOUS edges emitted for a duplicated name")
	}
	// And node counts are untouched — no new nodes, ever.
	if len(base.Nodes) != len(got.Nodes) {
		t.Errorf("node count changed %d → %d; shortlisting must not create nodes", len(base.Nodes), len(got.Nodes))
	}
}
