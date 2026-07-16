// change-plan — the first composed one-call verb (ADR 2026-07-16-composed-
// answers, A2): what an agent previously stitched from query + card +
// affected + a tests grep arrives as ONE bounded answer — what the symbol is,
// who calls it, the blast radius, WHICH TESTS TO RUN (the derived tests-for
// view: affected filtered to test declarations, no persisted edge), what
// historically co-changes, and a confidence footer (A1) separating parsed
// fact from inference. Deterministic composition of existing analyze verbs;
// nothing new is indexed.
package app

import (
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/analyze"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// testPathRe recognizes test declarations by source-path convention — the
// same heuristic the golden judge and the tests-for spike validated.
var testPathRe = regexp.MustCompile(`(_test\.(go|py|rb)$|\.(test|spec)\.[jt]sx?$|(^|/)tests?/|\.Tests(/|$)|(^|/)test_)`)

// caps keep the composed answer bounded (the ADR budget: ≤3,500 tokens
// default output). Overflow is summarized as a count, never truncated silently.
const (
	planMaxCallers = 10
	planMaxImpacts = 15
	planMaxTests   = 10
	planMaxCochng  = 8
)

type changePlan struct {
	Target     schema.Node      `json:"target"`
	Signature  string           `json:"signature,omitempty"`
	Doc        string           `json:"doc,omitempty"`
	CalledBy   []string         `json:"called_by,omitempty"`
	Calls      []string         `json:"calls,omitempty"`
	Impacts    []analyze.Impact `json:"impacts"`
	Tests      []analyze.Impact `json:"tests"`
	CoChanges  []string         `json:"co_changes,omitempty"`
	Confidence planConfidence   `json:"confidence"`
	Truncated  map[string]int   `json:"truncated,omitempty"` // section -> hidden count
}

type planConfidence struct {
	Extracted int `json:"extracted_edges"`
	Inferred  int `json:"inferred_edges"`
	CoChange  int `json:"co_change_evidence"`
}

// cmdChangePlan composes card + affected + tests-for + co-change into one
// bounded, cited answer.
func cmdChangePlan(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if len(f.args) != 1 {
		return fmt.Errorf(`usage: ctx-optimize change-plan "X" [--depth N] [--json]`)
	}
	nodes, edges, sc, storeRoot, err := loadGraphScoped(f)
	if err != nil {
		return err
	}
	t0 := time.Now()
	cw := &countingWriter{w: stdout}
	stdout = cw
	st, _ := openStore(f)
	defer func() { served(st, "change-plan", f.args[0], 1, cw, t0) }()

	depth := 2
	if v, ok := f.strs["depth"]; ok {
		fmt.Sscanf(v, "%d", &depth)
	}

	card, cerr := analyze.Card(nodes, edges, f.args[0])
	// Module-scope miss: mirror affected/card — answer repo-wide, say where.
	scopeNote := ""
	if cerr != nil && sc != nil && sc.kind == scopeModule {
		if fn, fe, ferr := federatedAll(sc, storeRoot); ferr == nil {
			if c2, err2 := analyze.Card(fn, fe, f.args[0]); err2 == nil {
				scopeNote = fmt.Sprintf("[not in %s — found in %s]", sc.moduleName, moduleOwnerOf(sc, c2.Node.Source))
				card, cerr = c2, nil
				nodes, edges = fn, fe
			}
		}
	}
	if cerr != nil {
		return cerr
	}
	_, impacts, aerr := analyze.Affected(nodes, edges, card.Node.Label, depth, nil)
	if aerr != nil {
		// Label-resolution edge case: fall back to the asked name.
		_, impacts, aerr = analyze.Affected(nodes, edges, f.args[0], depth, nil)
		if aerr != nil {
			return aerr
		}
	}

	plan := changePlan{
		Target: card.Node, Signature: card.Signature, Doc: card.Doc,
		CalledBy: card.CalledBy, Calls: card.Calls,
		Truncated: map[string]int{},
	}
	for _, im := range impacts {
		switch {
		case im.Via == "co_changed_with":
			plan.Confidence.CoChange++
			plan.CoChanges = append(plan.CoChanges, im.Node.Source)
		case testPathRe.MatchString(im.Node.Source):
			plan.Tests = append(plan.Tests, im)
		default:
			plan.Impacts = append(plan.Impacts, im)
		}
	}
	// Confidence: parsed fact vs inference among the edges that reached the
	// blast set (co-change counted separately as weak evidence).
	seen := map[string]bool{card.Node.ID: true}
	for _, im := range impacts {
		seen[im.Node.ID] = true
	}
	for _, e := range edges {
		if e.Relation == "co_changed_with" || !seen[e.Target] || !seen[e.Source] {
			continue
		}
		if e.Confidence == schema.Extracted {
			plan.Confidence.Extracted++
		} else {
			plan.Confidence.Inferred++
		}
	}
	trim := func(what string, n int, cap int) int {
		if n > cap {
			plan.Truncated[what] = n - cap
			return cap
		}
		return n
	}
	plan.CalledBy = plan.CalledBy[:trim("called_by", len(plan.CalledBy), planMaxCallers)]
	plan.Calls = plan.Calls[:trim("calls", len(plan.Calls), planMaxCallers)]
	plan.Impacts = plan.Impacts[:trim("impacts", len(plan.Impacts), planMaxImpacts)]
	plan.Tests = plan.Tests[:trim("tests", len(plan.Tests), planMaxTests)]
	plan.CoChanges = plan.CoChanges[:trim("co_changes", len(plan.CoChanges), planMaxCochng)]
	if len(plan.Truncated) == 0 {
		plan.Truncated = nil
	}

	if f.bools["json"] {
		out := map[string]any{"result": plan}
		if scopeNote != "" {
			out["scope"] = scopeNote
		}
		return emit(stdout, out)
	}
	if scopeNote != "" {
		fmt.Fprintln(stdout, scopeNote)
	}
	fmt.Fprintf(stdout, "change plan: %s  [%s]  %s %s\n", plan.Target.Label, plan.Target.Kind, plan.Target.Source, plan.Target.Location)
	if plan.Signature != "" {
		fmt.Fprintf(stdout, "  sig: %s\n", plan.Signature)
	}
	if len(plan.CalledBy) > 0 {
		fmt.Fprintf(stdout, "\ncallers (%d):\n", len(plan.CalledBy))
		for _, c := range plan.CalledBy {
			fmt.Fprintf(stdout, "  %s\n", c)
		}
	}
	if len(plan.Impacts) > 0 {
		fmt.Fprintf(stdout, "\nblast radius (depth %d, %d shown):\n", depth, len(plan.Impacts))
		for _, im := range plan.Impacts {
			fmt.Fprintf(stdout, "  d%d %s  [%s]  via %s\n", im.Depth, im.Node.Label, im.Node.Kind, im.Via)
		}
	}
	if len(plan.Tests) > 0 {
		fmt.Fprintf(stdout, "\ntests to run (%d):\n", len(plan.Tests))
		for _, im := range plan.Tests {
			fmt.Fprintf(stdout, "  %s  (%s)\n", im.Node.Label, im.Node.Source)
		}
	} else {
		fmt.Fprintf(stdout, "\ntests to run: none reachable in the graph — check conventions outside this module\n")
	}
	if len(plan.CoChanges) > 0 {
		fmt.Fprintf(stdout, "\nhistorically co-changes with:\n")
		for _, c := range plan.CoChanges {
			fmt.Fprintf(stdout, "  %s\n", c)
		}
	}
	fmt.Fprintf(stdout, "\nconfidence: %d extracted, %d inferred edges; %d co-change evidence\n",
		plan.Confidence.Extracted, plan.Confidence.Inferred, plan.Confidence.CoChange)
	for what, n := range plan.Truncated {
		fmt.Fprintf(stdout, "  (+%d more %s — --json for all)\n", n, what)
	}
	return nil
}
