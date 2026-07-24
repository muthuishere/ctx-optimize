// Package graphfilter is the ONE predicate + projection engine shared by every
// read verb (nodes/edges/deps, export, query pre-rank narrowing, hubs,
// affected) so consumers never pipe our output through jq/python (ADR
// 2026-07-24-portable-export-consumption). It operates on the already-loaded
// []schema.Node / []schema.Edge slices (from loadGraphScoped, so federation is
// free) in a single in-process pass — no serialization, no subprocess, no
// second parse: that is the whole speed win over `export | jq`.
package graphfilter

import (
	"fmt"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// Pred is a conjunction of dimensions. A dimension with an OR-set matches when
// the record's value is in the set; multiple dimensions AND together. The zero
// Pred matches everything (Empty() == true).
type Pred struct {
	Kinds         []string // node kind ∈ set
	FileTypes     []string // node file_type ∈ set
	IDPrefix      string   // node/edge id/source prefix (dep:, k8s://, module://)
	Label         string   // node label contains
	Relations     []string // edge relation ∈ set
	Confidences   []string // edge confidence ∈ set
	From, To      string   // edge source/target exact
	Producer      string   // metadata["producer"] ==
	ScopeContains string   // node-only: metadata["scopes"] contains (dep aggregate)
	Where         []Cond   // ANDed metadata/field conditions (shared: both streams)
}

// Cond is one `k=v` (exact) or `k~v` (contains) condition, resolved against a
// record's top-level string fields OR metadata.<k> (bare key = metadata key).
type Cond struct {
	Key      string
	Val      string
	Contains bool // ~ vs =
}

// Empty reports whether the predicate constrains nothing (fast path: skip).
func (p Pred) Empty() bool {
	return len(p.Kinds) == 0 && len(p.FileTypes) == 0 && p.IDPrefix == "" &&
		p.Label == "" && len(p.Relations) == 0 && len(p.Confidences) == 0 &&
		p.From == "" && p.To == "" && p.Producer == "" && p.ScopeContains == "" &&
		len(p.Where) == 0
}

func inSet(v string, set []string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

// MatchNode reports whether a node satisfies every node-relevant dimension.
// Edge-only dimensions (relation/confidence/from/to) are ignored for nodes.
func (p Pred) MatchNode(n schema.Node) bool {
	if len(p.Kinds) > 0 && !inSet(n.Kind, p.Kinds) {
		return false
	}
	if len(p.FileTypes) > 0 && !inSet(n.FileType, p.FileTypes) {
		return false
	}
	if p.IDPrefix != "" && !strings.HasPrefix(n.ID, p.IDPrefix) {
		return false
	}
	if p.Label != "" && !strings.Contains(n.Label, p.Label) {
		return false
	}
	if p.Producer != "" && n.Metadata["producer"] != p.Producer {
		return false
	}
	if p.ScopeContains != "" && !strings.Contains(n.Metadata["scopes"], p.ScopeContains) {
		return false
	}
	return matchWhere(p.Where, n.Metadata, func(k string) (string, bool) {
		return nodeField(n, k)
	})
}

// MatchEdge reports whether an edge satisfies every edge-relevant dimension.
// Node-only dimensions (kind/file_type/label) are ignored for edges.
func (p Pred) MatchEdge(e schema.Edge) bool {
	if len(p.Relations) > 0 && !inSet(e.Relation, p.Relations) {
		return false
	}
	if len(p.Confidences) > 0 && !inSet(e.Confidence, p.Confidences) {
		return false
	}
	if p.From != "" && e.Source != p.From {
		return false
	}
	if p.To != "" && e.Target != p.To {
		return false
	}
	if p.IDPrefix != "" && !strings.HasPrefix(e.Source, p.IDPrefix) && !strings.HasPrefix(e.Target, p.IDPrefix) {
		return false
	}
	if p.Producer != "" && e.Metadata["producer"] != p.Producer {
		return false
	}
	return matchWhere(p.Where, e.Metadata, func(k string) (string, bool) {
		return edgeField(e, k)
	})
}

func matchWhere(conds []Cond, meta map[string]string, field func(string) (string, bool)) bool {
	for _, c := range conds {
		got, ok := resolveKey(c.Key, meta, field)
		if !ok {
			return false // missing key = no-match, never a crash (stress-test #4)
		}
		if c.Contains {
			if !strings.Contains(got, c.Val) {
				return false
			}
		} else if got != c.Val {
			return false
		}
	}
	return true
}

// resolveKey looks a where-key up as a top-level field first (via field), then
// as metadata.<k> or a bare metadata key.
func resolveKey(key string, meta map[string]string, field func(string) (string, bool)) (string, bool) {
	if mk, ok := strings.CutPrefix(key, "metadata."); ok {
		v, ok := meta[mk]
		return v, ok
	}
	if v, ok := field(key); ok {
		return v, true
	}
	v, ok := meta[key]
	return v, ok
}

func nodeField(n schema.Node, k string) (string, bool) {
	switch k {
	case "id":
		return n.ID, true
	case "label":
		return n.Label, true
	case "kind":
		return n.Kind, true
	case "file_type":
		return n.FileType, true
	case "source":
		return n.Source, true
	case "location":
		return n.Location, true
	}
	return "", false
}

func edgeField(e schema.Edge, k string) (string, bool) {
	switch k {
	case "source", "from":
		return e.Source, true
	case "target", "to":
		return e.Target, true
	case "relation":
		return e.Relation, true
	case "confidence":
		return e.Confidence, true
	}
	return "", false
}

// Apply narrows both slices by the predicate in one pass. Node-only and
// edge-only dimensions apply to their own stream; a Pred that constrains only
// nodes leaves edges untouched, and vice-versa (see hasNodeDims/hasEdgeDims).
func Apply(nodes []schema.Node, edges []schema.Edge, p Pred) ([]schema.Node, []schema.Edge) {
	if p.Empty() {
		return nodes, edges
	}
	outN := nodes
	if p.hasNodeDims() {
		outN = nodes[:0:0]
		for _, n := range nodes {
			if p.MatchNode(n) {
				outN = append(outN, n)
			}
		}
	}
	outE := edges
	if p.hasEdgeDims() {
		outE = edges[:0:0]
		for _, e := range edges {
			if p.MatchEdge(e) {
				outE = append(outE, e)
			}
		}
	}
	return outN, outE
}

// hasNodeDims / hasEdgeDims decide which streams a predicate touches. Shared
// dims (id-prefix, producer, where) count for both.
func (p Pred) sharedDims() bool {
	return p.IDPrefix != "" || p.Producer != "" || len(p.Where) > 0
}
func (p Pred) hasNodeDims() bool {
	return len(p.Kinds) > 0 || len(p.FileTypes) > 0 || p.Label != "" || p.ScopeContains != "" || p.sharedDims()
}
func (p Pred) hasEdgeDims() bool {
	return len(p.Relations) > 0 || len(p.Confidences) > 0 || p.From != "" || p.To != "" || p.sharedDims()
}

// ParsePred builds a predicate from the shared flag strings. `--scope X` is
// sugar for `--where scopes~X` (the dep-node aggregate). Comma splits an
// OR-set within a dimension; `--where` is comma-separated ANDed conds.
func ParsePred(str map[string]string) (Pred, error) {
	p := Pred{
		Kinds:       splitSet(str["kind"]),
		FileTypes:   splitSet(str["file-type"]),
		Relations:   splitSet(str["relation"]),
		Confidences: splitSet(str["confidence"]),
		IDPrefix:    str["id-prefix"],
		Label:       str["label"],
		From:        str["from"],
		To:          str["to"],
		Producer:    str["producer"],
	}
	if s := strings.TrimSpace(str["scope"]); s != "" {
		p.ScopeContains = s
	}
	if w := strings.TrimSpace(str["where"]); w != "" {
		for _, part := range strings.Split(w, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			c, err := parseCond(part)
			if err != nil {
				return Pred{}, err
			}
			p.Where = append(p.Where, c)
		}
	}
	return p, nil
}

func parseCond(part string) (Cond, error) {
	if i := strings.IndexByte(part, '~'); i >= 0 {
		return Cond{Key: strings.TrimSpace(part[:i]), Val: strings.TrimSpace(part[i+1:]), Contains: true}, nil
	}
	if i := strings.IndexByte(part, '='); i >= 0 {
		return Cond{Key: strings.TrimSpace(part[:i]), Val: strings.TrimSpace(part[i+1:])}, nil
	}
	return Cond{}, fmt.Errorf("bad --where condition %q (want key=value or key~value)", part)
}

func splitSet(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ProjectNode / ProjectEdge reduce a record to the requested dotted fields
// (id, label, kind, file_type, source, location, metadata.<k>). Empty fields
// returns the whole record unchanged (caller emits the struct).
func ProjectNode(n schema.Node, fields []string) map[string]any {
	m := map[string]any{}
	for _, f := range fields {
		if v, ok := resolveKey(f, n.Metadata, func(k string) (string, bool) { return nodeField(n, k) }); ok {
			m[f] = v
		} else {
			m[f] = nil
		}
	}
	return m
}

func ProjectEdge(e schema.Edge, fields []string) map[string]any {
	m := map[string]any{}
	for _, f := range fields {
		if v, ok := resolveKey(f, e.Metadata, func(k string) (string, bool) { return edgeField(e, k) }); ok {
			m[f] = v
		} else {
			m[f] = nil
		}
	}
	return m
}

// Fields parses a --select list into dotted field names (sorted-stable order
// preserved as given).
func Fields(s string) []string { return splitSet(s) }

// SortNodes / SortEdges give deterministic output for snapshots.
func SortNodes(ns []schema.Node) {
	sort.Slice(ns, func(i, j int) bool { return ns[i].ID < ns[j].ID })
}
func SortEdges(es []schema.Edge) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].Source != es[j].Source {
			return es[i].Source < es[j].Source
		}
		if es[i].Target != es[j].Target {
			return es[i].Target < es[j].Target
		}
		return es[i].Relation < es[j].Relation
	})
}
