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
	// ADR 21: a directory with no SUBDIRECTORIES is not a leaf — it opens onto
	// its files. src/app/data holds one file, so it is still a way in. This
	// assertion previously demanded the opposite, which is the belief the ADR
	// overturns: mm/kasan has no subdirectories and 17 files, 330 functions and
	// 361 real call edges inside it.
	if d := cardByID(in, "src/app/data"); d == nil || d.Children != 1 {
		t.Errorf("src/app/data holds 1 file and should open onto it, got %+v", d)
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

// TestSelfCardOpensAtFileGrain — inside src/app there is a card for the files
// sitting directly in src/app. It used to advertise nothing, because entering
// it at directory grain re-derives the scene already on screen. But those files
// are a real level (ADR 21), and on drivers/base/firmware_loader the hub card
// stood for ten of them with no way to open any. It is a door now; it just
// opens at FILE grain, which inference cannot reach because the same root also
// has subdirectories.
func TestSelfCardOpensAtFileGrain(t *testing.T) {
	nodes, edges := nested()
	in := Derive("demo", nodes, edges, Options{Root: "src/app"})
	self := cardByID(in, "src/app")
	if self == nil {
		t.Fatal("the root's own files must still be drawn as a card")
	}
	if self.Children == 0 {
		t.Error("the self card holds files and must offer them")
	}
	if self.EnterGrain != "file" {
		t.Errorf("the self card opens at %q; directory grain would re-derive this scene", self.EnterGrain)
	}
	files := Derive("demo", nodes, edges, Options{Root: "src/app", Grain: "file"})
	if files.Level != "file" {
		t.Fatalf("forcing file grain gave level %q", files.Level)
	}
	for _, c := range files.Cards {
		if c.ID == "src/app/web" || c.ID == "src/app/core" {
			t.Errorf("file grain drew a subdirectory: %+v", c)
		}
	}
	// a card reached by inference alone carries no forced grain
	if top := cardByID(Derive("demo", nodes, edges, Options{}), "src/app"); top == nil || top.EnterGrain != "" {
		t.Errorf("src/app from the top level should infer its grain, got %+v", top)
	}
}

// TestQuestionsUseRealNamesAndRealVerbs — a suggested question is only useful
// if it can be pasted. Both halves have to be true: the names must be names
// this repo contains, and the command must be a verb this binary has. A
// plausible-looking command that errors is worse than no suggestion at all.
func TestQuestionsUseRealNamesAndRealVerbs(t *testing.T) {
	nodes, edges := fixture()
	s := Derive("demo", nodes, edges, Options{})
	if len(s.Questions) == 0 {
		t.Fatal("a scene with a hub, links and ports produced no questions")
	}
	// every verb the questions may use, from `ctx-optimize help`
	verbs := map[string]bool{
		"change-plan": true, "affected": true, "path": true, "card": true,
		"boundaries": true, "query": true, "explain": true, "nodes": true, "edges": true,
	}
	// Every declaration label the fixture actually defines. The fixture is
	// re-labelled with QUALIFIERS on purpose: the first version of this test
	// used bare names, so it never noticed that Top was being handed the
	// display-stripped form and produced commands that resolved to nothing.
	declared := map[string]bool{}
	for i := range nodes {
		if declKinds[nodes[i].Kind] {
			nodes[i].Label = "Pkg." + nodes[i].Label
			declared[nodes[i].Label] = true
		}
	}
	s = Derive("demo", nodes, edges, Options{})
	for _, q := range s.Questions {
		if q.Text == "" || q.Command == "" {
			t.Errorf("half-built question: %+v", q)
		}
		fields := strings.Fields(q.Command)
		if len(fields) < 2 || fields[0] != "ctx-optimize" {
			t.Errorf("command is not a ctx-optimize invocation: %q", q.Command)
			continue
		}
		if !verbs[fields[1]] {
			t.Errorf("question uses a verb this binary does not have: %q", q.Command)
		}
		// any quoted argument must be a symbol the fixture defines
		for _, arg := range strings.Split(q.Command, "\"") {
			if arg == "" || strings.HasPrefix(arg, "ctx-optimize") || strings.TrimSpace(arg) == "" {
				continue
			}
			if strings.HasPrefix(arg, "-") || strings.Contains(arg, " ") {
				continue
			}
			if !declared[arg] {
				t.Errorf("question asks about %q, which this repo does not declare (command %q)", arg, q.Command)
			}
		}
	}
}

// TestQuestionsFollowTheScene — the questions describe THIS scene, so drilling
// into a different level must produce different ones. A fixed list dressed up
// as derivation is the same failure as position that carries no information.
func TestQuestionsFollowTheScene(t *testing.T) {
	nodes, edges := nested()
	top := Derive("demo", nodes, edges, Options{})
	in := Derive("demo", nodes, edges, Options{Root: "src/app"})
	if len(top.Questions) == 0 || len(in.Questions) == 0 {
		t.Fatalf("questions missing: top=%d inside=%d", len(top.Questions), len(in.Questions))
	}
	same := len(top.Questions) == len(in.Questions)
	for i := range top.Questions {
		if !same {
			break
		}
		if top.Questions[i] != in.Questions[i] {
			same = false
		}
	}
	if same {
		t.Error("the same questions came back for the whole repo and for one directory inside it")
	}
	// a store with no ports must not offer a boundaries question
	np, ne := noPorts()
	for _, q := range Derive("demo", np, ne, Options{}).Questions {
		if strings.Contains(q.Command, "boundaries") {
			t.Errorf("offered a boundaries question for a store with no ports: %q", q.Command)
		}
	}
}

// TestDeriveSaysWhenEveryDirectoryIsALeaf — on linux/mm all four subsystems are
// genuinely leaf directories, and the level read as though drill-down were
// broken. A leaf is a fact about the code and has to say so.
// oneFile is a single file with real internal call structure — three functions
// where `handle` calls `parse` and `store`, and `store` calls `parse`. This is
// the shape ADR 21 exists for: at directory grain none of it is visible,
// because every one of these edges is internal to a single card.
func oneFile() ([]schema.Node, []schema.Edge) {
	const src = "src/app/web/f.go"
	nodes := []schema.Node{
		{ID: src, Label: "f.go", Kind: "file", Source: src},
		{ID: src + "::handle", Label: "handle", Kind: "function", Source: src, Location: "L10-L20"},
		{ID: src + "::parse", Label: "parse", Kind: "function", Source: src, Location: "L22-L30"},
		{ID: src + "::store", Label: "store", Kind: "function", Source: src, Location: "L32-L40"},
	}
	call := func(a, b string, n int) []schema.Edge {
		var out []schema.Edge
		for i := 0; i < n; i++ {
			out = append(out, schema.Edge{
				Source: src + "::" + a, Target: src + "::" + b,
				Relation: "calls", Confidence: schema.Inferred,
			})
		}
		return out
	}
	var edges []schema.Edge
	edges = append(edges, call("handle", "parse", 3)...)
	edges = append(edges, call("handle", "store", 2)...)
	edges = append(edges, call("store", "parse", 4)...)
	return nodes, edges
}

// TestDrillReachesTheDeclarations is the whole of ADR 21: a file is not a wall.
// At directory grain these three functions are one card and their seven call
// edges are invisible; scoped to the file they are the scene.
func TestDrillReachesTheDeclarations(t *testing.T) {
	nodes, edges := oneFile()
	s := Derive("demo", nodes, edges, Options{Root: "src/app/web/f.go"})
	if s.Level != "declaration" {
		t.Fatalf("level = %q, want declaration", s.Level)
	}
	if s.Empty != "" {
		t.Fatalf("a file with three functions calling each other drew nothing: %q", s.Empty)
	}
	got := map[string]*Card{}
	for i := range s.Cards {
		got[s.Cards[i].Label] = &s.Cards[i]
	}
	for _, want := range []string{"handle", "parse", "store"} {
		if got[want] == nil {
			t.Errorf("declaration %q is missing; cards = %v", want, s.Cards)
		}
	}
	// `parse` is called by both others (3 + 4) and calls nothing: it is the hub
	if p := got["parse"]; p == nil || !p.Hub || p.In != 7 {
		t.Errorf("parse should be the hub with 7 edges in, got %+v", p)
	}
	// a declaration carries its file AND its line range, which is the point of
	// descending this far
	if p := got["parse"]; p != nil && !strings.Contains(p.Dir, "L22-L30") {
		t.Errorf("declaration card does not carry its location: %q", p.Dir)
	}
	// and it is the floor
	for _, c := range s.Cards {
		if c.Children != 0 {
			t.Errorf("a declaration claims something is inside it: %+v", c)
		}
	}
}

func TestDeriveSaysWhenItHasRunOutOfLevels(t *testing.T) {
	nodes, edges := oneFile()
	// Declaration grain IS the floor, and has to say so.
	decl := Derive("demo", nodes, edges, Options{Root: "src/app/web/f.go"})
	if decl.Level != "declaration" {
		t.Fatalf("scoping to a file gave level %q, want declaration", decl.Level)
	}
	floor := false
	for _, n := range decl.Notes {
		if strings.Contains(n, "floor") {
			floor = true
		}
	}
	if !floor {
		t.Errorf("the deepest level does not say it is the deepest; notes = %q", decl.Notes)
	}
	// and a level with somewhere to go must NOT claim it has run out
	nn, ne := nested()
	for _, n := range Derive("demo", nn, ne, Options{}).Notes {
		if strings.Contains(n, "as deep as the store goes") || strings.Contains(n, "floor") {
			t.Errorf("a level with an enterable card claims it is the floor: %q", n)
		}
	}
}

// TestDrillReachesTheFiles — the level between directories and declarations.
// A directory with no SUBDIRECTORIES was reported as a leaf, which is how
// mm/kasan (17 files, 330 functions, 361 real call edges) came to be a wall.
func TestDrillReachesTheFiles(t *testing.T) {
	// two files in one directory, calling each other; no subdirectories
	const dir = "src/leafdir"
	a, b := dir+"/a.go", dir+"/b.go"
	nodes := []schema.Node{
		{ID: a, Label: "a.go", Kind: "file", Source: a},
		{ID: b, Label: "b.go", Kind: "file", Source: b},
		{ID: a + "::A", Label: "A", Kind: "function", Source: a, Location: "L1-L5"},
		{ID: b + "::B", Label: "B", Kind: "function", Source: b, Location: "L1-L5"},
		// something outside, so the directory is reachable at the top level
		{ID: "src/other/c.go", Label: "c.go", Kind: "file", Source: "src/other/c.go"},
		{ID: "src/other/c.go::C", Label: "C", Kind: "function", Source: "src/other/c.go", Location: "L1-L5"},
	}
	var edges []schema.Edge
	for i := 0; i < 6; i++ {
		edges = append(edges, schema.Edge{Source: a + "::A", Target: b + "::B",
			Relation: "calls", Confidence: schema.Inferred})
	}
	for i := 0; i < 2; i++ {
		edges = append(edges, schema.Edge{Source: "src/other/c.go::C", Target: a + "::A",
			Relation: "calls", Confidence: schema.Inferred})
	}

	top := Derive("demo", nodes, edges, Options{})
	leaf := cardByID(top, dir)
	if leaf == nil {
		t.Fatalf("%s is missing from the top level; cards = %v", dir, top.Cards)
	}
	// it has NO subdirectories, and it is still a way in — 2 files
	if leaf.Children != 2 {
		t.Errorf("%s has no subdirectories but 2 files; Children = %d, want 2", dir, leaf.Children)
	}

	in := Derive("demo", nodes, edges, Options{Root: dir})
	if in.Level != "file" {
		t.Fatalf("scoping to a directory with no subdirectories gave level %q, want file", in.Level)
	}
	if in.Empty != "" {
		t.Fatalf("a directory with two files calling each other drew nothing: %q", in.Empty)
	}
	names := map[string]*Card{}
	for i := range in.Cards {
		names[in.Cards[i].Label] = &in.Cards[i]
	}
	if names["a.go"] == nil || names["b.go"] == nil {
		t.Fatalf("file cards missing; got %v", in.Cards)
	}
	// the 6 A->B calls lift to one a.go -> b.go arrow
	l := linkOf(in, a, b, "calls")
	if l == nil || l.Weight != 6 {
		t.Errorf("file-to-file arrow should be 6 real calls, got %+v", l)
	}
	// the edge that LEAVES the directory must not be drawn at this level
	for _, lk := range in.Links {
		if strings.HasPrefix(lk.From, "src/other") || strings.HasPrefix(lk.To, "src/other") {
			t.Errorf("drew an edge from outside the directory: %+v", lk)
		}
	}
	// each file opens onto its declarations
	if names["a.go"].Children != 1 {
		t.Errorf("a.go holds 1 declaration; Children = %d", names["a.go"].Children)
	}
	// and the level says what a card now means
	said := false
	for _, n := range in.Notes {
		if strings.Contains(n, "a card here is a file") {
			said = true
		}
	}
	if !said {
		t.Errorf("the file level does not say a card is a file; notes = %q", in.Notes)
	}
}

// TestCardsCountTrafficLeavingTheScene — inside one file, a function's callers
// in that file are usually a small fraction of its callers in the repo, and the
// repo-wide number is the one that says whether it is load-bearing. Those edges
// cannot be DRAWN (the other end is not on screen) but they must be counted.
func TestCardsCountTrafficLeavingTheScene(t *testing.T) {
	const src = "src/app/web/f.go"
	nodes, edges := oneFile()
	// something far away calls `parse` 100 times and `handle` calls out twice
	nodes = append(nodes,
		schema.Node{ID: "src/far/g.go", Label: "g.go", Kind: "file", Source: "src/far/g.go"},
		schema.Node{ID: "src/far/g.go::G", Label: "G", Kind: "function", Source: "src/far/g.go", Location: "L1-L2"},
	)
	for i := 0; i < 100; i++ {
		edges = append(edges, schema.Edge{Source: "src/far/g.go::G", Target: src + "::parse",
			Relation: "calls", Confidence: schema.Inferred})
	}
	for i := 0; i < 2; i++ {
		edges = append(edges, schema.Edge{Source: src + "::handle", Target: "src/far/g.go::G",
			Relation: "calls", Confidence: schema.Inferred})
	}

	s := Derive("demo", nodes, edges, Options{Root: src})
	byLabel := map[string]*Card{}
	for i := range s.Cards {
		byLabel[s.Cards[i].Label] = &s.Cards[i]
	}
	parse, handle := byLabel["parse"], byLabel["handle"]
	if parse == nil || handle == nil {
		t.Fatalf("missing declarations; cards = %v", s.Cards)
	}
	// in-scene numbers are unchanged: 3 + 4 calls to parse from this file
	if parse.In != 7 {
		t.Errorf("parse.In = %d, want 7 (the calls inside this file)", parse.In)
	}
	// and the 100 callers from elsewhere are counted, not lost
	if parse.ExtIn != 100 {
		t.Errorf("parse.ExtIn = %d, want 100 (callers outside this file)", parse.ExtIn)
	}
	if handle.ExtOut != 2 {
		t.Errorf("handle.ExtOut = %d, want 2 (calls leaving this file)", handle.ExtOut)
	}
	// crossing edges are NEVER drawn as arrows — the other end is not on screen
	for _, l := range s.Links {
		if strings.Contains(l.From, "src/far") || strings.Contains(l.To, "src/far") {
			t.Errorf("drew an arrow to something not in the scene: %+v", l)
		}
	}
	// a declaration calling itself-in-file must not be double counted as external
	if handle.ExtIn != 0 {
		t.Errorf("handle.ExtIn = %d, want 0", handle.ExtIn)
	}
}
