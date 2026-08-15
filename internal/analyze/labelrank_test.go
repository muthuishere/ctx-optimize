package analyze

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// flask, reduced: FIVE nodes tie on a case-insensitive label match for "Flask",
// and only one of them is the class. The old smallest-ID tiebreak answered with
// the README heading; demoting prose alone promoted the dependency; demoting
// that promoted the config key. Rank by what a node IS and the tie resolves
// once, for all of them.
func TestLabelTieResolvesToTheDeclaration(t *testing.T) {
	nodes := []schema.Node{
		{ID: "README.md::flask", Label: "Flask", Kind: "section", FileType: "document", Source: "README.md", Location: "L3-L54"},
		{ID: "dep://pypi/flask", Label: "flask", Kind: "dependency", FileType: "manifest", Source: "pyproject.toml"},
		{ID: "module://Flask", Label: "Flask", Kind: "import", FileType: "code", Source: "app.py"},
		{ID: "pyproject.toml#flask", Label: "flask", Kind: "config_key", FileType: "config", Source: "pyproject.toml", Location: "L82"},
		{ID: "src/flask/app.py::Flask", Label: "Flask", Kind: "class", FileType: "code", Source: "src/flask/app.py", Location: "L109-L1625"},
	}
	got, via, err := ResolveVia(nodes, "Flask")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.ID != "src/flask/app.py::Flask" {
		t.Errorf("resolved to %s (%s); want the class — a definition must beat prose, a dependency and a config key", got.ID, got.Kind)
	}
	if via != "exact-label" {
		t.Errorf("via = %q, want exact-label", via)
	}
}

// The rank only settles a TIE. A repo whose ONLY node for a name is the
// dependency must still resolve to it — otherwise demoting mentions would turn
// real answers into misses.
func TestLoneMentionStillResolves(t *testing.T) {
	for _, n := range []schema.Node{
		{ID: "dep://npm/react", Label: "react", Kind: "dependency", FileType: "manifest", Source: "package.json"},
		{ID: "README.md::setup", Label: "Setup", Kind: "section", FileType: "document", Source: "README.md", Location: "L1-L9"},
	} {
		got, _, err := ResolveVia([]schema.Node{n}, n.Label)
		if err != nil || got == nil || got.ID != n.ID {
			t.Errorf("lone %s node %q did not resolve: got %v err %v", n.Kind, n.ID, got, err)
		}
	}
}

// A refusal is only actionable if each candidate can be cited. Without the
// location the reader must run another command per candidate, which is what
// capped five judged questions at 0.75 (file+symbol, no line).
func TestAmbiguousCandidatesCarryLocation(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a.go::DoMergeThing", Label: "DoMergeThing", Kind: "function", FileType: "code", Source: "a.go", Location: "L10-L20"},
		{ID: "b.go::MergeHelper", Label: "MergeHelper", Kind: "function", FileType: "code", Source: "b.go", Location: "L30-L40"},
	}
	_, _, err := ResolveVia(nodes, "merge thing helper")
	var amb *AmbiguousError
	if !asAmbiguous(err, &amb) {
		t.Fatalf("want AmbiguousError, got %v", err)
	}
	for _, c := range amb.Candidates {
		if c.Location == "" || c.Source == "" {
			t.Errorf("candidate %s lost its location/source: %+v", c.ID, c)
		}
	}
	msg := amb.Error()
	for _, want := range []string{"L10-L20", "L30-L40"} {
		if !strings.Contains(msg, want) {
			t.Errorf("rendered refusal omits %s — the reader cannot cite a candidate:\n%s", want, msg)
		}
	}
	// It must still REFUSE. Adding locations is not permission to pick.
	if !strings.Contains(msg, "refusing to guess") {
		t.Errorf("refusal lost its refusal:\n%s", msg)
	}
}

func asAmbiguous(err error, out **AmbiguousError) bool {
	a, ok := err.(*AmbiguousError)
	if ok {
		*out = a
	}
	return ok
}
