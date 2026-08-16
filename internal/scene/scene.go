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
	ID    string `json:"id"`    // the directory path, the stable key
	Label string `json:"label"` // last path segment (the display name)
	Dir   string `json:"dir"`   // full directory, shown as the card's subtitle
	Files int    `json:"files"` // file nodes under it
	Decls int    `json:"decls"` // declaration nodes under it
	In    int    `json:"in"`    // lifted edges arriving (how depended-upon)
	Out   int    `json:"out"`   // lifted edges leaving (how dependent)
	// ExtIn/ExtOut are real edges whose OTHER END is outside this scene — the
	// callers a function has elsewhere in the repo, the things a file reaches
	// beyond its directory. They are never drawn as arrows (the other end is
	// not on screen) but they are usually the number that says whether a card
	// matters, so they are printed.
	ExtIn  int    `json:"ext_in"`
	ExtOut int    `json:"ext_out"`
	Layer  int    `json:"layer"`  // longest-path depth in the lifted DAG
	Row    int    `json:"row"`    // slot within the layer
	Detail string `json:"detail"` // top declarations by degree, " · " joined
	Glyph  string `json:"glyph"`  // derived from which transports it touches
	Hub    bool   `json:"hub"`    // the most depended-upon subsystem
	// EnterGrain is the grain a card opens AT. Almost always "" (infer from the
	// directory), but the card standing for a root's OWN files has to say
	// "file" — inference would look at the same root, see subdirectories, and
	// hand back the scene you are already on.
	EnterGrain string `json:"enter_grain"`
	// Children is how many directories sit strictly under this card. It is the
	// drill-down affordance: a card with 0 is a leaf and must not invite a click
	// that would land on an empty scene.
	Children int `json:"children"`
	// Inner is how many edges would be DRAWN one level inside this card. It is
	// the difference between "there are things in here" and "there is something
	// to see in here": include/net/sctp/structs.h holds 11 declarations and not
	// one edge between them, because it is a header — it offered a green link
	// to a screen that could only say "nothing to draw".
	Inner int `json:"inner"`
	// Top is the single highest-degree declaration in this directory — a real
	// symbol name, which is what a question or a `card` command needs to be
	// worth pasting.
	Top string `json:"top"`
}

// Question is something worth ASKING about this scene, paired with the verb
// that answers it. Both halves are derived from the drawn facts: the names in
// the text are the names on screen, and the command is one this binary
// actually has. A suggested question that does not run, or that asks about a
// symbol this repo does not contain, would be worse than no suggestion.
type Question struct {
	Text    string `json:"text"`
	Command string `json:"command"`
}

// Crumb is one step of the drill-down trail. Root is what to pass back as
// Options.Root to return to that level, so the client never does path
// arithmetic on its own.
type Crumb struct {
	Label string `json:"label"`
	Root  string `json:"root"`
	// Module, when set, means this crumb leaves the current store entirely —
	// it is the REPO above a module, and clicking it changes which graph you
	// are looking at rather than which directory of this one. Empty on every
	// crumb that is a directory of the store already open.
	Module string `json:"module,omitempty"`
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
	// Transport is the KIND of boundary a port-derived link came through
	// (network.http, config.env, process.exec …). It is what lets the picture
	// colour a line by what it actually is, and it is the same value the outer
	// world groups carry, so the two halves of the scene agree. Empty on links
	// derived from code or manifests, which have no transport.
	Transport string `json:"transport,omitempty"`
	// Detail NAMES what the arrow stands for, when a count alone would leave
	// the reader guessing. "SHARES 12" between a UI and an API reads as "the UI
	// calls the API"; it is in fact twelve THIRD PARTIES both of them call, and
	// only the names say so. Empty where the relation is self-explanatory.
	Detail string `json:"detail,omitempty"`
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
	// Openers names the subsystems that actually open this group, and
	// OpenerTotal how many there are. On a big repo almost no plate has an
	// arrow: linux's 25 config.env ports are opened from 9 directories and not
	// one of them is among the seven drawn, so the plate floats and reads as a
	// broken link. Naming them puts the other end of the arrow on screen even
	// when the card cannot be.
	Openers     []string `json:"openers,omitempty"`
	OpenerTotal int      `json:"opener_total,omitempty"`
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
	Root            string   `json:"root"`  // what this scene is scoped to ("" = whole repo)
	Level           string   `json:"level"` // what a card stands for: directory | file | declaration
	Crumbs          []Crumb  `json:"crumbs"`
	// Inside is what this level HOLDS, for the case where it holds things but
	// draws nothing — a directory whose subdirectories never reference each
	// other. Saying "holds 2 subdirectories" and offering no way to reach them
	// makes the reader back out and hunt; these are the way in.
	Inside    []Crumb    `json:"inside"`
	Questions []Question `json:"questions"`
	Empty     string     `json:"empty,omitempty"`
	// Redirect names the store the reader should be looking at instead. A level
	// with exactly one card is a chooser wearing a diagram — this ADR's own kill
	// criterion — so rather than draw it, the scene says where the content is
	// and the viewer moves the address there. Empty in every other case.
	Redirect string `json:"redirect,omitempty"`
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
	// Root scopes the scene to one directory: only nodes at or under it are
	// considered, and a card becomes a CHILD directory of Root rather than a
	// full path. Empty means the whole repo.
	//
	// Drilling re-derives rather than filtering the parent scene, and that is the
	// point: inside src/aiteam the layering, the hub and the ranking are all
	// recomputed from the edges that exist THERE. A subsystem that looked like a
	// leaf from the top can be the hub of its own level, and a filtered view
	// could never show that.
	Root string
	// Grain forces the level instead of inferring it from Root. It exists for
	// one case that inference cannot express: a directory that has BOTH
	// subdirectories and files of its own. Scoped to it you get its
	// subdirectories, and its own files — often the bulk of the code, and on
	// drivers/base/firmware_loader the ten files the hub card stands for — had
	// no way to be opened at all. "" means infer.
	Grain string
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

// Level is what a card stands for at the current root. A leaf DIRECTORY is not
// a leaf: mm/kasan has no subdirectories and 17 files, 330 functions and 361
// real `calls` edges inside it (ADR 21). So the drill keeps going — the unit
// becomes the file, and then the declaration — and everything downstream
// (lifting, ranking, layering, hub) is unchanged, because all of it keys off
// `owner` and only the grain of `owner` moves.
type Level int

const (
	LevelDir  Level = iota // cards are directories
	LevelFile              // cards are files inside one directory
	LevelDecl              // cards are declarations inside one file
)

func (l Level) String() string {
	switch l {
	case LevelFile:
		return "file"
	case LevelDecl:
		return "declaration"
	}
	return "directory"
}

// levelFor decides the grain from what `root` NAMES: nothing or a directory
// with subdirectories keeps directory grain; a directory with none descends to
// its files; a file descends to its declarations.
func levelFor(root, grain string, dirs, files map[string]bool) Level {
	switch grain {
	case "file":
		return LevelFile
	case "decl":
		return LevelDecl
	}
	if root == "" {
		return LevelDir
	}
	if files[root] {
		return LevelDecl
	}
	for d := range dirs {
		if d != root && strings.HasPrefix(d, root+"/") {
			return LevelDir
		}
	}
	if dirs[root] {
		return LevelFile
	}
	return LevelDir // unknown root: the empty-scene path reports it
}

// unitLabel is what a card is CALLED at this grain: the directory or file's
// last path segment, or a declaration's own name.
func unitLabel(unit string, level Level, byID map[string]schema.Node) string {
	if level == LevelDecl {
		if n, ok := byID[unit]; ok && n.Label != "" {
			return n.Label
		}
	}
	return path.Base(unit)
}

// unitDir is the card's subtitle: where the thing lives.
func unitDir(unit string, level Level, byID map[string]schema.Node) string {
	if level == LevelDecl {
		if n, ok := byID[unit]; ok {
			d := srcPath(n.Source)
			if n.Location != "" {
				return d + " " + n.Location
			}
			return d
		}
	}
	return unit
}

// srcPath normalises a node source to forward slashes.
func srcPath(s string) string { return strings.ReplaceAll(s, "\\", "/") }

// unitOf maps a node to the card it belongs to at this grain, or "" when it
// takes no part in the scene.
func unitOf(n schema.Node, root string, level Level) string {
	src := srcPath(n.Source)
	switch level {
	case LevelFile:
		// files sitting directly in root; the file node and everything declared
		// in it both belong to the file
		if src == "" || path.Dir(src) != root {
			return ""
		}
		return src
	case LevelDecl:
		// one card per declaration in the file
		if src != root || !declKinds[n.Kind] {
			return ""
		}
		return n.ID
	default:
		full := subsystemOf(n)
		if full == "" {
			return ""
		}
		return scopeTo(root, full)
	}
}

// scopeTo collapses a node's full directory to the subsystem key for a scene
// rooted at root. At the top level (root == "") the subsystem IS the full
// directory, exactly as before. Inside a root it is the next path segment down,
// so drilling into src/aiteam yields src/aiteam/api, src/aiteam/storage, … and
// files sitting directly in src/aiteam collect under src/aiteam itself.
// A directory outside root returns "" and takes no part in the scene.
func scopeTo(root, dir string) string {
	if root == "" {
		return dir
	}
	if dir == root {
		return root
	}
	if !strings.HasPrefix(dir, root+"/") {
		return ""
	}
	rest := dir[len(root)+1:]
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	return root + "/" + rest
}

// questionsFor turns the drawn scene into things worth asking an agent, each
// paired with the verb that answers it. Every name used is a name on screen and
// every command is one this binary has — a suggestion that does not run is
// worse than no suggestion.
//
// The order is by how much the scene says about it: the hub first (it is the
// thing most code depends on), then the heaviest single relation, then the
// outer world, which is the part of a codebase people can least afford to
// guess at.
func questionsFor(sc Scene, ports map[string]schema.Node) []Question {
	var qs []Question
	add := func(text, cmd string) {
		if text == "" || cmd == "" {
			return
		}
		qs = append(qs, Question{Text: text, Command: cmd})
	}

	var hub *Card
	for i := range sc.Cards {
		if sc.Cards[i].Hub {
			hub = &sc.Cards[i]
		}
	}
	if hub != nil && hub.Top != "" {
		add("What breaks if I change `"+hub.Top+"`? It is in the most depended-upon directory here ("+
			itoa(hub.In)+" edges in).",
			"ctx-optimize change-plan \""+hub.Top+"\"")
		add("Who calls into `"+hub.Label+"` and why does everything depend on it?",
			"ctx-optimize affected \""+hub.Top+"\"")
	}

	// the heaviest drawn relation between two cards
	best := -1
	for i, l := range sc.Links {
		if strings.HasPrefix(l.To, "world:") {
			continue
		}
		if best < 0 || l.Weight > sc.Links[best].Weight {
			best = i
		}
	}
	if best >= 0 {
		l := sc.Links[best]
		from, to := cardOf(sc, l.From), cardOf(sc, l.To)
		if from != nil && to != nil && from.Top != "" && to.Top != "" {
			add("`"+from.Label+"` "+strings.ToLower(l.Relation)+" `"+to.Label+"` "+itoa(l.Weight)+
				" times — what is that coupling actually doing?",
				"ctx-optimize path \""+from.Top+"\" \""+to.Top+"\"")
		}
	}

	if len(ports) > 0 {
		add("What does this codebase call out to, and what does it expose?",
			"ctx-optimize boundaries")
		sensitive := 0
		for _, p := range ports {
			if p.Metadata["sensitive"] == "true" {
				sensitive++
			}
		}
		if sensitive > 0 {
			add("Which "+itoa(sensitive)+" of these are secrets, and where is each one read?",
				"ctx-optimize boundaries --sensitive")
		}
		// the busiest transport gets its own question, named
		bestT, bestN := "", 0
		for _, w := range sc.World {
			if w.Total > bestN {
				bestT, bestN = w.Transport, w.Total
			}
		}
		if bestT != "" {
			add("Show me all "+itoa(bestN)+" `"+bestT+"` boundaries with a file:line for each.",
				"ctx-optimize boundaries --transport "+bestT)
		}
	}

	// the biggest source of dependency: what it leans on
	var src *Card
	for i := range sc.Cards {
		if sc.Cards[i].Out > 0 && (src == nil || sc.Cards[i].Out > src.Out) {
			src = &sc.Cards[i]
		}
	}
	if src != nil && src.Top != "" && (hub == nil || src.ID != hub.ID) {
		add("`"+src.Label+"` depends on more than anything else here ("+itoa(src.Out)+
			" edges out). What is it pulling in?",
			"ctx-optimize card \""+src.Top+"\"")
	}
	return qs
}

func cardOf(sc Scene, id string) *Card {
	for i := range sc.Cards {
		if sc.Cards[i].ID == id {
			return &sc.Cards[i]
		}
	}
	return nil
}

// topDecl is the single highest-degree declaration, already sorted by caller.
//
// It returns the label VERBATIM. detailOf strips the qualifier before the last
// dot because a card has ~200px and `Repo.Get` reads better as `Get` — but that
// is a DISPLAY choice, and Top is not for display: it is pasted into `card` /
// `change-plan` / `path`, which resolve against the stored label. Reusing the
// stripped form here produced `ctx-optimize change-plan "kfree"` for the linux
// kernel, where the node is `kfree_via_page.kunit` and nothing resolves — a
// suggested command that cannot run, which is worse than no suggestion.
func topDecl(list []schema.Node) string {
	for _, n := range list {
		if n.Label != "" {
			return n.Label
		}
	}
	return ""
}

// commonDir is the deepest directory that contains both paths.
func commonDir(a, b string) string {
	if a == b {
		return a
	}
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	if n == 0 {
		return ""
	}
	return strings.Join(as[:n], "/")
}

// crumbsFor builds the trail back out. The first crumb is always the whole
// repo, so there is no level you can drill into and not be able to leave.
func crumbsFor(title, root string) []Crumb {
	out := []Crumb{{Label: title, Root: ""}}
	if root == "" {
		return out
	}
	acc := ""
	for _, seg := range strings.Split(root, "/") {
		if seg == "" {
			continue
		}
		if acc == "" {
			acc = seg
		} else {
			acc += "/" + seg
		}
		out = append(out, Crumb{Label: seg, Root: acc})
	}
	return out
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
	opt.Root = strings.Trim(strings.ReplaceAll(opt.Root, "\\", "/"), "/")
	sc := Scene{
		Module:     module,
		Title:      titleOf(module),
		TotalNodes: len(nodes),
		TotalEdges: len(edges),
		Root:       opt.Root,
		Crumbs:     crumbsFor(titleOf(module), opt.Root),
	}

	// ---- 0. what exists, so the grain can be chosen before anything is bucketed.
	allDirs := map[string]bool{} // every real directory, at full depth
	allFiles := map[string]bool{}
	for _, n := range nodes {
		if n.Kind == "port" {
			continue
		}
		if d := subsystemOf(n); d != "" {
			allDirs[d] = true
		}
		if n.Kind == "file" && n.Source != "" {
			allFiles[srcPath(n.Source)] = true
		}
	}
	level := levelFor(opt.Root, opt.Grain, allDirs, allFiles)
	sc.Level = level.String()

	// ---- 1. every node to its unit, and node degree for detail lines.
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
		d := unitOf(n, opt.Root, level)
		if d == "" {
			continue // outside the drilled root, or not a unit at this grain
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

	// ---- 1b. How much there is to SEE one level down, per unit.
	//
	// An edge between two units is drawn at exactly ONE level: the lowest common
	// ancestor of its endpoints. Above that they collapse into the same card and
	// it is internal; below it, one endpoint is outside the scene. So a single
	// pass keyed on the LCA gives every directory and every file its inner edge
	// count, instead of deriving a child scene per card.
	inner := map[string]int{}
	for _, e := range edges {
		if _, ok := liftedRelations[e.Relation]; !ok || e.Confidence == schema.Ambiguous {
			continue
		}
		a, ok1 := byID[e.Source]
		b, ok2 := byID[e.Target]
		if !ok1 || !ok2 {
			continue
		}
		fa, fb := srcPath(a.Source), srcPath(b.Source)
		if fa == "" || fb == "" {
			continue
		}
		if fa == fb {
			inner[fa]++ // two declarations in one file: drawn at file level
			continue
		}
		inner[commonDir(path.Dir(fa), path.Dir(fb))]++
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
	// Edges that CROSS the scope boundary. Inside mm/kasan/common.c,
	// check_page_allocation shows 3 callers — but that counts only the callers
	// in this file. It may be called from a hundred places in the kernel, and
	// that is the number that says whether it is load-bearing.
	//
	// They cannot be drawn as arrows, because the other end is not on screen,
	// and drawing an arrow to nothing is exactly the sin the world view was
	// killed for. So they are COUNTED and printed on the card instead: real
	// edges, honestly labelled as leaving the picture.
	extIn := map[string]int{}
	extOut := map[string]int{}
	for _, e := range edges {
		rel, ok := liftedRelations[e.Relation]
		if !ok || e.Confidence == schema.Ambiguous {
			continue
		}
		a, b := owner[e.Source], owner[e.Target]
		switch {
		case a == "" && b == "":
			continue // neither end is in this scene
		case drop(a) || drop(b):
			continue // excluded tree; the notes already say it is excluded
		case a == "":
			extIn[b]++ // arrives from outside the scope
		case b == "":
			extOut[a]++ // leaves the scope
		case a == b:
			continue // internal to one card: not a relationship between cards
		default:
			lifted[lk{a, b, rel}] += 1
		}
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
		// A subsystem with no cross-directory edge is still a subsystem. It used
		// to be dropped here — "nothing to draw it with" — which was true only
		// while a scene was a flow chart: with no arrow, a card had no column to
		// stand in. clis/go/brain holds `brain` and `skill`, neither importing
		// the other, and the whole level collapsed to a sentence saying so with
		// the two of them reduced to pill links underneath.
		//
		// The layout can place a SET now (clustered, no direction claimed), so
		// the cards are drawn and the arrows are simply absent. Edged subsystems
		// still rank first, so nothing that had a place loses it.
		pool = append(pool, ranked{d, a})
	}
	sort.Slice(pool, func(i, j int) bool {
		di, dj := pool[i].a.in+pool[i].a.out, pool[j].a.in+pool[j].a.out
		if di != dj {
			return di > dj
		}
		// Among subsystems with no edges at all, the bigger one is the more
		// likely to be worth looking at.
		if pool[i].a.files+pool[i].a.decls != pool[j].a.files+pool[j].a.decls {
			return pool[i].a.files+pool[i].a.decls > pool[j].a.files+pool[j].a.decls
		}
		return pool[i].dir < pool[j].dir
	})
	// A level with subsystems but no edges between them is NOT empty. It used
	// to be reported as empty and the cards thrown away, because a scene was a
	// flow chart and a card with no arrow had no column to stand in — the
	// reader drilled into clis/go/brain, which holds `brain` and `skill`, and
	// got a sentence explaining that neither imports the other while the two of
	// them were reduced to pill links underneath it.
	//
	// The layout places a SET now, and says on screen that x no longer carries
	// direction. So the cards are drawn, the arrows are simply absent, and
	// Empty is kept for what it was always for: a root that names nothing, and
	// a level that really does hold nothing.
	if len(pool) == 0 {
		if opt.Root != "" && len(owner) == 0 {
			// The root matched nothing at all. Distinguishing this from "a real
			// leaf" matters: one is a typo, the other is the truth about the code.
			sc.Empty = "no directory `" + opt.Root + "` in this store."
		} else if opt.Root != "" {
			// Drilling found a real directory with nothing to draw INSIDE it. Say
			// which, and leave the crumbs intact so the trail out still works —
			// a dead end you cannot back out of is worse than no drill-down.
			// The wording follows the GRAIN. At file grain it said "across its
			// own subdirectories" while the header said every card is a file,
			// which describes a level the reader is not on.
			// Say what IS here, not only what is missing. A bare "nothing to
			// draw" leaves the reader unable to tell a real leaf from a broken
			// view; the count does that in one line.
			what := "subdirectories"
			switch level {
			case LevelFile:
				what = "files"
			case LevelDecl:
				what = "declarations"
			}
			// what IS here, as somewhere to go
			names := make([]string, 0, len(subs))
			for d := range subs {
				names = append(names, d)
			}
			sort.Strings(names)
			for i, d := range names {
				if i >= 12 {
					break
				}
				label := path.Base(d)
				if level == LevelDecl {
					if n, ok := byID[d]; ok && n.Label != "" {
						label = n.Label
					}
				}
				sc.Inside = append(sc.Inside, Crumb{Label: label, Root: d})
			}
			held := len(subs)
			sc.Empty = "`" + opt.Root + "` holds " + itoa(held) + " " + what +
				", and none of them call or import each other — so there is no flow to draw at this level. " +
				"Nothing is missing; use the trail above to go back up."
			if held == 0 {
				sc.Empty = "`" + opt.Root + "` has no " + what + " in the store."
			}
		} else {
			sc.Empty = "no subsystem in this store has a cross-directory `imports` or `calls` edge — " +
				"there is no flow to draw. Try the graph viewer."
		}
		return sc.finish()
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

	// Children is "is there another level inside this card", counted from what
	// EXISTS rather than from what is drawn beside it: a card is enterable
	// because the code has structure under it, not because that structure
	// happened to rank high enough to appear.
	//
	// At directory grain that is immediate subdirectories, and a directory with
	// none is NOT a leaf — it opens onto its files (ADR 21). At file grain it is
	// the declarations in the file. A declaration is the floor.
	kids := map[string]int{}
	selfGrain := map[string]string{}
	switch level {
	case LevelDir:
		for _, r := range chosen {
			if r.dir == opt.Root {
				// The card for files directly inside the current root. Entering
				// it at directory grain would re-derive this very scene, so it
				// opens at FILE grain instead — which is what it stands for.
				for f := range allFiles {
					if path.Dir(f) == r.dir {
						kids[r.dir]++
					}
				}
				selfGrain[r.dir] = "file"
				continue
			}
			seen := map[string]bool{}
			for d := range allDirs {
				if child := scopeTo(r.dir, d); child != "" && child != r.dir {
					seen[child] = true
				}
			}
			if len(seen) > 0 {
				kids[r.dir] = len(seen)
				continue
			}
			// no subdirectories — count the files, which is the next level down
			for f := range allFiles {
				if path.Dir(f) == r.dir {
					kids[r.dir]++
				}
			}
		}
	case LevelFile:
		for _, n := range nodes {
			if declKinds[n.Kind] && srcPath(n.Source) != "" {
				kids[srcPath(n.Source)]++
			}
		}
	}

	row := rowOrder(chosen, links, layer)
	for _, r := range chosen {
		sc.Cards = append(sc.Cards, Card{
			ID: r.dir, Label: unitLabel(r.dir, level, byID), Dir: unitDir(r.dir, level, byID),
			Files: r.a.files, Decls: r.a.decls,
			In: r.a.in, Out: r.a.out,
			ExtIn: extIn[r.dir], ExtOut: extOut[r.dir],
			Layer: layer[r.dir], Row: row[r.dir],
			Detail:     detailOf(top[r.dir]),
			Glyph:      glyphOf(touch[r.dir]),
			Hub:        r.dir == hub,
			Children:   kids[r.dir],
			Inner:      inner[r.dir],
			EnterGrain: selfGrain[r.dir],
			Top:        topDecl(top[r.dir]),
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

	// A transport plate with no arrow to any card is the norm on a big repo,
	// not a bug: on linux the 25 config.env ports are opened from 122
	// directories and not one of them is among the seven drawn. But an
	// unconnected plate LOOKS like a broken link, so it says which it is —
	// counting the directories that really do open it, so the reader knows the
	// ports are attached to something even though the something is off screen.
	drawnTo := map[string]bool{}
	for _, l := range wlinks {
		drawnTo[l.To] = true
	}
	openers := map[string]map[string]bool{}
	for k := range pwAll {
		if openers[k.to] == nil {
			openers[k.to] = map[string]bool{}
		}
		openers[k.to][k.from] = true
	}
	var orphan []string
	for i, w := range sc.World {
		owners := openers["world:"+w.Transport]
		if len(owners) > 0 {
			names := make([]string, 0, len(owners))
			for d := range owners {
				names = append(names, d)
			}
			// By weight would be better, but the count per directory is not
			// kept; alphabetical is at least STABLE, which a picture that is
			// meant to be diffable needs more than it needs a ranking.
			sort.Strings(names)
			sc.World[i].OpenerTotal = len(names)
			if len(names) > 3 {
				names = names[:3]
			}
			sc.World[i].Openers = names
		}
		if drawnTo["world:"+w.Transport] {
			continue
		}
		orphan = append(orphan, w.Transport+" ("+itoa(len(owners))+" directories)")
	}
	if len(orphan) > 0 {
		sort.Strings(orphan)
		sc.Notes = append(sc.Notes,
			"no arrow reaches "+strings.Join(orphan, ", ")+
				" — those ports are opened from directories that are not among the ones drawn. "+
				"The ports are real; the arrow is not drawn because its other end is off screen")
	}

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

	scope := "directories"
	switch {
	case level == LevelDecl:
		scope = "declarations in " + opt.Root
	case level == LevelFile:
		scope = "files in " + opt.Root
	case opt.Root != "":
		scope = "subdirectories of " + opt.Root
	}
	sc.Notes = append(sc.Notes,
		"top "+itoa(sc.SubsystemsShown)+" of "+itoa(sc.SubsystemsTotal)+
			" "+scope+" by cross-directory edge weight — this is a SAMPLE, not the whole graph")
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
	// A store with no `port` nodes draws no outer world. Say so, rather than
	// leaving the bottom band silently blank — an absent band and an absent
	// FEATURE look identical on screen, and only one of them is the user's
	// business to fix.
	if len(ports) == 0 {
		sc.Notes = append(sc.Notes,
			"no boundaries recorded for this store — run `ctx-optimize boundaries` to draw the outer world")
	}
	// Leaves are a FACT about the code, not a missing feature. Without this a
	// level where nothing can be entered looks identical to one where the
	// drill-down is broken — which is exactly how it read on linux/mm, where
	// all four subsystems are genuinely leaf directories.
	leaves := 0
	for _, c := range sc.Cards {
		if c.Children == 0 {
			leaves++
		}
	}
	if leaves == len(sc.Cards) && len(sc.Cards) > 0 {
		if level == LevelDecl {
			sc.Notes = append(sc.Notes,
				"these are declarations inside one file — this is the floor, there is nothing further to open")
		} else {
			sc.Notes = append(sc.Notes,
				"nothing here has another level inside it — this is as deep as the store goes")
		}
	}
	if level != LevelDir {
		sc.Notes = append(sc.Notes,
			"a card here is a "+level.String()+", not a directory; arrows are real edges between "+
				level.String()+"s")
	}
	// What "outside" means depends on the grain, and a number whose meaning the
	// reader has to guess is not a fact. Say it.
	ext := 0
	for _, c := range sc.Cards {
		ext += c.ExtIn + c.ExtOut
	}
	if ext > 0 {
		where := "outside `" + opt.Root + "`"
		if opt.Root == "" {
			where = "outside this repo's own code (external modules and dependencies)"
		}
		sc.Notes = append(sc.Notes,
			itoa(ext)+" edges on these cards reach "+where+
				" — counted on the card, never drawn, because the other end is not on screen")
	}
	sc.Questions = questionsFor(sc, ports)
	return sc.finish()
}

// finish normalises the wire shape. Every slice field on Scene is typed as an
// ARRAY by the client; a nil slice marshals to `null`, and `for (… of null)`
// throws and blanks the entire viewer. That is exactly what a store with no
// ports did to Chips — one empty field took down a screen that had seven cards
// and twenty-one links to draw. So `null` is not a value this contract has.
func (s Scene) finish() Scene {
	if s.Cards == nil {
		s.Cards = []Card{}
	}
	if s.Links == nil {
		s.Links = []Link{}
	}
	if s.World == nil {
		s.World = []World{}
	}
	if s.Stats == nil {
		s.Stats = []Stat{}
	}
	if s.Chips == nil {
		s.Chips = []string{}
	}
	if s.Notes == nil {
		s.Notes = []string{}
	}
	if s.Crumbs == nil {
		s.Crumbs = []Crumb{}
	}
	if s.Questions == nil {
		s.Questions = []Question{}
	}
	if s.Inside == nil {
		s.Inside = []Crumb{}
	}
	return s
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
	case t["consumes:storage.browser"] || t["provides:storage.browser"]:
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
