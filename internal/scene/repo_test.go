package scene

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// mods builds the shape the spike found everywhere: a client module that
// declares the package a sibling publishes.
func mods() []RepoModule {
	return []RepoModule{
		{Key: "acme/apps/ui", Path: "apps/ui", Name: "ui", Nodes: 100, Edges: 200, Code: 150,
			Publishes: []string{"dep:npm/@acme/ui"},
			Declares:  []string{"dep:npm/@acme/api", "dep:npm/react"}},
		{Key: "acme/apps/api", Path: "apps/api", Name: "api", Nodes: 300, Edges: 600, Code: 500,
			Publishes: []string{"dep:npm/@acme/api"},
			Declares:  []string{"dep:npm/express"}},
		{Key: "acme/apps/worker", Path: "apps/worker", Name: "worker", Nodes: 50, Edges: 60, Code: 40,
			Publishes: []string{"dep:npm/@acme/worker"},
			Declares:  []string{"dep:npm/@acme/api"}},
	}
}

func repoLink(sc Scene, from, to string) (Link, bool) {
	for _, l := range sc.Links {
		if l.From == from && l.To == to {
			return l, true
		}
	}
	return Link{}, false
}

// The whole point of D0: two DECLARATIONS meeting on one global id become the
// arrow a reader has been asking for.
func TestDeriveRepoJoinsDeclaresToPublishes(t *testing.T) {
	sc := DeriveRepo("acme", mods(), Options{})
	if sc.Level != "module" {
		t.Fatalf("level = %q, want module", sc.Level)
	}
	l, ok := repoLink(sc, "acme/apps/ui", "acme/apps/api")
	if !ok {
		t.Fatalf("no ui -> api link; links = %+v", sc.Links)
	}
	if l.Relation != "depends" || l.Weight != 1 {
		t.Fatalf("ui -> api = %+v, want depends weight 1", l)
	}
	if _, ok := repoLink(sc, "acme/apps/worker", "acme/apps/api"); !ok {
		t.Fatalf("no worker -> api link; links = %+v", sc.Links)
	}
	// ui declares TWO packages and only one of them is a sibling's. The weight
	// asserted above is what proves react is not counted — a check that the
	// external id never appears as a link target could not fail, because a link
	// is only drawn between two cards and react was never a card.
	if len(sc.Cards) != 3 {
		t.Fatalf("cards = %d, want 3", len(sc.Cards))
	}
}

// A card's ID is the store key, because that is what a click has to open.
func TestDeriveRepoCardOpensTheModuleStore(t *testing.T) {
	sc := DeriveRepo("acme", mods(), Options{})
	for _, c := range sc.Cards {
		if !strings.HasPrefix(c.ID, "acme/") {
			t.Fatalf("card %q is not a store key", c.ID)
		}
		if c.Children == 0 || c.Inner == 0 {
			t.Fatalf("module %q has code but offers no way in (children=%d inner=%d)",
				c.ID, c.Children, c.Inner)
		}
	}
	// A module with no code is a leaf: offering a door to an empty screen is
	// the exact failure ADR 21 fixed at directory grain.
	m := mods()
	m[0].Code = 0
	sc = DeriveRepo("acme", m, Options{})
	for _, c := range sc.Cards {
		if c.ID == "acme/apps/ui" && c.Children != 0 {
			t.Fatalf("module with no code still invites a click: %+v", c)
		}
	}
}

// The hub is what the repo hangs off, and it is the module OTHERS declare.
func TestDeriveRepoHubIsMostDependedUpon(t *testing.T) {
	sc := DeriveRepo("acme", mods(), Options{})
	hub := ""
	for _, c := range sc.Cards {
		if c.Hub {
			hub = c.ID
		}
	}
	if hub != "acme/apps/api" {
		t.Fatalf("hub = %q, want acme/apps/api", hub)
	}
}

// `shares` means "both call the same external service". It must never be drawn
// where a real dependency already says something stronger and more specific.
func TestDeriveRepoSharesIsSeparateFromDepends(t *testing.T) {
	m := mods()
	openai := RepoPort{ID: "port:network.http:>api.openai.com", Transport: "network.http",
		Direction: "consumes", Identifier: "api.openai.com"}
	m[0].Ports = []RepoPort{openai}
	m[1].Ports = []RepoPort{openai}
	m[2].Ports = []RepoPort{openai}
	sc := DeriveRepo("acme", m, Options{})
	for _, l := range sc.Links {
		if l.Relation != "shares" {
			continue
		}
		if (l.From == "acme/apps/ui" && l.To == "acme/apps/api") ||
			(l.From == "acme/apps/api" && l.To == "acme/apps/ui") {
			t.Fatalf("shares drawn over a real depends: %+v", l)
		}
	}
	l, ok := repoLink(sc, "acme/apps/ui", "acme/apps/worker")
	if !ok {
		t.Fatalf("ui/worker share a port and no dependency, expected a shares link; got %+v", sc.Links)
	}
	if l.Relation != "shares" {
		t.Fatalf("ui -> worker = %q, want shares", l.Relation)
	}
	explained := false
	for _, n := range sc.Notes {
		if strings.Contains(n, "not a call between them") {
			explained = true
		}
	}
	if !explained {
		t.Fatalf("the dashed link is drawn but never explained as not-a-call; notes = %v", sc.Notes)
	}
	// A COUNT is not an explanation. "SHARES 12" between a ui and an api reads
	// as "the ui calls the api"; only naming the third parties says otherwise,
	// and that has to travel on the link itself, where the reader is looking.
	if !strings.Contains(l.Detail, "api.openai.com") {
		t.Fatalf("the link does not name what is shared: %+v", l)
	}
	if l.Label == "SHARES" {
		t.Fatalf("the label is a verb the reader has to guess at: %q", l.Label)
	}
}

// A port one module PROVIDES and another CONSUMES is a call between them —
// directed, with an arrowhead — and must never be flattened into the symmetric
// "they both call the same third party".
func TestDeriveRepoDirectedCallBeatsSharedThirdParty(t *testing.T) {
	m := mods()
	route := "port:network.http:>/v1/resume"
	openai := "port:network.http:>api.openai.com"
	// api PROVIDES the route; worker CONSUMES it — that is a call between them.
	m[1].Ports = []RepoPort{{ID: route, Transport: "network.http", Direction: "provides", Identifier: "/v1/resume"}}
	m[2].Ports = []RepoPort{
		{ID: route, Transport: "network.http", Direction: "consumes", Identifier: "/v1/resume"},
		{ID: openai, Transport: "network.http", Direction: "consumes", Identifier: "api.openai.com"},
	}
	// ui and worker merely CONSUME the same third party.
	m[0].Ports = []RepoPort{{ID: openai, Transport: "network.http", Direction: "consumes", Identifier: "api.openai.com"}}
	sc := DeriveRepo("acme", m, Options{})

	l, ok := repoLink(sc, "acme/apps/worker", "acme/apps/api")
	if !ok {
		t.Fatalf("no worker -> api link; links = %+v", sc.Links)
	}
	// worker already DECLARES api's package, which is the stronger statement.
	if l.Relation != "depends" {
		t.Fatalf("worker -> api = %q; a declared dependency outranks a port join", l.Relation)
	}

	// ui neither declares nor provides anything worker needs, so the only thing
	// between them is a third party they both call.
	sh, ok := repoLink(sc, "acme/apps/ui", "acme/apps/worker")
	if !ok {
		sh, ok = repoLink(sc, "acme/apps/worker", "acme/apps/ui")
	}
	if !ok || sh.Relation != "shares" {
		t.Fatalf("ui/worker = %+v, want a dashed shares link", sh)
	}

	// And with the declaration removed, the port join is what is left — and it
	// is DIRECTED, because one side provides what the other consumes.
	m[2].Declares = nil
	sc = DeriveRepo("acme", m, Options{})
	c, ok := repoLink(sc, "acme/apps/worker", "acme/apps/api")
	if !ok || c.Relation != "calls" {
		t.Fatalf("worker -> api = %+v, want a directed calls link", c)
	}
	if !strings.Contains(c.Detail, "/v1/resume") {
		t.Fatalf("the call does not name what is called: %+v", c)
	}
	if _, back := repoLink(sc, "acme/apps/api", "acme/apps/worker"); back {
		t.Fatalf("a directed call was drawn in both directions")
	}
}

// Two modules publishing one name is a broken manifest, not a join. Guessing
// which one owns it would draw a confident arrow from no evidence.
func TestDeriveRepoContestedPackageIsDroppedNotGuessed(t *testing.T) {
	m := mods()
	m[2].Publishes = []string{"dep:npm/@acme/api"} // worker claims api's name
	sc := DeriveRepo("acme", m, Options{})
	for _, l := range sc.Links {
		if l.Relation == "depends" && l.To != "" &&
			(l.To == "acme/apps/api" || l.To == "acme/apps/worker") {
			t.Fatalf("contested package still drew an arrow: %+v", l)
		}
	}
	found := false
	for _, n := range sc.Notes {
		if strings.Contains(n, "more than one module") {
			found = true
		}
	}
	if !found {
		t.Fatalf("contested package dropped silently; notes = %v", sc.Notes)
	}
}

// A module whose graph could not be read keeps its card. Dropping it would
// read as "nothing depends on it", which is a claim we have no evidence for.
func TestDeriveRepoUnreadModuleKeepsItsCard(t *testing.T) {
	m := mods()
	m[1].Unread = true
	sc := DeriveRepo("acme", m, Options{})
	seen := false
	for _, c := range sc.Cards {
		if c.ID == "acme/apps/api" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("unread module dropped from the scene; cards = %+v", sc.Cards)
	}
	found := false
	for _, n := range sc.Notes {
		if strings.Contains(n, "could not be read") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unread module not disclosed; notes = %v", sc.Notes)
	}
}

// Modules that did not make the card cut still need a way in, or a big repo
// hides most of itself with no way to reach the rest.
func TestDeriveRepoOverflowModulesAreReachable(t *testing.T) {
	var m []RepoModule
	for i := 0; i < 20; i++ {
		m = append(m, RepoModule{
			Key: "big/pkg" + itoa(i), Path: "pkg" + itoa(i), Name: "pkg" + itoa(i),
			Nodes: 10, Edges: 10, Code: 5,
		})
	}
	sc := DeriveRepo("big", m, Options{Cards: 6})
	if len(sc.Cards) != 6 {
		t.Fatalf("cards = %d, want 6", len(sc.Cards))
	}
	if len(sc.Inside) != 14 {
		t.Fatalf("inside = %d, want the 14 modules not drawn", len(sc.Inside))
	}
	if sc.SubsystemsTotal != 20 {
		t.Fatalf("total = %d, want 20", sc.SubsystemsTotal)
	}
}

// The same contract Derive holds: no nil slice reaches the client, because
// `for (… of null)` blanks the whole viewer.
func TestDeriveRepoNeverSendsNull(t *testing.T) {
	for _, sc := range []Scene{DeriveRepo("empty", nil, Options{}), DeriveRepo("acme", mods(), Options{})} {
		data, err := json.Marshal(sc)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"cards", "links", "world", "stats", "chips", "notes", "crumbs", "questions", "inside"} {
			if m[k] == nil {
				t.Fatalf("scene.%s is null for %q", k, sc.Module)
			}
		}
	}
}

// A count and its noun have to agree. "1 modules" and "0 declared dependencys"
// are the kind of detail that makes a reader wonder what else is sloppy.
func TestDeriveRepoCountsAgreeWithTheirNouns(t *testing.T) {
	one := DeriveRepo("solo", mods()[:1], Options{})
	for _, st := range one.Stats {
		if st.Text == "1" && strings.HasSuffix(st.Label, "s") {
			t.Fatalf("stat reads %q %q", st.Text, st.Label)
		}
	}
	for _, sc := range []Scene{one, DeriveRepo("acme", mods(), Options{})} {
		for _, c := range append(sc.Chips, func() []string {
			var out []string
			for _, st := range sc.Stats {
				out = append(out, st.Text+" "+st.Label)
			}
			return out
		}()...) {
			if strings.Contains(c, "dependencys") || strings.Contains(c, "callss") ||
				strings.HasPrefix(c, "1 modules") || strings.Contains(c, "pairs calling") && strings.HasPrefix(c, "1 ") {
				t.Fatalf("count disagrees with its noun: %q", c)
			}
		}
	}
}

// A repo with no joins must say why nothing is drawn. A blank canvas and a
// broken feature look identical on screen.
func TestDeriveRepoWithNoJoinsSaysWhy(t *testing.T) {
	m := mods()
	for i := range m {
		m[i].Declares = nil
	}
	sc := DeriveRepo("acme", m, Options{})
	if len(sc.Links) != 0 {
		t.Fatalf("links = %+v, want none", sc.Links)
	}
	found := false
	for _, n := range sc.Notes {
		if strings.Contains(n, "declares no package another one publishes") ||
			strings.Contains(n, "no module in this repo declares") {
			found = true
		}
	}
	if !found {
		t.Fatalf("empty repo scene does not explain itself; notes = %v", sc.Notes)
	}
}

// A module that publishes nothing can never be pointed AT. That is a property
// of its manifest, not of the architecture, and the reader must be told.
func TestDeriveRepoSilentModulesAreDisclosed(t *testing.T) {
	m := mods()
	m[1].Publishes = nil
	sc := DeriveRepo("acme", m, Options{})
	found := false
	for _, n := range sc.Notes {
		if strings.Contains(n, "declare no package name of their own") {
			found = true
		}
	}
	if !found {
		t.Fatalf("silent module not disclosed; notes = %v", sc.Notes)
	}
}

// Every suggested command must be one this binary actually has, with a target
// that exists in this scene.
func TestDeriveRepoQuestionsAreRunnable(t *testing.T) {
	sc := DeriveRepo("acme", mods(), Options{})
	if len(sc.Questions) == 0 {
		t.Fatal("no questions on a scene with arrows")
	}
	for _, q := range sc.Questions {
		if !strings.HasPrefix(q.Command, "ctx-optimize edges ") {
			t.Fatalf("question command is not a real verb: %q", q.Command)
		}
		if strings.Contains(q.Command, "--module ") {
			t.Fatalf("question uses a flag this binary does not have: %q", q.Command)
		}
	}
}

// Vendored copies are real dependencies but they are not this repo's products.
func TestDeriveRepoFlagsVendoredModules(t *testing.T) {
	m := mods()
	m[2].Vendored = true
	sc := DeriveRepo("acme", m, Options{})
	for _, c := range sc.Cards {
		if c.ID == "acme/apps/worker" && !strings.Contains(c.Detail, "vendored") {
			t.Fatalf("vendored module not marked: %+v", c)
		}
	}
}

// The linux case, at directory grain: a transport plate whose ports are opened
// by directories that did not make the card cut. Without the names the plate
// floats with no arrow and reads as a broken link — on linux, all three of them
// do, because the seven drawn directories open none of the 68 ports.
func TestDeriveNamesWhoOpensAnUnconnectedPlate(t *testing.T) {
	var nodes []schema.Node
	var edges []schema.Edge
	// two directories with real code, which will be the drawn cards
	for _, d := range []string{"core", "util"} {
		nodes = append(nodes,
			schema.Node{ID: d + "/a.go", Label: "a.go", Kind: "file", FileType: "code", Source: d + "/a.go"},
			schema.Node{ID: d + "/a.go::F", Label: "F" + d, Kind: "function", FileType: "code", Source: d + "/a.go", Location: "L1-L9"})
	}
	edges = append(edges, schema.Edge{Source: "core/a.go", Target: "util/a.go", Relation: "imports", Confidence: schema.Extracted})
	// a port opened from a THIRD directory that has nothing else in it
	nodes = append(nodes,
		schema.Node{ID: "scripts/build.sh", Label: "build.sh", Kind: "file", FileType: "code", Source: "scripts/build.sh"},
		schema.Node{ID: "port:config.env:>CI_TOKEN", Label: "CI_TOKEN", Kind: "port", FileType: "boundary",
			Source:   "port://config.env/CI_TOKEN",
			Metadata: map[string]string{"transport": "config.env", "direction": "consumes", "identifier": "CI_TOKEN"}})
	edges = append(edges, schema.Edge{Source: "scripts/build.sh", Target: "port:config.env:>CI_TOKEN",
		Relation: "consumes", Confidence: schema.Inferred})

	// Cards is how many are drawn BESIDES the hub, so 1 gives core+util and
	// leaves scripts — the directory that actually opens the port — off screen,
	// which is the whole point of the fixture.
	sc := Derive("m", nodes, edges, Options{Cards: 1})
	if len(sc.World) != 1 {
		t.Fatalf("world = %+v, want the one config.env group", sc.World)
	}
	w := sc.World[0]
	if w.OpenerTotal != 1 || len(w.Openers) != 1 || w.Openers[0] != "scripts" {
		t.Fatalf("plate does not name who opens it: %+v", w)
	}
	for _, l := range sc.Links {
		if strings.HasPrefix(l.To, "world:") {
			t.Fatalf("an arrow was drawn to a plate no drawn card opens: %+v", l)
		}
	}
}

// A curve is labelled with the LAST segment of its transport. Everything after
// the first dot would put "BROWSER.LOCAL" on a line narrow enough that the
// label is the picture; the full name is in the key, one row per mode, so the
// short form only has to be recognisable.
func TestShortTransportNamesTheLastSegment(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"network.http", "HTTP"},
		{"config.env", "ENV"},
		{"process.exec", "EXEC"},
		{"storage.browser.local", "LOCAL"},
		{"storage.browser.session", "SESSION"},
		{"storage.browser.cookie", "COOKIE"},
		{"", "PORT"},
		{"weird", "WEIRD"},
	} {
		if got := shortTransport(tc.in); got != tc.want {
			t.Errorf("shortTransport(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
