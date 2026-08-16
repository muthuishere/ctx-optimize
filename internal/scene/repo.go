package scene

import (
	"sort"
	"strings"
)

// DefaultRepoCards is how many MODULES a repo scene draws. Higher than
// DefaultCards because a card here is a whole module rather than a directory:
// reqsume has 7 and mastra 242, and showing six of seven would hide a quarter
// of a small monorepo for no gain in legibility.
const DefaultRepoCards = 12

// RepoModule is one module of a repo, collected by the caller. The scene
// package never reads a store — it is a pure derivation, exactly as Derive is,
// so the same inputs always produce the same picture.
type RepoModule struct {
	Key     string // store key, e.g. "the-factory/apps/ui" — what a click opens
	Path    string // repo-relative path, e.g. "apps/ui"
	Name    string // display name from the module index
	Summary string // one-line about, from README or the top community
	Nodes   int
	Edges   int

	// Publishes and Declares are `dep:` node ids. They are the whole join:
	// A depends on B when A declares an id B publishes, and BOTH halves are
	// EXTRACTED — written in a manifest, not inferred from a name match.
	Publishes []string
	Declares  []string

	// Provides and Consumes are this module's `port` node ids, split by
	// DIRECTION. Port ids are global, which is what lets them join across
	// separately-gathered module stores — and the direction is what decides
	// which of two very different statements the join supports:
	//
	//   A consumes what B PROVIDES → A calls B. Directed, an arrow.
	//   both CONSUME the same port  → they call the same third party. Not a
	//                                 call between them, and never drawn as one.
	//
	// Collapsing the two into one "shares" was the defect: reqsume's ui and api
	// share twelve ports and every one is consumes/consumes — OpenAI, GitHub,
	// LinkedIn, firebase — so the line a reader took for "ui calls api" was
	// twelve third parties they have in common.
	Provides []string
	Consumes []string

	// Code is how many code edges (imports/calls) the module holds. It is the
	// drill affordance — a module with none has no directory-grain picture
	// worth opening — and it is NOT the count of edges that would be drawn one
	// level in, which is what Card.Inner means at directory grain.
	Code int

	// Vendored marks a module that is a vendored copy of an upstream package
	// rather than a sibling product. It still gets a card; it is flagged so a
	// third_party/ tree cannot read as part of the architecture.
	Vendored bool

	// Unread marks a module whose graph could not be read. It keeps its card
	// and loses its arrows: a module silently dropped reads as "nothing depends
	// on it", which is a different and much worse claim.
	Unread bool
}

// DeriveRepo builds the scene one level ABOVE the directory grain: the cards
// are the modules of one repo, and the arrows are the declarations that join
// them. Clicking a card opens that module's own store at directory grain, so
// this is one more level on top of ADR 21's drill rather than a second
// mechanism.
func DeriveRepo(repo string, mods []RepoModule, opt Options) Scene {
	if opt.Cards <= 0 {
		opt.Cards = DefaultRepoCards
	}
	sc := Scene{
		Module: repo,
		Title:  titleOf(repo),
		Level:  "module",
		Crumbs: []Crumb{{Label: titleOf(repo), Root: ""}},
	}
	for _, m := range mods {
		sc.TotalNodes += m.Nodes
		sc.TotalEdges += m.Edges
	}
	sc.SubsystemsTotal = len(mods)
	if len(mods) == 0 {
		sc.Empty = "no modules recorded for " + repo
		sc.Notes = append(sc.Notes, "this repo has no module index — it is a single store, not a monorepo")
		return sc.finish()
	}

	// ---- 1. who publishes what. A `dep:` id published by two modules is not a
	// join we can trust: it says one of the manifests is wrong, or that two
	// copies of the same package live in the tree. It is dropped from the join
	// and counted, rather than drawing an arrow to an arbitrary one of them.
	publisher := map[string]string{}
	contested := map[string]bool{}
	for _, m := range mods {
		for _, id := range m.Publishes {
			if prev, ok := publisher[id]; ok && prev != m.Key {
				contested[id] = true
				continue
			}
			publisher[id] = m.Key
		}
	}

	// ---- 2. the arrows. A→B once per module pair, weighted by how many of B's
	// packages A declares.
	type pair struct{ from, to string }
	depends := map[pair]int{}
	for _, m := range mods {
		if m.Unread {
			continue
		}
		for _, id := range m.Declares {
			if contested[id] {
				continue
			}
			owner, ok := publisher[id]
			if !ok || owner == m.Key {
				continue // an external package, or the module's own name
			}
			depends[pair{m.Key, owner}]++
		}
	}

	// ---- 3. shared external ports. Undirected, and drawn only where there is
	// no `depends` between the two — a real dependency is the stronger and more
	// specific statement, and both lines between one pair is noise.
	provOf, consOf := map[string]map[string]bool{}, map[string]map[string]bool{}
	setOf := func(ids []string) map[string]bool {
		out := make(map[string]bool, len(ids))
		for _, p := range ids {
			out[p] = true
		}
		return out
	}
	for _, m := range mods {
		provOf[m.Key] = setOf(m.Provides)
		consOf[m.Key] = setOf(m.Consumes)
	}
	calls := map[pair][]string{}  // A consumes a port B provides
	shares := map[pair][]string{} // both consume the same port
	keys := make([]string, 0, len(mods))
	for _, m := range mods {
		keys = append(keys, m.Key)
	}
	sort.Strings(keys)
	related := func(a, b string) bool {
		return depends[pair{a, b}] > 0 || depends[pair{b, a}] > 0
	}
	for i, a := range keys {
		for _, b := range keys[i+1:] {
			if related(a, b) {
				continue
			}
			var ab, ba, both []string
			for p := range consOf[a] {
				if provOf[b][p] {
					ab = append(ab, p)
				} else if consOf[b][p] {
					both = append(both, p)
				}
			}
			for p := range consOf[b] {
				if provOf[a][p] {
					ba = append(ba, p)
				}
			}
			sort.Strings(ab)
			sort.Strings(ba)
			sort.Strings(both)
			if len(ab) > 0 {
				calls[pair{a, b}] = ab
			}
			if len(ba) > 0 {
				calls[pair{b, a}] = ba
			}
			// A directed call between the pair already says the specific thing;
			// what they merely have in common is the weaker statement.
			if len(both) > 0 && len(ab) == 0 && len(ba) == 0 {
				shares[pair{a, b}] = both
			}
		}
	}

	// ---- 4. rank: a module's weight is how much of the repo hangs off it.
	byKey := map[string]RepoModule{}
	in, out := map[string]int{}, map[string]int{}
	for _, m := range mods {
		byKey[m.Key] = m
	}
	for p, w := range depends {
		out[p.from] += w
		in[p.to] += w
	}
	cards := make([]ranked, 0, len(mods))
	aggs := map[string]*agg{}
	for _, m := range mods {
		a := &agg{files: m.Nodes, decls: m.Edges, in: in[m.Key], out: out[m.Key]}
		aggs[m.Key] = a
		cards = append(cards, ranked{dir: m.Key, a: a})
	}
	sort.Slice(cards, func(i, j int) bool {
		wi := cards[i].a.in*2 + cards[i].a.out
		wj := cards[j].a.in*2 + cards[j].a.out
		if wi != wj {
			return wi > wj
		}
		if cards[i].a.files != cards[j].a.files {
			return cards[i].a.files > cards[j].a.files
		}
		return cards[i].dir < cards[j].dir
	})
	chosen := cards
	if len(chosen) > opt.Cards {
		chosen = chosen[:opt.Cards]
	}
	sc.SubsystemsShown = len(chosen)
	shown := map[string]bool{}
	for _, c := range chosen {
		shown[c.dir] = true
	}

	// ---- 5. links, drawn only between cards that are both on screen.
	var links []Link
	drawnDep, drawnShare, drawnCall := 0, 0, 0
	for p, w := range depends {
		if !shown[p.from] || !shown[p.to] {
			continue
		}
		links = append(links, Link{From: p.from, To: p.to, Relation: "depends", Label: "DEPENDS", Weight: w})
		drawnDep++
	}
	for p, ids := range calls {
		if !shown[p.from] || !shown[p.to] {
			continue
		}
		links = append(links, Link{From: p.from, To: p.to, Relation: "calls", Label: "CALLS",
			Weight: len(ids), Detail: nameThem(ids)})
		drawnCall++
	}
	for p, ids := range shares {
		if !shown[p.from] || !shown[p.to] {
			continue
		}
		// The label says what it IS, not a verb the reader has to guess at.
		// "SHARES 12" between a ui and an api reads as "the ui calls the api";
		// it is twelve third parties they both call, and the names prove it.
		links = append(links, Link{From: p.from, To: p.to, Relation: "shares", Label: "BOTH CALL",
			Weight: len(ids), Detail: nameThem(ids)})
		drawnShare++
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Weight != links[j].Weight {
			return links[i].Weight > links[j].Weight
		}
		if links[i].From != links[j].From {
			return links[i].From < links[j].From
		}
		return links[i].To < links[j].To
	})
	sc.Links = links
	sc.LiftedShown = len(links)
	sc.LiftedTotal = len(depends) + len(shares) + len(calls)

	// ---- 6. layering. Only `depends` is a direction; `shares` is symmetric and
	// laying out by it would invent a hierarchy that the evidence does not have.
	var dirLinks []Link
	for _, l := range links {
		if l.Relation == "depends" {
			dirLinks = append(dirLinks, l)
		}
	}
	layer := layering(chosen, dirLinks)
	row := rowOrder(chosen, dirLinks, layer)

	hub, bestIn := "", 0
	for _, c := range chosen {
		if c.a.in > bestIn {
			hub, bestIn = c.dir, c.a.in
		}
	}
	for _, c := range chosen {
		m := byKey[c.dir]
		label := m.Name
		if label == "" {
			label = lastSeg(m.Path)
		}
		detail := m.Summary
		if detail == "" {
			detail = comma(m.Nodes) + " nodes · " + comma(m.Edges) + " edges"
		}
		if m.Vendored {
			detail = "vendored · " + detail
		}
		dir := m.Path
		if dir == "" {
			dir = m.Key
		}
		// Children/Inner are the drill affordance the client already honours: a
		// module with code in it opens at directory grain, one with none does
		// not offer a door to an empty screen.
		children, inner := 0, 0
		if !m.Unread && m.Code > 0 {
			children, inner = 1, m.Code
		}
		sc.Cards = append(sc.Cards, Card{
			ID: c.dir, Label: label, Dir: dir,
			Files: m.Nodes, Decls: m.Edges,
			In: c.a.in, Out: c.a.out,
			Layer: layer[c.dir], Row: row[c.dir],
			Detail:   detail,
			Hub:      c.dir == hub && bestIn > 0,
			Children: children,
			Inner:    inner,
			Top:      "",
		})
	}
	sort.Slice(sc.Cards, func(i, j int) bool {
		if sc.Cards[i].Layer != sc.Cards[j].Layer {
			return sc.Cards[i].Layer < sc.Cards[j].Layer
		}
		return sc.Cards[i].Row < sc.Cards[j].Row
	})

	// ---- 7. the way in for modules that did not make the cut.
	for _, c := range cards {
		if shown[c.dir] {
			continue
		}
		m := byKey[c.dir]
		label := m.Name
		if label == "" {
			label = lastSeg(m.Path)
		}
		sc.Inside = append(sc.Inside, Crumb{Label: label, Root: c.dir})
	}

	// A module grain with one card says nothing a reader could not see by
	// opening that module — it is the "chooser wearing a diagram" this ADR set
	// as its own kill criterion. Rather than draw it, hand back where the
	// content actually is.
	if len(sc.Cards) == 1 {
		sc.Redirect = sc.Cards[0].ID
	}

	// ---- 8. stats and the honesty notes.
	sc.Stats = []Stat{
		{Label: plural("module", "modules", len(mods)), Text: itoa(len(mods))},
		{Label: "nodes", Text: comma(sc.TotalNodes)},
		{Label: "edges", Text: comma(sc.TotalEdges)},
	}
	sc.Chips = append(sc.Chips, itoa(len(depends))+" declared "+
		plural("dependency", "dependencies", len(depends)))
	if len(calls) > 0 {
		sc.Chips = append(sc.Chips, itoa(len(calls))+" module-to-module "+
			plural("call", "calls", len(calls)))
	}
	if len(shares) > 0 {
		sc.Chips = append(sc.Chips, itoa(len(shares))+" "+
			plural("pair", "pairs", len(shares))+" calling the same third parties")
	}
	sc.Notes = append(sc.Notes,
		"a card here is a MODULE — clicking one opens that module's own store at directory grain")
	if sc.SubsystemsShown < sc.SubsystemsTotal {
		sc.Notes = append(sc.Notes,
			"top "+itoa(sc.SubsystemsShown)+" of "+itoa(sc.SubsystemsTotal)+
				" modules by how much of the repo hangs off them — this is a SAMPLE, not the whole repo")
	}
	sc.Notes = append(sc.Notes,
		itoa(drawnDep)+" of "+itoa(len(depends))+
			" `depends` arrows drawn — a module declaring a package a sibling publishes, EXTRACTED from both manifests")
	if len(calls) > 0 {
		sc.Notes = append(sc.Notes,
			itoa(drawnCall)+" of "+itoa(len(calls))+
				" `calls` arrows drawn — one module CONSUMES a port another PROVIDES, which is a call between them")
	}
	if len(shares) > 0 {
		sc.Notes = append(sc.Notes,
			itoa(drawnShare)+" of "+itoa(len(shares))+
				" dashed links drawn — both modules CONSUME the same external port, so they call the same third party. "+
				"That is not a call between them, which is why it has no arrowhead and the names are printed")
	}
	if hub != "" && bestIn > 0 {
		sc.Notes = append(sc.Notes, "hub = the module the most of this repo depends on ("+itoa(bestIn)+" declarations in)")
	}
	if n := len(contested); n > 0 {
		sc.Notes = append(sc.Notes,
			itoa(n)+" package names are published by more than one module — those joins are dropped, not guessed")
	}
	unread, vendored, silent := 0, 0, 0
	for _, m := range mods {
		if m.Unread {
			unread++
		}
		if m.Vendored {
			vendored++
		}
		if len(m.Publishes) == 0 && !m.Unread {
			silent++
		}
	}
	if unread > 0 {
		sc.Notes = append(sc.Notes,
			itoa(unread)+" modules could not be read — they keep their card and lose their arrows, never dropped")
	}
	if vendored > 0 {
		sc.Notes = append(sc.Notes,
			itoa(vendored)+" modules are vendored copies of upstream packages, not products of this repo")
	}
	if silent > 0 {
		sc.Notes = append(sc.Notes,
			itoa(silent)+" modules declare no package name of their own, so nothing can point AT them — "+
				"an arrow that is missing here may be a missing manifest, not a missing dependency")
	}
	if len(depends) == 0 {
		sc.Notes = append(sc.Notes,
			"no module in this repo declares a package another one publishes — either they are independent, "+
				"or they are joined by something this store does not record yet (an HTTP call, a spawned process)")
	}
	sc.Questions = repoQuestions(sc, byKey)
	return sc.finish()
}

// plural keeps a count and its noun in agreement. "1 modules" in the header
// strip is the kind of detail that makes a reader wonder what else is sloppy —
// and so is "0 declared dependencys", which is what appending an "s" produced.
// The plural is given, never guessed.
func plural(one, many string, n int) string {
	if n == 1 {
		return one
	}
	return many
}

// nameThem turns port ids into the names a reader recognises. A count is not
// an explanation: three names and a remainder is what turns "12" into "OpenAI,
// GitHub and LinkedIn, plus nine more".
func nameThem(ids []string) string {
	const show = 3
	out := make([]string, 0, show)
	for _, id := range ids {
		n := id
		if i := strings.LastIndex(n, ":>"); i >= 0 {
			n = n[i+2:]
		}
		if len(out) < show {
			out = append(out, n)
		}
	}
	s := strings.Join(out, ", ")
	if rest := len(ids) - len(out); rest > 0 {
		s += " +" + itoa(rest) + " more"
	}
	return s
}

// lastSeg is the display name for a path with no recorded module name.
func lastSeg(p string) string {
	p = strings.Trim(p, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	if p == "" {
		return "(root)"
	}
	return p
}

// repoQuestions asks about the modules actually on screen, with commands this
// binary has. Same rule as questionsFor: a suggestion that does not run, or
// that names something this repo does not contain, is worse than none.
func repoQuestions(sc Scene, byKey map[string]RepoModule) []Question {
	var qs []Question
	var hub string
	for _, c := range sc.Cards {
		if c.Hub {
			hub = c.ID
		}
	}
	// `--to <dep id>` is the precise question and it runs from anywhere: the
	// verbs federate across every module at the store root, so no command here
	// needs a store key the reader does not have.
	if hub != "" {
		m := byKey[hub]
		if len(m.Publishes) > 0 {
			qs = append(qs, Question{
				Text:    "Who in " + sc.Title + " depends on " + lastSeg(m.Path) + "?",
				Command: "ctx-optimize edges --relation declares --to " + m.Publishes[0],
			})
		}
	}
	for _, l := range sc.Links {
		if l.Relation != "depends" {
			continue
		}
		to := byKey[l.To]
		if len(to.Publishes) == 0 {
			continue
		}
		qs = append(qs, Question{
			Text:    "What exactly does " + lastSeg(byKey[l.From].Path) + " use from " + lastSeg(to.Path) + "?",
			Command: "ctx-optimize edges --relation declares --to " + to.Publishes[0],
		})
		break
	}
	if len(sc.Links) > 0 {
		qs = append(qs, Question{
			Text:    "Which modules of " + sc.Title + " publish a package of their own?",
			Command: "ctx-optimize edges --relation publishes",
		})
	}
	if len(qs) > 3 {
		qs = qs[:3]
	}
	return qs
}
