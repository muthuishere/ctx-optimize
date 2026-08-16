package graphfilter

// A filter that CANNOT POSSIBLY MATCH used to return a plausible empty answer.
//
//	ctx-optimize nodes --kind route   ->   (0 nodes)      exit 0
//
// There is no `route` kind — served routes are `port` nodes with
// direction=provides — and our own shipped authoring guide taught that exact
// command, so an agent following our instructions concluded the repo serves
// nothing. Same shape as the silently-ignored flag and the silently-dropped
// repeated --where (flagcheck.go): the reader is handed a well-formed answer to
// a question the tool never actually asked.
//
// `--kind` is deliberately an OPEN vocabulary — adapters mint their own kinds —
// so REJECTING an unknown kind would be wrong. DISCLOSURE is the fix: when a
// predicate value appears NOWHERE in the store, say so and name what does
// exist. Exit code stays 0; an empty result is a legitimate answer and scripts
// depend on that.
//
// The two cases must be told apart precisely, or the note becomes noise people
// learn to skip:
//
//   - `--kind file --label zzzz` -> 0 nodes. `file` EXISTS. This is a
//     legitimate empty answer and gets NO note.
//   - `--kind route`             -> 0 nodes. `route` exists on no node at all.
//     Only this gets the note.
//
// So the test is per-VALUE and per-DIMENSION, independent of the rest of the
// predicate: "does any record in this stream carry this value on this
// dimension?" A value that is present somewhere is never decorated, whatever
// else the predicate did.
//
// Cost: Explain is called ONLY on an empty result, and only by verbs that have
// already materialized the graph (loadGraph). Nobody pays for it on a hit.
// Suggestion sets are collected in ONE pass over the already-parsed slices,
// bounded, and only for the dimensions the predicate actually constrains.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// showCap is how many present values a note names. collectCap bounds memory (and
// the honesty of the list) for a high-cardinality key like `id`: past it we stop
// collecting and suggest nothing rather than suggest a biased sample.
const (
	showCap    = 20
	collectCap = 500
)

// Dimension names as they appear in the note and in the JSON field.
const (
	DimKind       = "kind"
	DimFileType   = "file_type"
	DimRelation   = "relation"
	DimConfidence = "confidence"
	DimProducer   = "producer"
	DimScope      = "scope"
	DimWhereKey   = "where-key"
	DimWhereValue = "where-value"
)

// Miss is one predicate value that matches NOTHING in the store — not "nothing
// after the other conditions", but nothing at all on that dimension.
type Miss struct {
	Dimension string   `json:"dimension"`         // kind | file_type | relation | confidence | producer | where-key | where-value
	Stream    string   `json:"stream"`            // "node" or "edge"
	Key       string   `json:"key,omitempty"`     // the --where key, for where-* misses
	Value     string   `json:"value"`             // the value that is present nowhere
	Present   []string `json:"present"`           // sorted, capped: what DOES exist on this dimension
	Omitted   int      `json:"omitted,omitempty"` // how many present values the cap left out
	Message   string   `json:"message"`           // the one-line human sentence
}

// Disclosure is what an empty-but-impossible filter reports. It is a
// DISCLOSURE, never an error: exit code stays 0.
type Disclosure struct {
	Misses []Miss `json:"misses"`
}

// Explain returns the disclosure for a predicate that produced an empty result,
// or nil when every value it names exists somewhere (a legitimate empty). Call
// it ONLY when the result is empty.
//
// useNodes/useEdges say which streams the verb actually consumed, so `nodes`
// never reports about relations and `edges` never reports about kinds.
func Explain(nodes []schema.Node, edges []schema.Edge, p Pred, useNodes, useEdges bool) *Disclosure {
	var misses []Miss
	if useNodes {
		misses = append(misses, nodeOwnMisses(nodes, p)...)
	}
	if useEdges {
		misses = append(misses, edgeOwnMisses(edges, p)...)
	}
	// --where and --producer apply to BOTH streams, so a verb that reads both
	// (query/report/hubs/export) must only disclose a key/value that is absent
	// from BOTH. `--where direction=provides` is a node-side condition edges
	// were never expected to satisfy; reporting "no edge carries direction"
	// there would be exactly the false alarm this feature must not generate.
	misses = append(misses, sharedMisses(nodes, edges, p, useNodes, useEdges)...)
	if len(misses) == 0 {
		return nil
	}
	return &Disclosure{Misses: misses}
}

// valueSet is a bounded distinct-value collector. Once it passes collectCap it
// stops growing and reports capped: a suggestion drawn from an arbitrary
// prefix of a 2.85M-value key would be worse than no suggestion.
type valueSet struct {
	seen   map[string]bool
	capped bool
}

func newValueSet() *valueSet { return &valueSet{seen: map[string]bool{}} }

func (v *valueSet) add(s string) {
	if v.capped || v.seen[s] {
		return
	}
	if len(v.seen) >= collectCap {
		v.capped = true
		return
	}
	v.seen[s] = true
}

func (v *valueSet) has(s string) bool { return v.seen[s] }

// containsAny reports whether any collected value CONTAINS sub — the test for a
// substring dimension. A capped collector answers true (unknown, so never
// disclose on a partial set).
func (v *valueSet) containsAny(sub string) bool {
	if v.capped {
		return true
	}
	for s := range v.seen {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// list returns the sorted, display-capped values plus how many were omitted. A
// capped collector suggests nothing (the set it holds is not the whole truth).
func (v *valueSet) list() ([]string, int) {
	if v.capped {
		return nil, 0
	}
	out := make([]string, 0, len(v.seen))
	for s := range v.seen {
		out = append(out, s)
	}
	sort.Strings(out)
	if len(out) > showCap {
		return out[:showCap], len(out) - showCap
	}
	return out, 0
}

// nodeOwnMisses covers the dimensions only NODES have: kind, file_type, scope.
func nodeOwnMisses(nodes []schema.Node, p Pred) []Miss {
	wantKind, wantFT := len(p.Kinds) > 0, len(p.FileTypes) > 0
	wantScope := p.ScopeContains != ""
	if !wantKind && !wantFT && !wantScope {
		return nil
	}
	kinds, ftypes, scopes := newValueSet(), newValueSet(), newValueSet()
	for i := range nodes {
		n := &nodes[i]
		if wantKind {
			kinds.add(n.Kind)
		}
		if wantFT {
			ftypes.add(n.FileType)
		}
		if wantScope {
			sc := n.Scope
			if sc == "" {
				sc = n.Metadata["scopes"]
			}
			if sc != "" {
				scopes.add(sc)
			}
		}
	}
	var out []Miss
	out = append(out, setMisses("node", DimKind, p.Kinds, kinds)...)
	out = append(out, setMisses("node", DimFileType, p.FileTypes, ftypes)...)
	// --scope is a CONTAINS match on a small enum ("runtime", "dev,runtime"),
	// so "no node's scope contains this" is decidable and worth saying. --label
	// and --id-prefix are free text and are deliberately left alone: a miss
	// there is the normal outcome of a search, not an impossible filter.
	if wantScope && !scopes.containsAny(p.ScopeContains) {
		present, omitted := scopes.list()
		out = append(out, Miss{
			Dimension: DimScope, Stream: "node", Value: p.ScopeContains,
			Present: present, Omitted: omitted,
			Message: fmt.Sprintf("no node in this store has a scope containing %q", p.ScopeContains),
		})
	}
	return out
}

// edgeOwnMisses covers the dimensions only EDGES have: relation, confidence.
func edgeOwnMisses(edges []schema.Edge, p Pred) []Miss {
	wantRel, wantConf := len(p.Relations) > 0, len(p.Confidences) > 0
	if !wantRel && !wantConf {
		return nil
	}
	rels, confs := newValueSet(), newValueSet()
	for i := range edges {
		e := &edges[i]
		if wantRel {
			rels.add(e.Relation)
		}
		if wantConf {
			confs.add(e.Confidence)
		}
	}
	var out []Miss
	out = append(out, setMisses("edge", DimRelation, p.Relations, rels)...)
	out = append(out, setMisses("edge", DimConfidence, p.Confidences, confs)...)
	return out
}

// sharedMisses covers --producer and --where, which both streams can satisfy.
// A miss is reported only when EVERY consumed stream misses.
func sharedMisses(nodes []schema.Node, edges []schema.Edge, p Pred, useNodes, useEdges bool) []Miss {
	wantProd := p.Producer != ""
	if !wantProd && len(p.Where) == 0 {
		return nil
	}
	stream := "record"
	switch {
	case useNodes && !useEdges:
		stream = "node"
	case useEdges && !useNodes:
		stream = "edge"
	}
	producers := newValueSet()
	conds := condCollectors(p.Where)
	if useNodes {
		for i := range nodes {
			n := &nodes[i]
			if wantProd {
				producers.add(n.Metadata["producer"])
			}
			collectConds(conds, n.Metadata, func(k string) (string, bool) { return nodeField(*n, k) })
		}
	}
	if useEdges {
		for i := range edges {
			e := &edges[i]
			if wantProd {
				producers.add(e.Metadata["producer"])
			}
			collectConds(conds, e.Metadata, func(k string) (string, bool) { return edgeField(*e, k) })
		}
	}
	var out []Miss
	if wantProd {
		out = append(out, setMisses(stream, DimProducer, []string{p.Producer}, producers)...)
	}
	out = append(out, whereMisses(stream, conds, func() *valueSet {
		v := newValueSet()
		if useNodes {
			addNodeKeys(v, nodes)
		}
		if useEdges {
			addEdgeKeys(v, edges)
		}
		return v
	})...)
	return out
}

// setMisses reports every value of an exact-match OR-set that the store has
// never seen. A set like `--kind route,file` where only `route` is absent still
// discloses `route`: "no node has kind route" is a fact whenever it is printed.
func setMisses(stream, dim string, want []string, got *valueSet) []Miss {
	var out []Miss
	for _, w := range want {
		if got.has(w) {
			continue
		}
		present, omitted := got.list()
		out = append(out, Miss{
			Dimension: dim, Stream: stream, Value: w,
			Present: present, Omitted: omitted,
			Message: sentence(stream, dim, "", w),
		})
	}
	return out
}

// condCollector tracks, for ONE --where condition, whether its key resolves on
// any record and which values that key takes.
type condCollector struct {
	cond    Cond
	present bool // the key resolved on at least one record
	vals    *valueSet
}

func condCollectors(conds []Cond) []*condCollector {
	if len(conds) == 0 {
		return nil
	}
	out := make([]*condCollector, len(conds))
	for i, c := range conds {
		out[i] = &condCollector{cond: c, vals: newValueSet()}
	}
	return out
}

func collectConds(cs []*condCollector, meta map[string]string, field func(string) (string, bool)) {
	for _, c := range cs {
		v, ok := resolveKey(c.cond.Key, meta, field)
		if !ok {
			continue
		}
		c.present = true
		c.vals.add(v)
	}
}

// whereMisses distinguishes an ABSENT KEY (`--where transprt=http` — the key is
// on no record, so name the keys that exist) from an ABSENT VALUE (`--where
// kind=route` — the key is real, the value is not). A `~` (contains) condition
// gets the key check only: substring membership is not enumerable from a value
// set, so we never guess there.
func whereMisses(stream string, cs []*condCollector, keys func() *valueSet) []Miss {
	var out []Miss
	keysNeeded := false
	for _, c := range cs {
		if !c.present {
			keysNeeded = true
		}
	}
	var keySet *valueSet
	if keysNeeded {
		keySet = keys() // second pass, paid ONLY when a key turned out absent
	}
	for _, c := range cs {
		switch {
		case !c.present:
			present, omitted := keySet.list()
			out = append(out, Miss{
				Dimension: DimWhereKey, Stream: stream, Key: c.cond.Key, Value: c.cond.Key,
				Present: present, Omitted: omitted,
				Message: sentence(stream, DimWhereKey, c.cond.Key, c.cond.Key),
			})
		case !c.cond.Contains && !c.vals.has(c.cond.Val):
			present, omitted := c.vals.list()
			out = append(out, Miss{
				Dimension: DimWhereValue, Stream: stream, Key: c.cond.Key, Value: c.cond.Val,
				Present: present, Omitted: omitted,
				Message: sentence(stream, DimWhereValue, c.cond.Key, c.cond.Val),
			})
		}
	}
	return out
}

// addNodeKeys / addEdgeKeys enumerate every addressable --where key: the
// top-level fields plus every metadata key present. Only reached when a key was
// absent everywhere, which is the one case where the list is worth a second pass.
func addNodeKeys(v *valueSet, nodes []schema.Node) {
	for _, f := range []string{"id", "label", "kind", "file_type", "source", "location", "scope"} {
		v.add(f)
	}
	for i := range nodes {
		if v.capped { // nothing left to learn; stop walking 2.85M metadata maps
			return
		}
		for k := range nodes[i].Metadata {
			v.add(k)
		}
	}
}

func addEdgeKeys(v *valueSet, edges []schema.Edge) {
	for _, f := range []string{"source", "target", "relation", "confidence"} {
		v.add(f)
	}
	for i := range edges {
		if v.capped {
			return
		}
		for k := range edges[i].Metadata {
			v.add(k)
		}
	}
}

func sentence(stream, dim, key, val string) string {
	switch dim {
	case DimWhereKey:
		return fmt.Sprintf("no %s in this store carries the key %q", stream, key)
	case DimWhereValue:
		return fmt.Sprintf("no %s in this store has %s=%q", stream, key, val)
	default:
		return fmt.Sprintf("no %s in this store has %s %q", stream, dim, val)
	}
}

// label names the "present:" list for a dimension.
func (m Miss) presentLabel() string {
	switch m.Dimension {
	case DimWhereKey:
		return "keys present"
	case DimWhereValue:
		return fmt.Sprintf("values present for %s", m.Key)
	case DimScope:
		return "scopes present"
	case DimFileType:
		return "file types present"
	default:
		return m.Dimension + "s present"
	}
}

// Note renders the disclosure for HUMAN output, indented to hang off a count
// line: `(0 nodes)` + Note() reads as one sentence. Machine output never uses
// this — see the filter_disclosure field.
func (d *Disclosure) Note() string {
	if d == nil || len(d.Misses) == 0 {
		return ""
	}
	var b strings.Builder
	for i, m := range d.Misses {
		if i == 0 {
			b.WriteString("  — " + m.Message)
		} else {
			b.WriteString("\n            " + m.Message)
		}
		if len(m.Present) > 0 {
			b.WriteString(";\n            " + m.presentLabel() + ": " + strings.Join(m.Present, ", "))
			if m.Omitted > 0 {
				b.WriteString(fmt.Sprintf(" (+%d more)", m.Omitted))
			}
		}
	}
	return b.String()
}

// Block renders the same disclosure as a standalone paragraph, for verbs whose
// human output has no count line to hang off (report, hubs, affected, query).
func (d *Disclosure) Block() string {
	if d == nil || len(d.Misses) == 0 {
		return ""
	}
	var b strings.Builder
	for _, m := range d.Misses {
		b.WriteString("note: " + m.Message)
		if len(m.Present) > 0 {
			b.WriteString(";\n      " + m.presentLabel() + ": " + strings.Join(m.Present, ", "))
			if m.Omitted > 0 {
				b.WriteString(fmt.Sprintf(" (+%d more)", m.Omitted))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
