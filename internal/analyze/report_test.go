package analyze

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// reportFixture: two dense subsystems joined by ONE real call, plus stdlib
// import stubs (which every repo has and which mean nothing about ITS
// architecture) and one unattributed call site.
func reportFixture() ([]schema.Node, []schema.Edge) {
	var nodes []schema.Node
	var edges []schema.Edge
	add := func(id, kind, src string) {
		nodes = append(nodes, schema.Node{ID: id, Kind: kind, Label: id, Source: src, Location: "L1-L2"})
	}
	link := func(a, b, rel, conf string) {
		edges = append(edges, schema.Edge{Source: a, Target: b, Relation: rel, Confidence: conf, Weight: 1})
	}
	for _, g := range []string{"a", "b"} {
		for i := '1'; i <= '8'; i++ {
			add(g+string(i), "function", g+"/"+g+".go")
		}
		for i := '1'; i <= '8'; i++ {
			for j := i + 1; j <= '8'; j++ {
				link(g+string(i), g+string(j), "calls", schema.Extracted)
			}
		}
	}
	// The seam.
	link("a1", "b1", "calls", schema.Extracted)
	// Noise that must NOT be reported as architecture.
	add("module://strings", "module", "module://strings")
	for _, g := range []string{"a", "b"} {
		for i := '1'; i <= '8'; i++ {
			link(g+string(i), "module://strings", "imports", schema.Extracted)
		}
	}
	link("a2", "a3", "contains", schema.Extracted)
	// An abstention.
	link("b2", "a4", "calls", schema.Ambiguous)
	link("b3", "a4", "calls", schema.Ambiguous)
	return nodes, edges
}

// The gaps section is the whole point: every comparable tool reports what it
// found, none report what they refused to guess.
func TestReportSaysWhatItDoesNotKnow(t *testing.T) {
	nodes, edges := reportFixture()
	r := Report(nodes, edges)
	if r.TotalGaps != 2 {
		t.Errorf("TotalGaps = %d, want 2", r.TotalGaps)
	}
	if len(r.Gaps) == 0 || r.Gaps[0].Node != "a4" || r.Gaps[0].Unresolved != 2 {
		t.Errorf("top gap wrong: %+v", r.Gaps)
	}
	out := RenderReport(r)
	if !strings.Contains(out, "What this graph does NOT know") {
		t.Error("report omitted the abstention section")
	}
	if !strings.Contains(out, "were NOT guessed") {
		t.Error("report does not say the call sites were refused rather than missing")
	}
}

// Import stubs are the most-connected nodes in any repo and say nothing about
// its architecture. graphify excludes stdlib from god-node ranking for the same
// reason; we had not, so the first report listed strings/os/fmt as the top hubs.
func TestReportHubsExcludeImportStubs(t *testing.T) {
	nodes, edges := reportFixture()
	r := Report(nodes, edges)
	for _, h := range r.Hubs {
		if strings.HasPrefix(h.Node.ID, "module://") {
			t.Errorf("import stub %q ranked as a hub — it displaces real abstractions", h.Node.ID)
		}
	}
	if len(r.Hubs) == 0 {
		t.Error("no hubs at all — the filter ate everything")
	}
}

// A seam is a code dependency crossing a subsystem boundary. Nesting,
// manifests, git co-change and doc links are different lanes; each of these was
// measured filling the section before the allowlist landed.
func TestReportBridgesAreDependenciesOnly(t *testing.T) {
	nodes, edges := reportFixture()
	r := Report(nodes, edges)
	for _, b := range r.Bridges {
		if !bridgeRelations[b.Relation] {
			t.Errorf("bridge with non-dependency relation %q: %+v", b.Relation, b)
		}
		if strings.HasPrefix(b.Source, "module://") || strings.HasPrefix(b.Target, "module://") {
			t.Errorf("bridge touches an external module — not a subsystem: %+v", b)
		}
		if b.FromComm == b.ToComm {
			t.Errorf("bridge inside one subsystem: %+v", b)
		}
	}
}

// One row per subsystem PAIR. Without this a single over-attracting node fills
// the table — measured on this repo, every slot was `… calls
// AmbiguousError.Error`, because call resolution keys on the bare method name.
func TestReportBridgesDedupeBySubsystemPair(t *testing.T) {
	nodes, edges := reportFixture()
	// Many edges between the same two subsystems.
	for i := '2'; i <= '8'; i++ {
		edges = append(edges, schema.Edge{
			Source: "a" + string(i), Target: "b" + string(i),
			Relation: "calls", Confidence: schema.Extracted, Weight: 1,
		})
	}
	r := Report(nodes, edges)
	seen := map[string]bool{}
	for _, b := range r.Bridges {
		key := b.FromComm + "->" + b.ToComm
		if seen[key] {
			t.Errorf("subsystem pair %q reported twice — dedupe failed", key)
		}
		seen[key] = true
	}
}

// The report must be deterministic: same graph in, byte-identical report out,
// so a committed report diffs only when the architecture actually moved.
func TestReportIsDeterministic(t *testing.T) {
	nodes, edges := reportFixture()
	a := RenderReport(Report(nodes, edges))
	b := RenderReport(Report(nodes, edges))
	if a != b {
		t.Error("two renders of the same graph differ")
	}
	// And independent of input order.
	rev := make([]schema.Edge, len(edges))
	for i := range edges {
		rev[i] = edges[len(edges)-1-i]
	}
	if c := RenderReport(Report(nodes, rev)); c != a {
		t.Error("report depends on edge input order")
	}
}

// Structure must be computed from facts. An AMBIGUOUS edge may only ever
// influence the gaps count.
func TestReportStructureIgnoresAmbiguous(t *testing.T) {
	nodes, edges := reportFixture()
	withMaybes := Report(nodes, edges)
	facts := WithoutAmbiguous(edges)
	factsOnly := Report(nodes, facts)

	if len(withMaybes.Subsystems) != len(factsOnly.Subsystems) {
		t.Errorf("subsystem count changed with ambiguous edges: %d vs %d",
			len(withMaybes.Subsystems), len(factsOnly.Subsystems))
	}
	if len(withMaybes.Bridges) != len(factsOnly.Bridges) {
		t.Errorf("bridge count changed with ambiguous edges: %d vs %d",
			len(withMaybes.Bridges), len(factsOnly.Bridges))
	}
	if factsOnly.TotalGaps != 0 {
		t.Errorf("facts-only report reported %d gaps", factsOnly.TotalGaps)
	}
	if withMaybes.TotalGaps == 0 {
		t.Error("gaps disappeared — the abstention count must survive the filter")
	}
}
