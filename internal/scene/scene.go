// Package scene derives a DRAWABLE architecture scene from a store's graph.
//
// It exists because a picture of a codebase is only worth drawing if the
// picture is DERIVED. ADR 2026-08-13-serve-world was killed by its own
// criterion: it laid ports around a wall, and "position on the wall carried no
// information — it was the sort order", with no edges between anything. A map
// with no routes is a list in a costume.
//
// So this package produces two things the killed view had neither of:
//
//  1. REAL EDGES. Every code edge (`imports`, `calls`) is LIFTED from
//     file/decl level to the directory that owns each endpoint, summed, and
//     kept with its relation and its site count. `consumes`/`provides` edges
//     are lifted the same way onto the transport groups they reach. Nothing is
//     synthesised: an arrow on screen is N real edges in the store.
//
//  2. POSITION THAT MEANS SOMETHING. A card's Layer is its longest-path depth
//     in the lifted subsystem DAG, so callers stand left of what they call and
//     the most depended-upon subsystem falls out on the right as the Hub. Read
//     the scene left to right and you are reading the direction dependencies
//     actually point. That is topology, not sort order — re-rank the cards and
//     the layout changes shape, not just order.
//
// The unit is the DIRECTORY, because in every language the extractor handles a
// directory is the package/module boundary the author chose. Ports (the outer
// world) are deliberately NOT bucketed by directory: they have no path, they
// are the edges of the system, and they get their own groups.
//
// Nothing here reads a file, dials anything, or touches a secret: ports carry
// env-var NAMES, and `sensitive` is a flag on the name. There is no code path
// in this package that could emit a value.
package scene

import (
	"path"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// DefaultCards is how many subsystems the scene draws besides the hub. Six is
// the reference illustration's count and about what stays legible at 1600x1000
// once every card carries a title, a detail line and its own edges.
const DefaultCards = 6

// DefaultDoors is how many port labels one transport group samples.
const DefaultDoors = 6

// Card is one subsystem — a directory — drawn as a numbered box.
type Card struct {
	ID     string `json:"id"`     // the directory path, the stable key
	Label  string `json:"label"`  // last path segment (the display name)
	Dir    string `json:"dir"`    // full directory, shown as the card's subtitle
	Files  int    `json:"files"`  // file nodes under it
	Decls  int    `json:"decls"`  // declaration nodes under it
	In     int    `json:"in"`     // lifted edges arriving (how depended-upon)
	Out    int    `json:"out"`    // lifted edges leaving (how dependent)
	Layer  int    `json:"layer"`  // longest-path depth in the lifted DAG
	Row    int    `json:"row"`    // slot within the layer
	Detail string `json:"detail"` // top declarations by degree, " · " joined
	Glyph  string `json:"glyph"`  // derived from which transports it touches
	Hub    bool   `json:"hub"`    // the most depended-upon subsystem
}

// Link is a lifted edge between two cards, or between a card and a transport
// group ("world:network.http"). Weight is how many real store edges it stands
// for; Label is what the renderer prints on the curve.
type Link struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
	Label    string `json:"label"`
	Weight   int    `json:"weight"`
}

// Door is ONE port, named. Never a value — Label is the env-var NAME, the
// route path, or the executable name, exactly as the boundary lane recorded it.
type Door struct {
	Label     string `json:"label"`
	Direction string `json:"direction"`
	Sensitive bool   `json:"sensitive"`
	Dynamic   bool   `json:"dynamic"`
}

// World is one transport group: the outer edge of the system.
type World struct {
	ID        string `json:"id"`
	Transport string `json:"transport"`
	Total     int    `json:"total"`
	Provides  int    `json:"provides"`
	Consumes  int    `json:"consumes"`
	Sensitive int    `json:"sensitive"`
	Sample    []Door `json:"sample"`
	Truncated bool   `json:"truncated"`
}

// Stat is one segment of the terminal-style header strip. The field is `text`,
// not `value`: the scene payload must never contain a key named `value`, and
// TestDeriveWorldCarriesNamesNeverValues enforces that as a blanket rule rather
// than a per-field judgement call.
type Stat struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// Scene is the whole derived picture.
type Scene struct {
	Module          string   `json:"module"`
	Title           string   `json:"title"`
	TotalNodes      int      `json:"total_nodes"`
	TotalEdges      int      `json:"total_edges"`
	SubsystemsTotal int      `json:"subsystems_total"`
	SubsystemsShown int      `json:"subsystems_shown"`
	LiftedTotal     int      `json:"lifted_total"` // lifted edges in the whole repo
	LiftedShown     int      `json:"lifted_shown"` // lifted edges drawn
	Cards           []Card   `json:"cards"`
	Links           []Link   `json:"links"`
	World           []World  `json:"world"`
	Stats           []Stat   `json:"stats"`
	Chips           []string `json:"chips"`
	Notes           []string `json:"notes"` // honesty lines, printed on screen
	Empty           string   `json:"empty,omitempty"`
}

// Options tunes the derivation. Zero values mean the defaults.
type Options struct {
	Cards int
	Doors int
	// IncludeTests keeps test/spec/fixture directories in the ranking. Off by
	// default because on a real service the test tree out-ranks half the
	// architecture (measured on reqsume/apps/api: test/integration/payments and
	// test/data both land in the top ten). Whichever way this goes, the scene
	// says so in Notes.
	IncludeTests bool
}

// declKinds are the node kinds that count as "a declaration in a subsystem".
var declKinds = map[string]bool{
	"function": true, "method": true, "type": true, "class": true, "interface": true,
}

// liftedRelations are the code relations worth lifting to directory level.
// `contains` is excluded: it is always within one file, so every lifted
// `contains` edge is a self-loop and carries no cross-subsystem information.
var liftedRelations = map[string]string{
	"imports": "IMPORTS",
	"calls":   "CALLS",
}

var testSegments = map[string]bool{
	"test": true, "tests": true, "spec": true, "specs": true,
	"__tests__": true, "testdata": true, "e2e": true, "fixtures": true,
	"_test": true, "test_data": true,
}

// isTestPath reports whether any path segment marks the dir as test material.
func isTestPath(dir string) bool {
	for _, seg := range strings.Split(dir, "/") {
		if testSegments[strings.ToLower(seg)] {
			return true
		}
	}
	return false
}

// subsystemOf maps a node to the directory that owns it, or "" when the node
// has no place in the tree (module:// imports, port:// doors, dep: entries).
func subsystemOf(n schema.Node) string {
	src := n.Source
	if src == "" || strings.Contains(src, "://") || strings.HasPrefix(src, "dep:") {
		return ""
	}
	if n.Kind == "port" {
		return ""
	}
	d := path.Dir(strings.ReplaceAll(src, "\\", "/"))
	if d == "." || d == "/" {
		return "(root)"
	}
	return d
}

type agg struct {
	files, decls int
	in, out      int
}

// ranked is one subsystem with its aggregate, in ranking order.
type ranked struct {
	dir string
	a   *agg
}

// Derive builds the scene. It is a pure function of the graph: same nodes and
// edges in, byte-identical scene out.
func Derive(module string, nodes []schema.Node, edges []schema.Edge, opt Options) Scene {
	if opt.Cards <= 0 {
		opt.Cards = DefaultCards
	}
	if opt.Doors <= 0 {
		opt.Doors = DefaultDoors
	}
	sc := Scene{
		Module:     module,
		Title:      titleOf(module),
		TotalNodes: len(nodes),
		TotalEdges: len(edges),
	}

	// ---- 1. every node to its subsystem, and node degree for detail lines.
	owner := make(map[string]string, len(nodes))
	byID := make(map[string]schema.Node, len(nodes))
	subs := map[string]*agg{}
	ports := map[string]schema.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
		if n.Kind == "port" {
			ports[n.ID] = n
			continue
		}
		d := subsystemOf(n)
		if d == "" {
			continue
		}
		owner[n.ID] = d
		a := subs[d]
		if a == nil {
			a = &agg{}
			subs[d] = a
		}
		switch {
		case n.Kind == "file":
			a.files++
		case declKinds[n.Kind]:
			a.decls++
		}
	}
	sc.SubsystemsTotal = len(subs)

	deg := make(map[string]int, len(nodes))
	for _, e := range edges {
		deg[e.Source]++
		deg[e.Target]++
	}

	// ---- 2. LIFT the code edges. AMBIGUOUS is dropped: the repo's standing
	// doctrine is that traversal verbs answer with facts, and an arrow drawn
	// from a call the store refused to attribute would be a drawn guess.
	//
	// Excluded directories are dropped from the LIFT, not just from the card
	// pool: if the test tree is not drawn, its calls must not inflate a card's
	// in-count either, or the number printed on a card describes arrows the
	// reader cannot see.
	drop := func(d string) bool { return !opt.IncludeTests && isTestPath(d) }
	type lk struct{ from, to, rel string }
	lifted := map[lk]int{}
	for _, e := range edges {
		rel, ok := liftedRelations[e.Relation]
		if !ok || e.Confidence == schema.Ambiguous {
			continue
		}
		a, b := owner[e.Source], owner[e.Target]
		if a == "" || b == "" || a == b || drop(a) || drop(b) {
			continue
		}
		lifted[lk{a, b, rel}] += 1
	}
	for k, w := range lifted {
		subs[k.from].out += w
		subs[k.to].in += w
	}

	// ---- 3. rank. Score = lifted degree; test trees demoted (and said so).
	var pool []ranked
	skippedTests := 0
	for d, a := range subs {
		if drop(d) {
			skippedTests++
			continue
		}
		if a.in+a.out == 0 {
			continue // no cross-subsystem edge: nothing to draw it with
		}
		pool = append(pool, ranked{d, a})
	}
	sort.Slice(pool, func(i, j int) bool {
		di, dj := pool[i].a.in+pool[i].a.out, pool[j].a.in+pool[j].a.out
		if di != dj {
			return di > dj
		}
		return pool[i].dir < pool[j].dir
	})
	if len(pool) == 0 {
		sc.Empty = "no subsystem in this store has a cross-directory `imports` or `calls` edge — " +
			"there is no flow to draw. Try the graph viewer."
		return sc
	}

	// ---- 4. the HUB: the most depended-upon subsystem. Highest lifted
	// in-degree among candidates that are mostly depended-upon rather than
	// mostly depending (in-share >= 0.6). This is the reference's centre — the
	// thing every card points at — and it is chosen by the graph, not by us.
	// Score = in-degree WEIGHTED by in-share, so a true sink (everything calls
	// it, it calls nothing) beats a busy middle layer that merely has more
	// arrows through it. Plain in-degree picks the mid-tier; this picks the
	// floor of the dependency stack, which is what "most depended on" means.
	hub := ""
	bestIn := 0
	bestScore := 0.0
	for _, r := range pool {
		tot := r.a.in + r.a.out
		if tot == 0 {
			continue
		}
		share := float64(r.a.in) / float64(tot)
		if share < 0.6 {
			continue
		}
		if score := float64(r.a.in) * share; score > bestScore {
			bestScore, bestIn, hub = score, r.a.in, r.dir
		}
	}

	// ---- 5. pick the cards: top N by lifted degree, hub excluded (it gets
	// its own slot) and appended last.
	var chosen []ranked
	for _, r := range pool {
		if r.dir == hub {
			continue
		}
		if len(chosen) >= opt.Cards {
			break
		}
		chosen = append(chosen, r)
	}
	if hub != "" {
		for _, r := range pool {
			if r.dir == hub {
				chosen = append(chosen, r)
				break
			}
		}
	}
	shown := map[string]bool{}
	for _, r := range chosen {
		shown[r.dir] = true
	}
	sc.SubsystemsShown = len(chosen)

	// ---- 6. the drawn links: lifted edges with BOTH ends on screen.
	var links []Link
	for k, w := range lifted {
		if !shown[k.from] || !shown[k.to] {
			continue
		}
		links = append(links, Link{From: k.from, To: k.to, Relation: strings.ToLower(k.rel), Label: k.rel, Weight: w})
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Weight != links[j].Weight {
			return links[i].Weight > links[j].Weight
		}
		if links[i].From != links[j].From {
			return links[i].From < links[j].From
		}
		if links[i].To != links[j].To {
			return links[i].To < links[j].To
		}
		return links[i].Relation < links[j].Relation
	})

	// ---- 7. LAYER: longest-path depth over the acyclic part of the drawn
	// graph. Cycles are broken by dropping the lighter edge of a back pair,
	// deterministically (heaviest edges are laid first). Layer is the whole
	// point: x on screen is dependency depth, so an arrow points the way the
	// dependency points, and a card's column is a fact about the graph.
	layer := layering(chosen, links)
	// hub always sits at the far right, past every card, matching what it is.
	maxLayer := 0
	for _, l := range layer {
		if l > maxLayer {
			maxLayer = l
		}
	}
	if hub != "" {
		layer[hub] = maxLayer + 1
	}
	// Compact: longest-path layering leaves gaps (nothing sits at depth 3 while
	// something sits at 4). Gaps waste half the canvas, and the ORDER of the
	// columns is the information, not their absolute index.
	used := map[int]bool{}
	for _, l := range layer {
		used[l] = true
	}
	idx := make([]int, 0, len(used))
	for l := range used {
		idx = append(idx, l)
	}
	sort.Ints(idx)
	compact := map[int]int{}
	for i, l := range idx {
		compact[l] = i
	}
	for k, l := range layer {
		layer[k] = compact[l]
	}

	// ---- 8. build the cards.
	// top declarations per shown subsystem, for the detail line.
	top := map[string][]schema.Node{}
	for _, n := range nodes {
		d := owner[n.ID]
		if d == "" || !shown[d] || !declKinds[n.Kind] {
			continue
		}
		top[d] = append(top[d], n)
	}
	for d := range top {
		list := top[d]
		sort.Slice(list, func(i, j int) bool {
			if deg[list[i].ID] != deg[list[j].ID] {
				return deg[list[i].ID] > deg[list[j].ID]
			}
			return list[i].ID < list[j].ID
		})
		top[d] = list
	}

	// which transports each shown subsystem touches (drives the glyph).
	touch := map[string]map[string]bool{}
	for _, e := range edges {
		if e.Relation != "consumes" && e.Relation != "provides" {
			continue
		}
		d := owner[e.Source]
		p, ok := ports[e.Target]
		if d == "" || !ok {
			continue
		}
		if touch[d] == nil {
			touch[d] = map[string]bool{}
		}
		touch[d][e.Relation+":"+p.Metadata["transport"]] = true
	}

	row := rowOrder(chosen, links, layer)
	for _, r := range chosen {
		sc.Cards = append(sc.Cards, Card{
			ID: r.dir, Label: path.Base(r.dir), Dir: r.dir,
			Files: r.a.files, Decls: r.a.decls,
			In: r.a.in, Out: r.a.out,
			Layer: layer[r.dir], Row: row[r.dir],
			Detail: detailOf(top[r.dir]),
			Glyph:  glyphOf(touch[r.dir]),
			Hub:    r.dir == hub,
		})
	}
	sort.Slice(sc.Cards, func(i, j int) bool {
		if sc.Cards[i].Layer != sc.Cards[j].Layer {
			return sc.Cards[i].Layer < sc.Cards[j].Layer
		}
		return sc.Cards[i].Row < sc.Cards[j].Row
	})

	// ---- 9. THE OUTER WORLD: transport groups, plus real card→world links.
	sc.World = worldGroups(ports, opt.Doors)
	worldSeen := map[string]bool{}
	for _, w := range sc.World {
		worldSeen[w.Transport] = true
	}
	pw := map[lk]int{}
	pwAll := map[lk]int{}
	for _, e := range edges {
		if e.Relation != "consumes" && e.Relation != "provides" {
			continue
		}
		d := owner[e.Source]
		p, ok := ports[e.Target]
		if !ok || d == "" || drop(d) {
			continue
		}
		t := p.Metadata["transport"]
		if !worldSeen[t] {
			continue
		}
		pwAll[lk{d, "world:" + t, e.Relation}]++
		if !shown[d] {
			continue
		}
		if e.Relation == "provides" {
			pw[lk{d, "world:" + t, "PROVIDES"}]++
		} else {
			pw[lk{d, "world:" + t, "CONSUMES"}]++
		}
	}
	var wlinks []Link
	for k, w := range pw {
		wlinks = append(wlinks, Link{From: k.from, To: k.to, Relation: strings.ToLower(k.rel), Label: k.rel, Weight: w})
	}
	sort.Slice(wlinks, func(i, j int) bool {
		if wlinks[i].Weight != wlinks[j].Weight {
			return wlinks[i].Weight > wlinks[j].Weight
		}
		if wlinks[i].From != wlinks[j].From {
			return wlinks[i].From < wlinks[j].From
		}
		return wlinks[i].To < wlinks[j].To
	})
	sc.Links = append(links, wlinks...)
	sc.LiftedShown = len(sc.Links)
	sc.LiftedTotal = len(lifted) + len(pwAll)

	// ---- 10. header stats + chips + the honesty notes.
	sc.Stats = []Stat{
		{Label: "subsystems", Text: itoa(sc.SubsystemsTotal)},
		{Label: "nodes", Text: comma(len(nodes))},
		{Label: "edges", Text: comma(len(edges))},
	}
	if len(ports) > 0 {
		sc.Stats = append(sc.Stats, Stat{Label: "ports", Text: itoa(len(ports))})
	}
	for _, w := range sc.World {
		sc.Chips = append(sc.Chips, itoa(w.Total)+" "+w.Transport)
	}
	sens := 0
	for _, p := range ports {
		if p.Metadata["sensitive"] == "true" {
			sens++
		}
	}
	if sens > 0 {
		sc.Chips = append(sc.Chips, itoa(sens)+" marked sensitive (names only)")
	}

	sc.Notes = append(sc.Notes,
		"top "+itoa(sc.SubsystemsShown)+" of "+itoa(sc.SubsystemsTotal)+
			" directories by cross-directory edge weight — this is a SAMPLE, not the whole graph")
	sc.Notes = append(sc.Notes,
		itoa(sc.LiftedShown)+" of "+itoa(sc.LiftedTotal)+" lifted relations drawn; every arrow is real store edges, summed")
	if hub != "" {
		sc.Notes = append(sc.Notes, "hub = most depended-upon directory ("+itoa(bestIn)+" edges in, "+
			itoa(subs[hub].out)+" out)")
	}
	sc.Notes = append(sc.Notes, "x = longest-path depth in the lifted dependency DAG; AMBIGUOUS edges excluded")
	if skippedTests > 0 {
		sc.Notes = append(sc.Notes, itoa(skippedTests)+" test/fixture directories excluded from the ranking")
	}
	return sc
}

// layering assigns each card its longest-path depth. Edges are considered
// heaviest-first and any edge that would close a cycle is skipped, so the
// result is deterministic and every kept edge points forward.
func layering(cards []ranked, links []Link) map[string]int {
	in := map[string][]string{}
	present := map[string]bool{}
	for _, c := range cards {
		present[c.dir] = true
	}
	kept := map[string][]string{} // from -> tos
	reach := func(from, to string) bool {
		// can we already get from `to` back to `from`? then from->to closes a cycle.
		seen := map[string]bool{to: true}
		stack := []string{to}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if cur == from {
				return true
			}
			for _, nx := range kept[cur] {
				if !seen[nx] {
					seen[nx] = true
					stack = append(stack, nx)
				}
			}
		}
		return false
	}
	for _, l := range links {
		if !present[l.From] || !present[l.To] || l.From == l.To {
			continue
		}
		if reach(l.From, l.To) {
			continue // back edge: drop it for layout only, it is still drawn
		}
		kept[l.From] = append(kept[l.From], l.To)
		in[l.To] = append(in[l.To], l.From)
	}
	layer := map[string]int{}
	var depth func(string, map[string]bool) int
	depth = func(id string, path map[string]bool) int {
		if v, ok := layer[id]; ok {
			return v
		}
		if path[id] {
			return 0
		}
		path[id] = true
		best := 0
		for _, p := range in[id] {
			if d := depth(p, path) + 1; d > best {
				best = d
			}
		}
		delete(path, id)
		layer[id] = best
		return best
	}
	// deterministic visit order
	order := make([]string, 0, len(cards))
	for _, c := range cards {
		order = append(order, c.dir)
	}
	sort.Strings(order)
	for _, id := range order {
		depth(id, map[string]bool{})
	}
	return layer
}

// rowOrder assigns each card its slot within its layer by BARYCENTRE — the
// mean row of its neighbours in the layer before, then a backward sweep on the
// layer after. This is the standard Sugiyama crossing-reduction pass, and it
// is why the picture reads: without it a card's vertical slot is its rank, the
// arrows cross for no reason, and the reader learns nothing from height.
// Ties fall back to the ranking order, so the result is deterministic.
func rowOrder(cards []ranked, links []Link, layer map[string]int) map[string]int {
	rank := map[string]int{}
	byLayer := map[int][]string{}
	for i, c := range cards {
		rank[c.dir] = i
		l := layer[c.dir]
		byLayer[l] = append(byLayer[l], c.dir)
	}
	maxL := 0
	for l := range byLayer {
		if l > maxL {
			maxL = l
		}
	}
	pos := map[string]float64{}
	for l := 0; l <= maxL; l++ {
		list := byLayer[l]
		sort.Slice(list, func(i, j int) bool { return rank[list[i]] < rank[list[j]] })
		for i, id := range list {
			pos[id] = float64(i)
		}
		byLayer[l] = list
	}
	// neighbours by layer distance
	prev := map[string][]string{}
	next := map[string][]string{}
	for _, lk := range links {
		if _, ok := layer[lk.From]; !ok {
			continue
		}
		if _, ok := layer[lk.To]; !ok {
			continue
		}
		if layer[lk.From] < layer[lk.To] {
			prev[lk.To] = append(prev[lk.To], lk.From)
			next[lk.From] = append(next[lk.From], lk.To)
		} else if layer[lk.From] > layer[lk.To] {
			prev[lk.From] = append(prev[lk.From], lk.To)
			next[lk.To] = append(next[lk.To], lk.From)
		}
	}
	bary := func(id string, side map[string][]string) float64 {
		ns := side[id]
		if len(ns) == 0 {
			return pos[id]
		}
		s := 0.0
		for _, n := range ns {
			s += pos[n]
		}
		return s / float64(len(ns))
	}
	sweep := func(side map[string][]string, order []int) {
		for _, l := range order {
			list := byLayer[l]
			b := make(map[string]float64, len(list))
			for _, id := range list {
				b[id] = bary(id, side)
			}
			sort.SliceStable(list, func(i, j int) bool {
				if b[list[i]] != b[list[j]] {
					return b[list[i]] < b[list[j]]
				}
				return rank[list[i]] < rank[list[j]]
			})
			for i, id := range list {
				pos[id] = float64(i)
			}
		}
	}
	fwd := make([]int, 0, maxL+1)
	for l := 1; l <= maxL; l++ {
		fwd = append(fwd, l)
	}
	bwd := make([]int, 0, maxL+1)
	for l := maxL - 1; l >= 0; l-- {
		bwd = append(bwd, l)
	}
	for i := 0; i < 3; i++ {
		sweep(prev, fwd)
		sweep(next, bwd)
	}
	out := map[string]int{}
	for _, list := range byLayer {
		for i, id := range list {
			out[id] = i
		}
	}
	return out
}

func worldGroups(ports map[string]schema.Node, sample int) []World {
	byT := map[string][]schema.Node{}
	for _, p := range ports {
		byT[p.Metadata["transport"]] = append(byT[p.Metadata["transport"]], p)
	}
	var out []World
	for t, list := range byT {
		// Sample order: sensitive names first (they are the ones to see), then
		// statically-resolved names, and only then the `resolved: dynamic`
		// placeholders (`${key}`) — a sample full of ${…} teaches nothing.
		sort.Slice(list, func(i, j int) bool {
			si := list[i].Metadata["sensitive"] == "true"
			sj := list[j].Metadata["sensitive"] == "true"
			if si != sj {
				return si
			}
			di := list[i].Metadata["resolved"] == "dynamic"
			dj := list[j].Metadata["resolved"] == "dynamic"
			if di != dj {
				return dj
			}
			return list[i].Label < list[j].Label
		})
		w := World{ID: "world:" + t, Transport: t, Total: len(list)}
		for _, p := range list {
			if p.Metadata["direction"] == "provides" {
				w.Provides++
			} else {
				w.Consumes++
			}
			if p.Metadata["sensitive"] == "true" {
				w.Sensitive++
			}
			if len(w.Sample) < sample {
				w.Sample = append(w.Sample, Door{
					Label:     p.Label,
					Direction: p.Metadata["direction"],
					Sensitive: p.Metadata["sensitive"] == "true",
					Dynamic:   p.Metadata["resolved"] == "dynamic",
				})
			}
		}
		w.Truncated = len(w.Sample) < w.Total
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return out[i].Transport < out[j].Transport
	})
	return out
}

func detailOf(list []schema.Node) string {
	var parts []string
	for _, n := range list {
		if len(parts) >= 3 {
			break
		}
		l := n.Label
		if i := strings.LastIndexByte(l, '.'); i > 0 && i < len(l)-1 {
			l = l[i+1:]
		}
		if l == "" {
			continue
		}
		parts = append(parts, l)
	}
	return strings.Join(parts, " · ")
}

// glyphOf picks a monospace mark from what the subsystem actually does at the
// boundary: serves HTTP, reads config, shells out, calls out. No transport
// edges at all → the plain "internal" diamond.
func glyphOf(t map[string]bool) string {
	switch {
	case t["provides:network.http"] || t["provides:network.ws"]:
		return "⇄"
	case t["consumes:process.exec"]:
		return "›_"
	case t["consumes:network.http"] || t["consumes:network.ws"]:
		return "↗"
	case t["consumes:config.env"]:
		return "⚙"
	case t["consumes:storage.local"] || t["provides:storage.local"]:
		return "⇪"
	default:
		return "◇"
	}
}

func titleOf(module string) string {
	if module == "" {
		return "store"
	}
	return path.Base(module)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func comma(n int) string {
	s := itoa(n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
