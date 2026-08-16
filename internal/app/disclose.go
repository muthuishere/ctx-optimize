package app

// Wiring for graphfilter's impossible-filter disclosure (see
// internal/graphfilter/disclose.go for the defect and the rule).
//
// WHERE IT GOES, and why it is not a field on stdout:
//
//   - HUMAN output — the note rides on stdout, hanging off the count line
//     (`(0 nodes)  — no node in this store has kind "route"; …`), because that
//     is where the reader already is.
//   - MACHINE output (--json / --ndjson) — stdout is NEVER touched. `nodes`,
//     `edges` and `deps` emit a BARE JSON ARRAY, `export` can emit dot/graphml/
//     csv/a file, and every --ndjson stream is one record per line: there is no
//     document to add a field to without changing the shape every existing
//     script parses. So the disclosure is written to STDERR as one JSON object,
//     `{"filter_disclosure": {...}}` — a field, machine-readable, and incapable
//     of corrupting a parse of stdout.
//
// Exit code stays 0 either way. This is disclosure, not an error.

import (
	"encoding/json"
	"io"

	"github.com/muthuishere/ctx-optimize/internal/graphfilter"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// discloser remembers the UNFILTERED streams a verb loaded, so an empty result
// can be explained against what the store actually holds. Holding the pre-filter
// slices is the whole trick: the filtered ones are empty by definition and can
// prove nothing.
type discloser struct {
	pred     graphfilter.Pred
	nodes    []schema.Node
	edges    []schema.Edge
	useNodes bool
	useEdges bool
}

func newDiscloser(pred graphfilter.Pred, nodes []schema.Node, edges []schema.Edge, useNodes, useEdges bool) *discloser {
	return &discloser{pred: pred, nodes: nodes, edges: edges, useNodes: useNodes, useEdges: useEdges}
}

// explain returns the disclosure when the verb's result is empty AND some value
// in the predicate is present nowhere in the store. A non-empty result, or an
// empty one whose every predicate value does exist, returns nil — a legitimate
// empty answer is never decorated.
func (d *discloser) explain(empty bool) *graphfilter.Disclosure {
	if d == nil || !empty || d.pred.Empty() {
		return nil
	}
	return graphfilter.Explain(d.nodes, d.edges, d.pred, d.useNodes, d.useEdges)
}

// disclosureJSON is the stderr envelope. The field name — filter_disclosure —
// is the documented contract.
type disclosureJSON struct {
	FilterDisclosure *graphfilter.Disclosure `json:"filter_disclosure"`
}

// emitDisclosure writes the machine form: exactly one JSON line on stderr, or
// nothing at all. Never touches stdout.
func emitDisclosure(w io.Writer, d *graphfilter.Disclosure) {
	if w == nil || d == nil || len(d.Misses) == 0 {
		return
	}
	enc := json.NewEncoder(w)
	_ = enc.Encode(disclosureJSON{FilterDisclosure: d})
}

// discloseTo routes one disclosure to the right place for the output mode the
// user asked for: stderr JSON for machines, a stdout block for humans.
func discloseTo(stdout, stderr io.Writer, d *graphfilter.Disclosure, machine bool) {
	if d == nil || len(d.Misses) == 0 {
		return
	}
	if machine {
		emitDisclosure(stderr, d)
		return
	}
	io.WriteString(stdout, d.Block())
}
