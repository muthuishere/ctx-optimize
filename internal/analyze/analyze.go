// Package analyze is the deterministic graph-walking layer: shortest path,
// reverse impact (affected), node explanation, hubs (god nodes). Pure
// functions over nodes+edges — no LLM, no store access; the CLI feeds it and
// renders. Everything returns complete, citable data (S1e discipline).
package analyze

import (
	"fmt"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/query"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// Resolve finds the node meant by a human name: exact id, exact label
// (case-insensitive), then best token overlap. Deterministic ties by id.
func Resolve(nodes []schema.Node, name string) (*schema.Node, error) {
	for i := range nodes {
		if nodes[i].ID == name {
			return &nodes[i], nil
		}
	}
	lower := strings.ToLower(name)
	var labelHit *schema.Node
	for i := range nodes {
		if strings.ToLower(nodes[i].Label) == lower {
			if labelHit == nil || nodes[i].ID < labelHit.ID {
				labelHit = &nodes[i]
			}
		}
	}
	if labelHit != nil {
		return labelHit, nil
	}
	// Fuzzy: token overlap against label+id.
	want := query.Tokenize(name)
	if len(want) == 0 {
		return nil, fmt.Errorf("no node named %q", name)
	}
	best, bestScore := -1, 0
	for i := range nodes {
		have := map[string]bool{}
		for _, t := range query.Tokenize(nodes[i].Label + " " + nodes[i].ID) {
			have[t] = true
		}
		s := 0
		for _, t := range want {
			if have[t] {
				s++
			}
		}
		if s > bestScore || (s == bestScore && s > 0 && best >= 0 && nodes[i].ID < nodes[best].ID) {
			best, bestScore = i, s
		}
	}
	if best < 0 || bestScore == 0 {
		return nil, fmt.Errorf("no node matching %q — try `ctx-optimize query %q` to find the right name", name, name)
	}
	return &nodes[best], nil
}

// ---- path ----

// Step is one hop of a path.
type Step struct {
	From     string `json:"from"`
	Relation string `json:"relation"`
	Dir      string `json:"dir"` // "->" or "<-"
	To       string `json:"to"`
}

type hop struct {
	prev string
	e    schema.Edge
	fwd  bool
}

// ShortestPath BFSes the undirected view from a to b.
func ShortestPath(nodes []schema.Node, edges []schema.Edge, a, b string) ([]Step, error) {
	from, err := Resolve(nodes, a)
	if err != nil {
		return nil, err
	}
	to, err := Resolve(nodes, b)
	if err != nil {
		return nil, err
	}
	if from.ID == to.ID {
		return []Step{}, nil
	}
	adj := map[string][]hop{}
	for _, e := range edges {
		adj[e.Source] = append(adj[e.Source], hop{prev: e.Source, e: e, fwd: true})
		adj[e.Target] = append(adj[e.Target], hop{prev: e.Target, e: e, fwd: false})
	}
	// stable neighbor order → stable paths
	for k := range adj {
		hs := adj[k]
		sort.Slice(hs, func(i, j int) bool {
			return other(hs[i]) < other(hs[j])
		})
	}
	visited := map[string]hop{from.ID: {}}
	queue := []string{from.ID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, h := range adj[cur] {
			nxt := other(h)
			if _, seen := visited[nxt]; seen {
				continue
			}
			visited[nxt] = h
			if nxt == to.ID {
				return unwind(visited, from.ID, to.ID), nil
			}
			queue = append(queue, nxt)
		}
	}
	return nil, fmt.Errorf("no path between %q and %q", from.Label, to.Label)
}

func other(h hop) string {
	if h.fwd {
		return h.e.Target
	}
	return h.e.Source
}

func unwind(visited map[string]hop, from, to string) []Step {
	var steps []Step
	cur := to
	for cur != from {
		h := visited[cur]
		dir := "->"
		if !h.fwd {
			dir = "<-"
		}
		steps = append(steps, Step{From: h.prev, Relation: h.e.Relation, Dir: dir, To: cur})
		cur = h.prev
	}
	// reverse into from→to order
	for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
		steps[i], steps[j] = steps[j], steps[i]
	}
	return steps
}

// ---- affected (reverse impact) ----

// Impact is one node reached by reverse traversal from the target.
type Impact struct {
	Node      schema.Node `json:"node"`
	Depth     int         `json:"depth"`
	Via       string      `json:"via"` // relation of the edge that reached it
	DependsOn string      `json:"depends_on"`
}

// Affected walks edges BACKWARD (source depends on target): everything with
// an edge INTO the blast set is impacted, up to depth hops. Optional relation
// filter (empty = all relations).
func Affected(nodes []schema.Node, edges []schema.Edge, name string, depth int, relations []string) (*schema.Node, []Impact, error) {
	target, err := Resolve(nodes, name)
	if err != nil {
		return nil, nil, err
	}
	if depth <= 0 {
		depth = 2
	}
	relOK := func(r string) bool {
		if len(relations) == 0 {
			return true
		}
		for _, want := range relations {
			if r == want {
				return true
			}
		}
		return false
	}
	incoming := map[string][]schema.Edge{}
	for _, e := range edges {
		incoming[e.Target] = append(incoming[e.Target], e)
	}
	byID := map[string]schema.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	seen := map[string]bool{target.ID: true}
	frontier := []string{target.ID}
	var out []Impact
	for d := 1; d <= depth && len(frontier) > 0; d++ {
		var next []string
		for _, id := range frontier {
			es := incoming[id]
			sort.Slice(es, func(i, j int) bool { return es[i].Source < es[j].Source })
			for _, e := range es {
				if seen[e.Source] || !relOK(e.Relation) {
					continue
				}
				seen[e.Source] = true
				out = append(out, Impact{Node: byID[e.Source], Depth: d, Via: e.Relation, DependsOn: id})
				next = append(next, e.Source)
			}
		}
		frontier = next
	}
	return target, out, nil
}

// ---- explain ----

// Explanation is the complete deterministic story of one node.
type Explanation struct {
	Node     schema.Node         `json:"node"`
	Outgoing map[string][]string `json:"outgoing"` // relation → target ids
	Incoming map[string][]string `json:"incoming"` // relation → source ids
}

func Explain(nodes []schema.Node, edges []schema.Edge, name string) (*Explanation, error) {
	n, err := Resolve(nodes, name)
	if err != nil {
		return nil, err
	}
	ex := &Explanation{Node: *n, Outgoing: map[string][]string{}, Incoming: map[string][]string{}}
	for _, e := range edges {
		if e.Source == n.ID {
			ex.Outgoing[e.Relation] = append(ex.Outgoing[e.Relation], e.Target)
		}
		if e.Target == n.ID {
			ex.Incoming[e.Relation] = append(ex.Incoming[e.Relation], e.Source)
		}
	}
	for _, m := range []map[string][]string{ex.Outgoing, ex.Incoming} {
		for k := range m {
			sort.Strings(m[k])
		}
	}
	return ex, nil
}

// RenderExplanation is the plain-language form; --json marshals directly.
func RenderExplanation(ex *Explanation) string {
	var sb strings.Builder
	n := ex.Node
	fmt.Fprintf(&sb, "%s is a %s (%s) from %s", n.Label, n.Kind, n.FileType, n.Source)
	if n.Location != "" {
		fmt.Fprintf(&sb, " %s", n.Location)
	}
	sb.WriteString(".\n")
	if p := n.Metadata["producer"]; p != "" {
		fmt.Fprintf(&sb, "gathered by: %s\n", p)
	}
	writeRels := func(title string, m map[string][]string, arrow string) {
		rels := make([]string, 0, len(m))
		for r := range m {
			rels = append(rels, r)
		}
		sort.Strings(rels)
		for _, r := range rels {
			ids := m[r]
			fmt.Fprintf(&sb, "%s %s (%d):\n", title, r, len(ids))
			for i, id := range ids {
				if i == 15 {
					fmt.Fprintf(&sb, "    … %d more\n", len(ids)-15)
					break
				}
				fmt.Fprintf(&sb, "    %s %s\n", arrow, id)
			}
		}
	}
	writeRels("outgoing", ex.Outgoing, "→")
	writeRels("incoming", ex.Incoming, "←")
	if len(ex.Outgoing) == 0 && len(ex.Incoming) == 0 {
		sb.WriteString("no edges — an isolated node.\n")
	}
	return sb.String()
}

// ---- card (the symbol-card primitive) ----

// CardData is everything an agent needs to reason about one symbol WITHOUT
// opening its file: signature, doc, location, containment, call graph. The
// spike campaign measured pointer-chase reads (find node → open file for the
// signature) as the #1 context waste; this is the fix.
type CardData struct {
	Node      schema.Node         `json:"node"`
	Signature string              `json:"signature,omitempty"`
	Doc       string              `json:"doc,omitempty"`
	Parent    string              `json:"parent,omitempty"`    // what contains it
	Contains  []string            `json:"contains,omitempty"`  // what it contains
	Calls     []string            `json:"calls,omitempty"`     // outgoing calls
	CalledBy  []string            `json:"called_by,omitempty"` // incoming calls
	Imports   []string            `json:"imports,omitempty"`   // file nodes only
	Other     map[string][]string `json:"other,omitempty"`     // any remaining relations, "rel →|←" keyed
}

func Card(nodes []schema.Node, edges []schema.Edge, name string) (*CardData, error) {
	n, err := Resolve(nodes, name)
	if err != nil {
		return nil, err
	}
	c := &CardData{Node: *n, Signature: n.Metadata["signature"], Doc: n.Metadata["doc"]}
	other := map[string][]string{}
	for _, e := range edges {
		switch {
		case e.Source == n.ID && e.Relation == "contains":
			c.Contains = append(c.Contains, e.Target)
		case e.Target == n.ID && e.Relation == "contains":
			c.Parent = e.Source
		case e.Source == n.ID && e.Relation == "calls":
			c.Calls = append(c.Calls, e.Target)
		case e.Target == n.ID && e.Relation == "calls":
			c.CalledBy = append(c.CalledBy, e.Source)
		case e.Source == n.ID && e.Relation == "imports":
			c.Imports = append(c.Imports, e.Target)
		case e.Source == n.ID:
			other[e.Relation+" →"] = append(other[e.Relation+" →"], e.Target)
		case e.Target == n.ID:
			other[e.Relation+" ←"] = append(other[e.Relation+" ←"], e.Source)
		}
	}
	for _, s := range [][]string{c.Contains, c.Calls, c.CalledBy, c.Imports} {
		sort.Strings(s)
	}
	for k := range other {
		sort.Strings(other[k])
	}
	if len(other) > 0 {
		c.Other = other
	}
	return c, nil
}

// RenderCard is the human/agent-readable form; --json marshals CardData.
func RenderCard(c *CardData) string {
	var sb strings.Builder
	n := c.Node
	fmt.Fprintf(&sb, "%s  [%s]  %s", n.Label, n.Kind, n.Source)
	if n.Location != "" {
		fmt.Fprintf(&sb, " %s", n.Location)
	}
	sb.WriteString("\n")
	if c.Signature != "" {
		fmt.Fprintf(&sb, "  sig: %s\n", c.Signature)
	}
	if c.Doc != "" {
		for _, line := range strings.Split(c.Doc, "\n") {
			fmt.Fprintf(&sb, "  doc: %s\n", line)
		}
	}
	if c.Parent != "" {
		fmt.Fprintf(&sb, "  in: %s\n", c.Parent)
	}
	writeList := func(title string, ids []string) {
		if len(ids) == 0 {
			return
		}
		fmt.Fprintf(&sb, "  %s (%d):\n", title, len(ids))
		for i, id := range ids {
			if i == 15 {
				fmt.Fprintf(&sb, "    … %d more\n", len(ids)-15)
				break
			}
			fmt.Fprintf(&sb, "    %s\n", id)
		}
	}
	writeList("contains", c.Contains)
	writeList("calls", c.Calls)
	writeList("called by", c.CalledBy)
	writeList("imports", c.Imports)
	rels := make([]string, 0, len(c.Other))
	for r := range c.Other {
		rels = append(rels, r)
	}
	sort.Strings(rels)
	for _, r := range rels {
		writeList(r, c.Other[r])
	}
	return sb.String()
}

// ---- hubs (god nodes) ----

type Hub struct {
	Node schema.Node `json:"node"`
	In   int         `json:"in"`
	Out  int         `json:"out"`
}

// Hubs returns the top-N nodes by total degree (ties by id).
func Hubs(nodes []schema.Node, edges []schema.Edge, top int) []Hub {
	if top <= 0 {
		top = 10
	}
	in, out := map[string]int{}, map[string]int{}
	for _, e := range edges {
		out[e.Source]++
		in[e.Target]++
	}
	hubs := make([]Hub, 0, len(nodes))
	for _, n := range nodes {
		if in[n.ID]+out[n.ID] == 0 {
			continue
		}
		hubs = append(hubs, Hub{Node: n, In: in[n.ID], Out: out[n.ID]})
	}
	sort.Slice(hubs, func(i, j int) bool {
		di, dj := hubs[i].In+hubs[i].Out, hubs[j].In+hubs[j].Out
		if di != dj {
			return di > dj
		}
		return hubs[i].Node.ID < hubs[j].Node.ID
	})
	if len(hubs) > top {
		hubs = hubs[:top]
	}
	return hubs
}
