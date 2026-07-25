package analyze

// report.go — the one-call orientation answer: "explain this repo to me."
// Composes what the store already knows into a single artifact: subsystems,
// hubs, the seams BETWEEN subsystems, and — the part nobody else reports — what
// the graph could not resolve.
//
// Deliberately NOT graphify's "surprising connections". That ranks edges by
// UNRELIABILITY (their scoring is AMBIGUOUS:3, INFERRED:2, EXTRACTED:1), so the
// most prominent finding in their report is the one least likely to be true.
// Under "say no instead of being wrong" that is exactly backwards. We report
// BRIDGES instead — edges that genuinely connect two detected subsystems, facts
// first — and we report our abstentions as their own section rather than
// dressing them up as insights.

import (
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// bridgeRelations: the relations that mean "A depends on B in code". A seam is
// a dependency crossing a subsystem boundary — nesting, manifests, git
// co-change and doc links are different lanes and are not architecture.
var bridgeRelations = map[string]bool{
	"calls": true, "imports": true, "uses": true,
	"handles": true, "depends_on": true, "extends": true, "implements": true,
}

const (
	reportSubsystems = 10 // largest first
	reportHubs       = 10
	reportBridges    = 15
	reportGaps       = 10
)

// Bridge is one edge whose endpoints live in DIFFERENT subsystems — the seam of
// the architecture. Both endpoints and both community labels are named, so the
// claim is checkable.
type Bridge struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Relation   string `json:"relation"`
	Confidence string `json:"confidence"`
	FromComm   string `json:"from_community"`
	ToComm     string `json:"to_community"`
	Degree     int    `json:"degree"` // combined endpoint degree — how load-bearing the seam is
}

// Gap is a symbol the store could NOT fully answer for: call sites naming it
// exist but could not be attributed, because the name is defined more than once.
type Gap struct {
	Node       string `json:"node"`
	Label      string `json:"label"`
	Source     string `json:"source"`
	Unresolved int    `json:"unattributed_callers"`
}

type ReportData struct {
	Nodes       int            `json:"nodes"`
	Edges       int            `json:"edges"`
	Confidence  map[string]int `json:"confidence"`
	Subsystems  []Community    `json:"subsystems"`
	Hubs        []Hub          `json:"hubs"`
	Bridges     []Bridge       `json:"bridges"`
	Gaps        []Gap          `json:"gaps,omitempty"`
	TotalGaps   int            `json:"total_unattributed_callers"`
	SubsystemsN int            `json:"subsystems_total"`
}

// Report composes the orientation answer. Everything structural is computed
// from FACTS only (WithoutAmbiguous, applied by the callees); the AMBIGUOUS
// edges are used solely to count what we could not resolve.
func Report(nodes []schema.Node, edges []schema.Edge) *ReportData {
	r := &ReportData{Nodes: len(nodes), Edges: len(edges), Confidence: map[string]int{}}
	for _, e := range edges {
		r.Confidence[e.Confidence]++
	}

	facts := WithoutAmbiguous(edges)
	comms := Communities(nodes, facts)
	r.SubsystemsN = len(comms)
	r.Subsystems = comms
	if len(r.Subsystems) > reportSubsystems {
		r.Subsystems = r.Subsystems[:reportSubsystems]
	}
	// Import stubs (module://strings, module://os …) are the most-connected nodes
	// in any repo and say nothing about ITS architecture — every file imports
	// strings. graphify excludes stdlib from god-node ranking for the same
	// reason (their analyze.py:9). Filtered here rather than in Hubs() so the
	// standalone `hubs` verb keeps its shipped behaviour.
	real := make([]schema.Node, 0, len(nodes))
	for _, n := range nodes {
		if !isImportStub(&n) {
			real = append(real, n)
		}
	}
	r.Hubs = Hubs(real, facts, reportHubs)

	// Bridges: an edge between two different communities. Only meaningful once
	// clustering found more than one subsystem.
	commOf := map[string]string{}
	for _, c := range comms {
		for _, m := range c.Members {
			commOf[m] = c.Label
		}
	}
	byID := map[string]schema.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	degree := map[string]int{}
	for _, e := range facts {
		degree[e.Source]++
		degree[e.Target]++
	}
	var bridges []Bridge
	for _, e := range facts {
		a, aok := commOf[e.Source]
		b, bok := commOf[e.Target]
		if !aok || !bok || a == b {
			continue
		}
		// A seam is a CODE DEPENDENCY crossing a subsystem boundary. Allowlist,
		// not denylist, so a relation added later cannot silently pollute this
		// section. Each exclusion below was measured on this repo, in order:
		//   contains        — nesting, not dependency (`analyze.go contains
		//                     analyze.go::AmbiguousError.Error`); took every
		//                     slot once stdlib imports were excluded
		//   declares        — manifest lane (`go.mod declares dep:wazero`)
		//   co_changed_with — git history, not structure; it flooded the list
		//                     with everything app.go was ever committed beside
		//   references      — doc→doc links
		if !bridgeRelations[e.Relation] {
			continue
		}
		// An external module is not a subsystem either. Without that check every
		// `import "strings"` looks like an architectural seam — measured: the
		// first 15 bridges on this repo were all
		// `X imports module://strings|os|fmt`.
		sn, tn := byID[e.Source], byID[e.Target]
		if isImportStub(&sn) || isImportStub(&tn) {
			continue
		}
		bridges = append(bridges, Bridge{
			Source: e.Source, Target: e.Target, Relation: e.Relation,
			Confidence: e.Confidence, FromComm: a, ToComm: b,
			Degree: degree[e.Source] + degree[e.Target],
		})
	}
	// Facts before inferences, then load-bearing first, then id — deterministic.
	sort.Slice(bridges, func(i, j int) bool {
		ci, cj := bridges[i].Confidence == schema.Extracted, bridges[j].Confidence == schema.Extracted
		if ci != cj {
			return ci
		}
		if bridges[i].Degree != bridges[j].Degree {
			return bridges[i].Degree > bridges[j].Degree
		}
		if bridges[i].Source != bridges[j].Source {
			return bridges[i].Source < bridges[j].Source
		}
		return bridges[i].Target < bridges[j].Target
	})
	// One row per SUBSYSTEM PAIR, not per edge. "Where subsystems touch" is a
	// question about pairs, and without this a single over-attracting node fills
	// the table: measured here, every slot was `… calls AmbiguousError.Error`,
	// because call resolution keys on the BARE method name so every `err.Error()`
	// in the repo resolves to the one decl labelled `Error`. That is a real
	// precision bug in resolution (recorded separately) — but even with a perfect
	// graph, 15 edges between the same two subsystems is one fact, not 15.
	seenPair := map[string]bool{}
	deduped := bridges[:0]
	for _, b := range bridges {
		key := b.FromComm + "\x00" + b.ToComm
		if seenPair[key] {
			continue
		}
		seenPair[key] = true
		deduped = append(deduped, b)
	}
	bridges = deduped
	if len(bridges) > reportBridges {
		bridges = bridges[:reportBridges]
	}
	r.Bridges = bridges

	// Gaps: what we refused to guess. This section is the point of the report —
	// every other tool tells you what it found.
	unresolved := map[string]int{}
	for _, e := range edges {
		if e.Confidence == schema.Ambiguous && e.Relation == "calls" {
			unresolved[e.Target]++
			r.TotalGaps++
		}
	}
	for id, n := range unresolved {
		node := byID[id]
		r.Gaps = append(r.Gaps, Gap{Node: id, Label: node.Label, Source: node.Source, Unresolved: n})
	}
	sort.Slice(r.Gaps, func(i, j int) bool {
		if r.Gaps[i].Unresolved != r.Gaps[j].Unresolved {
			return r.Gaps[i].Unresolved > r.Gaps[j].Unresolved
		}
		return r.Gaps[i].Node < r.Gaps[j].Node
	})
	if len(r.Gaps) > reportGaps {
		r.Gaps = r.Gaps[:reportGaps]
	}
	return r
}

// RenderReport is the human/markdown form — stable, sorted, diffable, so a
// report committed to a repo produces a readable diff when the architecture
// actually moves.
func RenderReport(r *ReportData) string {
	var sb strings.Builder
	sb.WriteString("# Graph report\n\n")
	sb.WriteString("| | |\n|---|---:|\n")
	writeRow := func(k string, v int) { sb.WriteString("| " + k + " | " + itoa(v) + " |\n") }
	writeRow("nodes", r.Nodes)
	writeRow("edges", r.Edges)
	for _, c := range []string{schema.Extracted, schema.Inferred, schema.Ambiguous} {
		if n := r.Confidence[c]; n > 0 {
			writeRow("  "+c, n)
		}
	}
	writeRow("subsystems", r.SubsystemsN)

	sb.WriteString("\n## Subsystems\n\n")
	if len(r.Subsystems) == 0 {
		sb.WriteString("None detected — the graph has no clustered structure.\n")
	} else {
		sb.WriteString("| subsystem | size | dirs |\n|---|---:|---|\n")
		for _, c := range r.Subsystems {
			sb.WriteString("| " + c.Label + " | " + itoa(len(c.Members)) + " | " + strings.Join(c.Dirs, ", ") + " |\n")
		}
	}

	sb.WriteString("\n## Hubs\n\n")
	sb.WriteString("| node | in | out | source |\n|---|---:|---:|---|\n")
	for _, h := range r.Hubs {
		sb.WriteString("| " + h.Node.Label + " | " + itoa(h.In) + " | " + itoa(h.Out) + " | " + h.Node.Source + " |\n")
	}

	sb.WriteString("\n## Bridges — where subsystems touch\n\n")
	if len(r.Bridges) == 0 {
		sb.WriteString("None — the detected subsystems do not reference each other.\n")
	} else {
		sb.WriteString("Edges whose endpoints sit in different subsystems: the seams, and the\n")
		sb.WriteString("first place a change leaks across an architectural boundary.\n\n")
		sb.WriteString("| from | relation | to | confidence |\n|---|---|---|---|\n")
		for _, b := range r.Bridges {
			sb.WriteString("| " + b.Source + " | " + b.Relation + " | " + b.Target + " | " + b.Confidence + " |\n")
		}
	}

	sb.WriteString("\n## What this graph does NOT know\n\n")
	if r.TotalGaps == 0 {
		sb.WriteString("Every call site resolved to exactly one declaration.\n")
	} else {
		sb.WriteString(itoa(r.TotalGaps) + " call sites could not be attributed: the callee name is defined\n")
		sb.WriteString("more than once, so they were NOT guessed. Grep the name to settle each one.\n\n")
		sb.WriteString("| symbol | unattributed callers | source |\n|---|---:|---|\n")
		for _, g := range r.Gaps {
			sb.WriteString("| " + g.Label + " | " + itoa(g.Unresolved) + " | " + g.Source + " |\n")
		}
	}
	return sb.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
