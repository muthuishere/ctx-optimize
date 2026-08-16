package scene

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// fixture is a small, HAND-WRITTEN repo shape. Every expectation in this file
// is written from THIS source, never captured from Derive's output — the repo
// has a standing lesson that a gate recording what it measures is not a gate.
//
// The shape, on purpose, is a four-deep chain with a fan-in sink:
//
//	handlers ──calls──▶ services ──calls──▶ repo ──calls──▶ db
//	    │                                     ▲              ▲
//	    └──────────────calls──────────────────┘              │
//	util ◀──calls── services                                 │
//	handlers ──provides──▶ port /a, /b        util ──calls────┘
//
// so `db` must be the hub, `handlers` must sit left of `services`, and the
// layering must be a fact about the arrows rather than the ranking order.
func fixture() ([]schema.Node, []schema.Edge) {
	dirs := map[string][]string{
		"src/handlers":   {"Serve", "Route", "Auth"},
		"src/services":   {"Charge", "Refund"},
		"src/repository": {"Get", "Put"},
		"src/db":         {"Open"},
		"src/util":       {"Clamp"},
		"test/api":       {"TestServe"},
	}
	var nodes []schema.Node
	for d, decls := range dirs {
		nodes = append(nodes, schema.Node{ID: d + "/f.go", Label: "f.go", Kind: "file", Source: d + "/f.go"})
		for _, fn := range decls {
			nodes = append(nodes, schema.Node{
				ID: d + "/f.go::" + fn, Label: fn, Kind: "function", Source: d + "/f.go", Location: "L1-L2",
			})
		}
	}
	nodes = append(nodes,
		schema.Node{ID: "port:network.http:</a", Label: "/a", Kind: "port", Source: "port://network.http//a",
			Metadata: map[string]string{"transport": "network.http", "direction": "provides", "identifier": "/a"}},
		schema.Node{ID: "port:network.http:</b", Label: "/b", Kind: "port", Source: "port://network.http//b",
			Metadata: map[string]string{"transport": "network.http", "direction": "provides", "identifier": "/b"}},
		schema.Node{ID: "port:config.env:>DB_PASSWORD", Label: "DB_PASSWORD", Kind: "port", Source: "port://config.env/DB_PASSWORD",
			Metadata: map[string]string{"transport": "config.env", "direction": "consumes", "sensitive": "true", "identifier": "DB_PASSWORD"}},
		// alphabetically FIRST and statically resolved: only the sensitive-first
		// rule can keep DB_PASSWORD at sample[0] against this one.
		schema.Node{ID: "port:config.env:>AAA_REGION", Label: "AAA_REGION", Kind: "port", Source: "port://config.env/AAA_REGION",
			Metadata: map[string]string{"transport": "config.env", "direction": "consumes", "identifier": "AAA_REGION"}},
		schema.Node{ID: "port:config.env:>${dyn}", Label: "${dyn}", Kind: "port", Source: "port://config.env/${dyn}",
			Metadata: map[string]string{"transport": "config.env", "direction": "consumes", "resolved": "dynamic", "identifier": "${dyn}"}},
		// a module node: has no directory, must never become a card
		schema.Node{ID: "module://fmt", Label: "fmt", Kind: "module", Source: "module://fmt"},
	)

	call := func(from, to string, n int) []schema.Edge {
		var out []schema.Edge
		for i := 0; i < n; i++ {
			out = append(out, schema.Edge{
				Source: from + "/f.go", Target: to + "/f.go", Relation: "calls", Confidence: schema.Inferred,
			})
		}
		return out
	}
	var edges []schema.Edge
	edges = append(edges, call("src/handlers", "src/services", 20)...)
	edges = append(edges, call("src/services", "src/repository", 15)...)
	edges = append(edges, call("src/repository", "src/db", 12)...)
	edges = append(edges, call("src/handlers", "src/repository", 4)...)
	edges = append(edges, call("src/services", "src/util", 9)...)
	edges = append(edges, call("src/util", "src/db", 3)...)
	edges = append(edges, call("test/api", "src/handlers", 30)...)
	// an AMBIGUOUS call the store refused to attribute: must never be drawn
	edges = append(edges, schema.Edge{
		Source: "src/util/f.go", Target: "src/services/f.go", Relation: "calls", Confidence: schema.Ambiguous,
	})
	// self-loop inside one directory: carries no cross-directory information
	edges = append(edges, schema.Edge{
		Source: "src/db/f.go", Target: "src/db/f.go", Relation: "calls", Confidence: schema.Inferred,
	})
	edges = append(edges,
		schema.Edge{Source: "src/handlers/f.go", Target: "port:network.http:</a", Relation: "provides", Confidence: schema.Inferred},
		schema.Edge{Source: "src/handlers/f.go", Target: "port:network.http:</b", Relation: "provides", Confidence: schema.Inferred},
		schema.Edge{Source: "src/db/f.go", Target: "port:config.env:>DB_PASSWORD", Relation: "consumes", Confidence: schema.Inferred},
		schema.Edge{Source: "src/db/f.go", Target: "port:config.env:>AAA_REGION", Relation: "consumes", Confidence: schema.Inferred},
		schema.Edge{Source: "src/db/f.go", Target: "port:config.env:>${dyn}", Relation: "consumes", Confidence: schema.Inferred},
	)
	return nodes, edges
}

func cardByID(s Scene, id string) *Card {
	for i := range s.Cards {
		if s.Cards[i].ID == id {
			return &s.Cards[i]
		}
	}
	return nil
}

func linkOf(s Scene, from, to, rel string) *Link {
	for i := range s.Links {
		if s.Links[i].From == from && s.Links[i].To == to && s.Links[i].Relation == rel {
			return &s.Links[i]
		}
	}
	return nil
}

// TestDeriveLiftsRealEdgeCounts pins that a drawn arrow is N REAL store edges.
// The weights below are counted from fixture()'s call() arguments by hand.
func TestDeriveLiftsRealEdgeCounts(t *testing.T) {
	n, e := fixture()
	s := Derive("demo", n, e, Options{})

	want := map[[2]string]int{
		{"src/handlers", "src/services"}:   20,
		{"src/services", "src/repository"}: 15,
		{"src/repository", "src/db"}:       12,
		{"src/handlers", "src/repository"}: 4,
		{"src/services", "src/util"}:       9,
		{"src/util", "src/db"}:             3,
	}
	for k, w := range want {
		l := linkOf(s, k[0], k[1], "calls")
		if l == nil {
			t.Fatalf("missing lifted link %s -> %s", k[0], k[1])
		}
		if l.Weight != w {
			t.Errorf("%s -> %s: weight %d, want %d", k[0], k[1], l.Weight, w)
		}
		if l.Label != "CALLS" {
			t.Errorf("%s -> %s: label %q, want CALLS", k[0], k[1], l.Label)
		}
	}
	// self-loops never become a link
	if l := linkOf(s, "src/db", "src/db", "calls"); l != nil {
		t.Errorf("a within-directory edge became a link: %+v", l)
	}
	// AMBIGUOUS is refused: util -> services was 1 ambiguous call only
	if l := linkOf(s, "src/util", "src/services", "calls"); l != nil {
		t.Errorf("AMBIGUOUS call was drawn as fact: %+v", l)
	}
	// a module:// node has no directory and must not appear as a card
	if c := cardByID(s, "module://fmt"); c != nil {
		t.Errorf("a module node became a card: %+v", c)
	}
	// test trees are excluded by default and the scene must SAY so
	if c := cardByID(s, "test/api"); c != nil {
		t.Errorf("test directory drawn despite IncludeTests=false")
	}
	if !strings.Contains(strings.Join(s.Notes, "\n"), "test/fixture directories excluded") {
		t.Errorf("exclusion not disclosed in notes: %v", s.Notes)
	}
}

// TestDeriveIsNotAResortedList is THE gate this whole view exists to pass.
// The killed wall view (openspec/changes/2026-08-13-serve-world) died because
// "position carried no information — it was the sort order", and because it
// drew no edges. This asserts both, from the fixture's arrows:
//
//	(a) the scene draws links at all, and
//	(b) a card's LAYER follows the direction of the arrows, not the ranking:
//	    handlers < services < repository < db, while the RANK order (by lifted
//	    degree) is a different order entirely.
func TestDeriveIsNotAResortedList(t *testing.T) {
	n, e := fixture()
	s := Derive("demo", n, e, Options{})

	if len(s.Links) == 0 {
		t.Fatal("scene drew zero links — a map with no routes is a list in a costume")
	}
	chain := []string{"src/handlers", "src/services", "src/repository", "src/db"}
	for i := 1; i < len(chain); i++ {
		a, b := cardByID(s, chain[i-1]), cardByID(s, chain[i])
		if a == nil || b == nil {
			t.Fatalf("chain card missing: %s / %s", chain[i-1], chain[i])
		}
		if a.Layer >= b.Layer {
			t.Errorf("layer does not follow the arrows: %s(L%d) should be left of %s(L%d)",
				a.ID, a.Layer, b.ID, b.Layer)
		}
	}
	// And prove layer is NOT the rank: by lifted degree the order is
	// handlers(24 out) / services(35) / repository(31) / db(15) / util(12) —
	// so a layer that equalled rank would put services first, not handlers.
	byRank := map[string]int{}
	for _, c := range s.Cards {
		byRank[c.ID] = c.In + c.Out
	}
	if byRank["src/services"] <= byRank["src/handlers"] {
		t.Fatalf("fixture no longer distinguishes rank from layer (services %d, handlers %d)",
			byRank["src/services"], byRank["src/handlers"])
	}
	if cardByID(s, "src/handlers").Layer != 0 {
		t.Errorf("handlers is the source of the chain and must be layer 0, got %d",
			cardByID(s, "src/handlers").Layer)
	}
}

// TestDeriveHubIsMostDependedOn pins the hub choice to in-degree, from the
// fixture's arrows: db receives 12+3 = 15 and sends 0.
func TestDeriveHubIsMostDependedOn(t *testing.T) {
	n, e := fixture()
	s := Derive("demo", n, e, Options{})
	var hubs []string
	for _, c := range s.Cards {
		if c.Hub {
			hubs = append(hubs, c.ID)
		}
	}
	if len(hubs) != 1 || hubs[0] != "src/db" {
		t.Fatalf("hub = %v, want [src/db]", hubs)
	}
	h := cardByID(s, "src/db")
	if h.In != 15 || h.Out != 0 {
		t.Errorf("hub in/out = %d/%d, want 15/0", h.In, h.Out)
	}
	// The hub sits past every card: nothing may share or exceed its column.
	for _, c := range s.Cards {
		if !c.Hub && c.Layer >= h.Layer {
			t.Errorf("card %s (L%d) is not left of the hub (L%d)", c.ID, c.Layer, h.Layer)
		}
	}
}

// TestDeriveWorldCarriesNamesNeverValues walks the ENCODED scene and fails on
// any key that could carry a credential, plus the fixture's planted fake
// secret value. Ports enter the graph as NAMES; nothing downstream may add a
// value field without this failing.
func TestDeriveWorldCarriesNamesNeverValues(t *testing.T) {
	n, e := fixture()
	// plant a fake value where a careless implementation would pick it up
	for i := range n {
		if n[i].ID == "port:config.env:>DB_PASSWORD" {
			n[i].Metadata["org.example"] = "hunter2-NOT-A-REAL-SECRET"
		}
	}
	s := Derive("demo", n, e, Options{})

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2") {
		t.Fatal("a port's value-shaped metadata reached the scene payload")
	}
	var any interface{}
	if err := json.Unmarshal(raw, &any); err != nil {
		t.Fatal(err)
	}
	assertNoValueKeys(t, any, "$")

	// the NAME itself must be there — that is the whole point
	var got []string
	for _, w := range s.World {
		for _, d := range w.Sample {
			got = append(got, d.Label)
		}
	}
	if !contains(got, "DB_PASSWORD") {
		t.Fatalf("port names missing from the scene: %v", got)
	}
	for _, w := range s.World {
		if w.Transport == "config.env" {
			if w.Sensitive != 1 {
				t.Errorf("config.env sensitive = %d, want 1", w.Sensitive)
			}
			if w.Sample[0].Label != "DB_PASSWORD" || !w.Sample[0].Sensitive {
				t.Errorf("sensitive name must sort first and stay flagged, got %+v", w.Sample[0])
			}
			// dynamic placeholders sort LAST: a sample full of ${…} teaches nothing
			if last := w.Sample[len(w.Sample)-1]; last.Label != "${dyn}" || !last.Dynamic {
				t.Errorf("dynamic placeholder must sort last, got %+v", last)
			}
		}
		if w.Transport == "network.http" && w.Provides != 2 {
			t.Errorf("network.http provides = %d, want 2", w.Provides)
		}
	}
}

var forbiddenKeys = map[string]bool{
	"value": true, "values": true, "secret": true, "password": true,
	"token": true, "credential": true, "credentials": true, "dsn": true, "url": true,
}

func assertNoValueKeys(t *testing.T, v interface{}, path string) {
	t.Helper()
	switch x := v.(type) {
	case map[string]interface{}:
		for k, sub := range x {
			if forbiddenKeys[strings.ToLower(k)] {
				t.Errorf("%s.%s: forbidden key %q in the scene payload", path, k, k)
			}
			assertNoValueKeys(t, sub, path+"."+k)
		}
	case []interface{}:
		for i, sub := range x {
			assertNoValueKeys(t, sub, path+"[]")
			_ = i
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// TestDeriveIsDeterministic: same graph in, byte-identical scene out, whatever
// order the store hands the nodes back in.
func TestDeriveIsDeterministic(t *testing.T) {
	n, e := fixture()
	a, err := json.Marshal(Derive("demo", n, e, Options{}))
	if err != nil {
		t.Fatal(err)
	}
	// reverse both slices: map iteration inside Derive must not leak out
	rn := make([]schema.Node, len(n))
	for i := range n {
		rn[i] = n[len(n)-1-i]
	}
	re := make([]schema.Edge, len(e))
	for i := range e {
		re[i] = e[len(e)-1-i]
	}
	b, err := json.Marshal(Derive("demo", rn, re, Options{}))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("scene is not deterministic\nA: %s\nB: %s", a, b)
	}
}

// TestDeriveSaysWhatItIsHiding: the scene is a sample and must print that.
func TestDeriveSaysWhatItIsHiding(t *testing.T) {
	n, e := fixture()
	s := Derive("demo", n, e, Options{Cards: 2})
	if s.SubsystemsShown >= s.SubsystemsTotal {
		t.Fatalf("expected a sample: shown %d of %d", s.SubsystemsShown, s.SubsystemsTotal)
	}
	joined := strings.Join(s.Notes, "\n")
	for _, want := range []string{"SAMPLE", "lifted relations drawn", "longest-path depth"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes never say %q:\n%s", want, joined)
		}
	}
	if s.LiftedShown >= s.LiftedTotal {
		t.Errorf("with 2 cards the scene must draw fewer links than exist: %d of %d",
			s.LiftedShown, s.LiftedTotal)
	}
}

// TestDeriveEmptyGraphRefusesToPretend: no cross-directory edge means there is
// no flow, and the scene says so instead of drawing a list of boxes.
func TestDeriveEmptyGraphRefusesToPretend(t *testing.T) {
	nodes := []schema.Node{
		{ID: "a/f.go", Label: "f.go", Kind: "file", Source: "a/f.go"},
		{ID: "b/f.go", Label: "f.go", Kind: "file", Source: "b/f.go"},
	}
	s := Derive("demo", nodes, nil, Options{})
	if s.Empty == "" {
		t.Fatal("a graph with no cross-directory edges must report Empty, not draw cards")
	}
	if len(s.Cards) != 0 || len(s.Links) != 0 {
		t.Fatalf("drew %d cards / %d links from a graph with no flow", len(s.Cards), len(s.Links))
	}
}

// noPorts is fixture() with every `port` node and every port edge removed —
// i.e. a real repo whose boundaries have never been run. Nothing else changes,
// so the cards and links below are identical to the main fixture's.
func noPorts() ([]schema.Node, []schema.Edge) {
	nodes, edges := fixture()
	var n2 []schema.Node
	for _, n := range nodes {
		if n.Kind != "port" {
			n2 = append(n2, n)
		}
	}
	var e2 []schema.Edge
	for _, e := range edges {
		if !strings.HasPrefix(e.Target, "port:") {
			e2 = append(e2, e)
		}
	}
	return n2, e2
}

// TestSceneNeverMarshalsNullArrays is the gate for a live black screen: a store
// with no ports produced no chips, Go marshalled the nil slice as `null`, and
// the client's `for (const s of scene.chips)` threw — blanking a view that had
// seven cards and twenty-one links ready to draw. The client types every one of
// these fields as an array, so `null` is not a value this contract has.
//
// Written as a blanket rule over the marshalled JSON rather than field by
// field: the next slice added to Scene is covered without anyone remembering.
func TestSceneNeverMarshalsNullArrays(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func() ([]schema.Node, []schema.Edge)
	}{
		{"full", fixture},
		{"no ports", noPorts},
		{"no flow at all", func() ([]schema.Node, []schema.Edge) {
			return []schema.Node{{ID: "a/f.go", Label: "f.go", Kind: "file", Source: "a/f.go"}}, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes, edges := tc.build()
			b, err := json.Marshal(Derive("demo", nodes, edges, Options{}))
			if err != nil {
				t.Fatal(err)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(b, &raw); err != nil {
				t.Fatal(err)
			}
			for k, v := range raw {
				if string(v) == "null" {
					t.Errorf("scene field %q marshalled as null — the client iterates it and will throw", k)
				}
			}
		})
	}
}

// TestDeriveSaysBoundariesAreMissing — an empty outer world and an outer world
// that was never gathered look identical on screen, and only one of them is the
// user's business to fix.
func TestDeriveSaysBoundariesAreMissing(t *testing.T) {
	nodes, edges := noPorts()
	s := Derive("demo", nodes, edges, Options{})
	if len(s.World) != 0 {
		t.Fatalf("fixture still has %d world groups — the port strip did not work", len(s.World))
	}
	found := false
	for _, n := range s.Notes {
		if strings.Contains(n, "no boundaries recorded") {
			found = true
		}
	}
	if !found {
		t.Errorf("no note explains the missing outer world; notes = %q", s.Notes)
	}
	// and the full fixture must NOT carry that note
	fn, fe := fixture()
	for _, n := range Derive("demo", fn, fe, Options{}).Notes {
		if strings.Contains(n, "no boundaries recorded") {
			t.Errorf("a store WITH ports claims it has none: %q", n)
		}
	}
}

// nested is a hand-written two-level repo. Directories are ranked at whatever
// depth they sit, so the TOP level shows a mix of levels — src/app (which holds
// files of its own) alongside its own children. The shape:
//
//	src/app ──calls──▶ src/app/web ──calls──▶ src/app/core ──calls──▶ src/app/data
//	                        └────────calls────────▶ src/lib/util   (20, the top hub)
//
// so src/lib/util is the most depended-upon thing in the REPO, while src/app/data
// is the most depended-upon thing INSIDE src/app. One scene cannot show both,
// and that is what drilling is for.
func nested() ([]schema.Node, []schema.Edge) {
	dirs := []string{"src/app", "src/app/web", "src/app/core", "src/app/data", "src/lib/util"}
	var nodes []schema.Node
	for _, d := range dirs {
		nodes = append(nodes, schema.Node{ID: d + "/f.go", Label: "f.go", Kind: "file", Source: d + "/f.go"})
		nodes = append(nodes, schema.Node{
			ID: d + "/f.go::F", Label: "F", Kind: "function", Source: d + "/f.go", Location: "L1-L2",
		})
	}
	call := func(from, to string, n int) []schema.Edge {
		var out []schema.Edge
		for i := 0; i < n; i++ {
			out = append(out, schema.Edge{
				Source: from + "/f.go", Target: to + "/f.go", Relation: "calls", Confidence: schema.Inferred,
			})
		}
		return out
	}
	var edges []schema.Edge
	edges = append(edges, call("src/app", "src/app/web", 6)...)
	edges = append(edges, call("src/app/web", "src/app/core", 10)...)
	edges = append(edges, call("src/app/core", "src/app/data", 12)...)
	edges = append(edges, call("src/app/web", "src/lib/util", 20)...)
	return nodes, edges
}

// TestDrillRederivesRatherThanFilters is the whole claim of drill-down: inside
// src/app the ranking, the layering and the HUB are recomputed from the edges
// that exist there. At the top level src/app is a source (out-edges only, so it
// can never be the hub); one level down, src/app/data — invisible from the top —
// is the most depended-upon thing on screen. A filtered parent scene could not
// produce that.
func TestDrillRederivesRatherThanFilters(t *testing.T) {
	nodes, edges := nested()

	top := Derive("demo", nodes, edges, Options{})
	hubTop := ""
	for _, c := range top.Cards {
		if c.Hub {
			hubTop = c.ID
		}
	}
	if hubTop != "src/lib/util" {
		t.Fatalf("top-level hub = %q, want src/lib/util (20 in) — the fixture is not the shape the test assumes", hubTop)
	}
	app := cardByID(top, "src/app")
	if app == nil {
		t.Fatal("src/app must be a card at the top level")
	}
	// The affordance: src/app has three subdirectories, so it is enterable.
	if app.Children != 3 {
		t.Errorf("src/app: Children = %d, want 3 (web, core, data)", app.Children)
	}

	in := Derive("demo", nodes, edges, Options{Root: "src/app"})
	if in.Empty != "" {
		t.Fatalf("drilling into src/app reported empty: %q", in.Empty)
	}
	got := map[string]bool{}
	for _, c := range in.Cards {
		got[c.ID] = true
	}
	for _, want := range []string{"src/app/web", "src/app/core", "src/app/data"} {
		if !got[want] {
			t.Errorf("drilled scene is missing %s; cards = %v", want, got)
		}
	}
	if got["src/lib/util"] || got["src/lib"] {
		t.Errorf("drilled scene drew a directory outside the root; cards = %v", got)
	}
	// the edge that leaves the root must not be drawn at this level
	for _, l := range in.Links {
		if strings.HasPrefix(l.To, "src/lib") || strings.HasPrefix(l.From, "src/lib") {
			t.Errorf("drilled scene drew an edge leaving the root: %+v", l)
		}
	}
	// the recomputed hub
	hub := ""
	for _, c := range in.Cards {
		if c.Hub {
			hub = c.ID
		}
	}
	if hub != "src/app/data" {
		t.Errorf("hub inside src/app = %q, want src/app/data (8 in, 0 out)", hub)
	}
	// a leaf must not invite a click
	if d := cardByID(in, "src/app/data"); d == nil || d.Children != 0 {
		t.Errorf("src/app/data should be a leaf (Children 0), got %+v", d)
	}
}

// TestDrillCrumbsAlwaysLeadOut — a level you can enter and not leave is worse
// than no drill-down at all, so the trail is checked including on the two dead
// ends (a real leaf, and a root that does not exist).
func TestDrillCrumbsAlwaysLeadOut(t *testing.T) {
	nodes, edges := nested()
	for _, root := range []string{"", "src/app", "src/app/data", "src/nope/deeper"} {
		s := Derive("demo", nodes, edges, Options{Root: root})
		if len(s.Crumbs) == 0 || s.Crumbs[0].Root != "" {
			t.Fatalf("root=%q: crumbs must start at the whole repo, got %+v", root, s.Crumbs)
		}
		if s.Root != root {
			t.Errorf("root=%q: scene reports Root=%q", root, s.Root)
		}
		want := 1
		if root != "" {
			want = 1 + len(strings.Split(root, "/"))
		}
		if len(s.Crumbs) != want {
			t.Errorf("root=%q: %d crumbs, want %d (%+v)", root, len(s.Crumbs), want, s.Crumbs)
		}
		last := s.Crumbs[len(s.Crumbs)-1]
		if last.Root != root {
			t.Errorf("root=%q: last crumb points at %q", root, last.Root)
		}
	}
	// the two dead ends must be DIFFERENT messages: one is a typo, the other is
	// the truth about the code.
	leaf := Derive("demo", nodes, edges, Options{Root: "src/app/data"}).Empty
	ghost := Derive("demo", nodes, edges, Options{Root: "src/nope"}).Empty
	if leaf == "" || ghost == "" {
		t.Fatalf("a dead end must say why: leaf=%q ghost=%q", leaf, ghost)
	}
	if leaf == ghost {
		t.Errorf("a missing directory and a real leaf give the same message: %q", leaf)
	}
	if !strings.Contains(ghost, "no directory") {
		t.Errorf("a missing root should say so, got %q", ghost)
	}
}

// TestDrillSelfCardIsNotADoor — inside src/app there is a card for the files
// sitting directly in src/app. It is a real subsystem and belongs on screen, but
// clicking it would re-derive the scene already on screen, so it must not
// advertise itself as enterable.
func TestDrillSelfCardIsNotADoor(t *testing.T) {
	nodes, edges := nested()
	in := Derive("demo", nodes, edges, Options{Root: "src/app"})
	self := cardByID(in, "src/app")
	if self == nil {
		t.Fatal("the root's own files must still be drawn as a card")
	}
	if self.Children != 0 {
		t.Errorf("the self card offers %d children — clicking it re-enters the same scene", self.Children)
	}
	// but from OUTSIDE, the same directory is very much a door
	if top := cardByID(Derive("demo", nodes, edges, Options{}), "src/app"); top == nil || top.Children == 0 {
		t.Errorf("src/app must be enterable from the top level, got %+v", top)
	}
}
