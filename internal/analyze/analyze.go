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

// Candidate is one near-match offered when fuzzy resolution has no clear
// winner.
type Candidate struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Score int    `json:"score"`
}

// AmbiguousError: fuzzy resolution found several nodes scoring alike —
// answering about one of them would be graphify's silent-nearest-match
// bug (ADR 2026-07-16-verify-verb). The safe default is to refuse with
// ranked candidates; --fuzzy opts into taking the deterministic top one.
type AmbiguousError struct {
	Name       string
	Candidates []Candidate
}

func (e *AmbiguousError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q has no exact match and several near names score alike — refusing to guess; pick one:", e.Name)
	for _, c := range e.Candidates {
		fmt.Fprintf(&b, "\n  %s  [%s]  %s", c.Label, c.Kind, c.ID)
	}
	b.WriteString("\n(use the exact label or id, or --fuzzy to take the top candidate)")
	return b.String()
}

// Resolve finds the node meant by a human name — permissive form kept for
// callers that don't surface resolution provenance.
func Resolve(nodes []schema.Node, name string) (*schema.Node, error) {
	n, _, err := ResolveVia(nodes, name)
	return n, err
}

// ResolveVia finds the node meant by a human name and reports HOW it
// resolved: "exact-id", "exact-label", "last-segment" (qualifier-stripped
// exact label: `ns::Class::Method` / `pkg.Class.method` → `Method`), or
// "fuzzy" (best token overlap). Verbs print via so a fuzzy answer can
// never masquerade as an exact one. A fuzzy TIE returns *AmbiguousError
// with ranked candidates instead of silently picking; a total miss
// suggests the nearest labels — the measured card dead-end pattern
// (chromium forensics) is an agent guessing qualified id formats and
// giving up after bare "no node" errors.
func ResolveVia(nodes []schema.Node, name string) (*schema.Node, string, error) {
	for i := range nodes {
		if nodes[i].ID == name {
			return &nodes[i], "exact-id", nil
		}
	}
	byLabel := func(want string) *schema.Node {
		var hit *schema.Node
		for i := range nodes {
			if strings.EqualFold(nodes[i].Label, want) {
				if hit == nil || nodes[i].ID < hit.ID {
					hit = &nodes[i]
				}
			}
		}
		return hit
	}
	if hit := byLabel(name); hit != nil {
		return hit, "exact-label", nil
	}
	if base := lastSegment(name); base != "" && !strings.EqualFold(base, name) {
		if hit := byLabel(base); hit != nil {
			return hit, "last-segment", nil
		}
	}
	// Fuzzy: token overlap against label+id.
	want := query.Tokenize(name)
	if len(want) == 0 {
		return nil, "", fmt.Errorf("no node named %q", name)
	}
	scores := make([]int, len(nodes))
	best, bestScore := -1, 0
	for i := range nodes {
		have := map[string]bool{}
		for _, t := range query.Tokenize(nodes[i].Label + " " + nodes[i].ID) {
			have[t] = true
		}
		for _, t := range want {
			if have[t] {
				scores[i]++
			}
		}
		if scores[i] > bestScore || (scores[i] == bestScore && scores[i] > 0 && best >= 0 && nodes[i].ID < nodes[best].ID) {
			best, bestScore = i, scores[i]
		}
	}
	// Coverage floor: a fuzzy hit must match at least HALF the asked tokens —
	// one stray common token ("name", "get") resolving a junk ask to a real
	// node is the wrong-symbol hallucination in miniature (found by the
	// grounding probe suite, P1b).
	if best < 0 || bestScore == 0 || bestScore*2 < len(want) {
		return nil, "", fmt.Errorf("no node matching %q%s (`ctx-optimize query %q` searches the store)", name, didYouMean(nodes, name), name)
	}
	// A tie at the top is ambiguity — refuse with candidates rather than
	// answer about a coin-flip winner (distinct labels only: same-label
	// twins resolve deterministically by id and are not a wrong-symbol risk).
	var tied []Candidate
	seen := map[string]bool{}
	for i := range nodes {
		if scores[i] == bestScore && !seen[nodes[i].Label] {
			seen[nodes[i].Label] = true
			tied = append(tied, Candidate{ID: nodes[i].ID, Label: nodes[i].Label, Kind: nodes[i].Kind, Score: scores[i]})
		}
	}
	if len(tied) > 1 {
		sort.Slice(tied, func(i, j int) bool { return tied[i].ID < tied[j].ID })
		if len(tied) > 5 {
			tied = tied[:5]
		}
		return nil, "", &AmbiguousError{Name: name, Candidates: tied}
	}
	return &nodes[best], "fuzzy", nil
}

// lastSegment strips the qualifier prefixes agents invent: path, `::` chain,
// dotted chain. Only ever used as a fallback tier after the full name missed.
func lastSegment(name string) string {
	s := name
	for _, sep := range []string{"/", "::", "."} {
		if i := strings.LastIndex(s, sep); i >= 0 {
			s = s[i+len(sep):]
		}
	}
	return strings.TrimSpace(s)
}

// didYouMean returns up to three nearest labels by trigram similarity —
// the difference between a dead end and a one-step correction. Runs only on
// the total-miss path, so the full scan is acceptable.
func didYouMean(nodes []schema.Node, name string) string {
	want := triSet(strings.ToLower(name))
	base := triSet(strings.ToLower(lastSegment(name)))
	type cand struct {
		label string
		score float64
	}
	seen := map[string]bool{}
	var cands []cand
	for i := range nodes {
		l := nodes[i].Label
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		got := triSet(strings.ToLower(l))
		s := diceSim(want, got)
		if b := diceSim(base, got); b > s {
			s = b
		}
		if s >= 0.3 {
			cands = append(cands, cand{l, s})
		}
	}
	sort.Slice(cands, func(a, b int) bool {
		if cands[a].score != cands[b].score {
			return cands[a].score > cands[b].score
		}
		return cands[a].label < cands[b].label
	})
	if len(cands) == 0 {
		return ""
	}
	if len(cands) > 3 {
		cands = cands[:3]
	}
	labels := make([]string, len(cands))
	for i, c := range cands {
		labels[i] = c.label
	}
	return " — did you mean: " + strings.Join(labels, ", ") + "?"
}

func triSet(s string) map[string]bool {
	t := map[string]bool{}
	for i := 0; i+3 <= len(s); i++ {
		t[s[i:i+3]] = true
	}
	return t
}

func diceSim(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	small, big := a, b
	if len(b) < len(a) {
		small, big = b, a
	}
	inter := 0
	for t := range small {
		if big[t] {
			inter++
		}
	}
	return 2 * float64(inter) / float64(len(a)+len(b))
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
	Node        schema.Node         `json:"node"`
	ResolvedVia string              `json:"resolved_via"` // exact-id | exact-label | last-segment | fuzzy
	Outgoing    map[string][]string `json:"outgoing"`     // relation → target ids
	Incoming    map[string][]string `json:"incoming"`     // relation → source ids
}

func Explain(nodes []schema.Node, edges []schema.Edge, name string) (*Explanation, error) {
	n, via, err := ResolveVia(nodes, name)
	if err != nil {
		return nil, err
	}
	ex := &Explanation{Node: *n, ResolvedVia: via, Outgoing: map[string][]string{}, Incoming: map[string][]string{}}
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
	Node        schema.Node       `json:"node"`
	ResolvedVia string            `json:"resolved_via"` // exact-id | exact-label | last-segment | fuzzy
	Signature string              `json:"signature,omitempty"`
	Doc       string              `json:"doc,omitempty"`
	Body      string              `json:"body,omitempty"`      // first lines of the actual source span, filled by the caller when the file is reachable
	Parent    string              `json:"parent,omitempty"`    // what contains it
	Contains  []string            `json:"contains,omitempty"`  // what it contains
	Calls     []string            `json:"calls,omitempty"`     // outgoing calls
	CalledBy  []string            `json:"called_by,omitempty"` // incoming calls
	Imports   []string            `json:"imports,omitempty"`   // file nodes only
	Other     map[string][]string `json:"other,omitempty"`     // any remaining relations, "rel →|←" keyed
}

func Card(nodes []schema.Node, edges []schema.Edge, name string) (*CardData, error) {
	n, via, err := ResolveVia(nodes, name)
	if err != nil {
		return nil, err
	}
	c := &CardData{Node: *n, ResolvedVia: via, Signature: n.Metadata["signature"], Doc: n.Metadata["doc"]}
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
	if c.Body != "" {
		sb.WriteString("  body:\n")
		for _, line := range strings.Split(c.Body, "\n") {
			fmt.Fprintf(&sb, "    %s\n", line)
		}
	}
	writeList := func(title string, ids []string, cap int) {
		if len(ids) == 0 {
			return
		}
		fmt.Fprintf(&sb, "  %s (%d):\n", title, len(ids))
		for i, id := range ids {
			if cap > 0 && i == cap {
				fmt.Fprintf(&sb, "    … %d more\n", len(ids)-cap)
				break
			}
			fmt.Fprintf(&sb, "    %s\n", id)
		}
	}
	writeList("contains", c.Contains, 15)
	writeList("calls", c.Calls, 15)
	// callers are never truncated — impact answers were measured to go wrong
	// when the tail was hidden (proof S16, D2)
	writeList("called by", c.CalledBy, 0)
	writeList("imports", c.Imports, 15)
	rels := make([]string, 0, len(c.Other))
	for r := range c.Other {
		rels = append(rels, r)
	}
	sort.Strings(rels)
	for _, r := range rels {
		writeList(r, c.Other[r], 15)
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
