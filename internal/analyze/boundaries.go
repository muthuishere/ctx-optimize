// Boundaries (ADR 2026-08-15-boundaries-verb): the system-context answer —
// what this codebase talks to across a boundary, and which of those partners
// live inside the same workspace.
//
// THE MODEL IS C4's system-context / container view, deliberately: CONSUMES is
// what this system calls out to, PROVIDES is what it exposes, and
// scope=internal marks a partner that lives inside this workspace. It is
// computed by JOIN at emit time — an identifier is internal iff some
// `provides` port in the same gather bears it — never guessed here.
//
// scope is PRESENT ONLY WHEN THAT JOIN HIT (ADR 2026-08-15-scope-join-broken).
// There is no `external` value: a miss means the two sides were different
// namespaces (a consumed HOST against a provided ROUTE PATH), which makes
// "external" undecidable rather than true. So absence reads as "not proven
// internal", and `--external` means "everything not proven internal".
//
// OPEN METADATA RIDES OpenTelemetry semantic conventions, which the port model
// already carries: otel.server.address, otel.http.route, otel.http.request.method.
// They pass through to JSON under their semconv names so a static boundary
// joins a runtime trace on the same key. We do not invent a second vocabulary
// for something semconv already names.
//
// Nothing here is a new fact. Every field is read from port nodes and their
// provides/consumes edges; if an answer is wrong the fix is a rule, not this
// file.
package analyze

import (
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// BoundarySite is one cited leg: the file that touches this boundary, the
// rule that found it, and at what confidence. Site is "file:LN" when the
// producer stamped one.
type BoundarySite struct {
	File       string `json:"file"`
	Site       string `json:"site,omitempty"`
	Rule       string `json:"rule,omitempty"`
	Confidence string `json:"confidence"`
	Module     string `json:"module,omitempty"`
}

// BoundaryEntry is one external name this system binds to, with every site
// that binds it rolled up. Tier is the STRONGEST confidence among its sites;
// MixedTiers says the sites disagree, because an INFERRED dep-tier claim and
// an EXTRACTED call-site claim are different claims and must stay visibly
// different (ADR 15 D2).
type BoundaryEntry struct {
	Identifier string            `json:"identifier"`
	Scope      string            `json:"scope,omitempty"` // "internal", or absent = not proven internal
	Tier       string            `json:"tier"`
	MixedTiers bool              `json:"mixed_tiers,omitempty"`
	Sensitive  bool              `json:"sensitive,omitempty"`
	Dynamic    bool              `json:"dynamic,omitempty"`
	Modules    []string          `json:"modules,omitempty"`
	Sites      int               `json:"sites"`
	Cite       string            `json:"cite,omitempty"` // first site, file:LN
	Otel       map[string]string `json:"otel,omitempty"` // semconv passthrough
}

// BoundaryGroup is one transport within one direction — network.http,
// config.env, process.exec — with the counts that make the summary readable
// before any entry is.
type BoundaryGroup struct {
	Transport string `json:"transport"`
	Total     int    `json:"total"`
	// Internal counts the ports whose identifier JOINED a `provides` port in
	// this workspace. There is deliberately no External counter: a port that
	// did not join is not thereby external (ADR 2026-08-15-scope-join-broken),
	// and `Total - Internal` is the honest reading — "not proven internal".
	Internal  int             `json:"internal,omitempty"`
	Sensitive int             `json:"sensitive,omitempty"`
	Dynamic   int             `json:"dynamic,omitempty"`
	Entries   []BoundaryEntry `json:"entries"`
	Withheld  int             `json:"withheld,omitempty"` // budgeted away; NEVER silent
}

// BoundaryReport is the whole surface, counts first.
type BoundaryReport struct {
	Ports        int             `json:"ports"`
	Modules      []string        `json:"modules,omitempty"`
	Consumes     []BoundaryGroup `json:"consumes"`
	Provides     []BoundaryGroup `json:"provides"`
	DynamicTotal int             `json:"dynamic_total,omitempty"`
	Truncated    bool            `json:"truncated,omitempty"`
}

// BoundaryOptions filters and budgets the report. Zero value = the default
// summary: every transport, budgeted.
type BoundaryOptions struct {
	Direction string // "" | provides | consumes
	Transport string // "" | exact transport, or a dotted prefix like "network"
	OnlyExt   bool
	OnlySens  bool
	All       bool // lift the per-group budget AND include dynamic entries
	PerGroup  int  // 0 → defaultPerGroup
}

// defaultPerGroup bounds each transport group the way query bounds hits (S1e):
// complete entries, hard cap, and the withheld count always stated. A repo
// with 84 env keys should answer "84 keys, 11 sensitive" and show the ones
// that matter, not print 84 lines.
const defaultPerGroup = 12

// tierRank orders confidence strongest-first so an entry reports its best
// evidence while MixedTiers records that the sites disagreed.
func tierRank(c string) int {
	switch c {
	case schema.Extracted:
		return 0
	case schema.Inferred:
		return 1
	case schema.Ambiguous:
		return 2
	}
	return 3
}

// moduleOf recovers the module a federated node id belongs to. Federated
// stores prefix ids with the module path (apps/api/port:config.env:>KEY);
// a single-module store has no prefix.
func moduleOf(id string) string {
	if i := strings.Index(id, "port:"); i > 0 {
		return strings.TrimSuffix(id[:i], "/")
	}
	return ""
}

type boundaryKey struct{ direction, transport, identifier string }

type boundaryAcc struct {
	scope     string
	sensitive bool
	dynamic   bool
	otel      map[string]string
	modules   map[string]bool
	sites     []BoundarySite
}

// Boundaries walks port nodes and their provides/consumes edges and returns
// the system-context summary. Federated stores carry the same logical port
// once per module, so grouping is by (direction, transport, identifier) —
// never by node id, which is module-prefixed.
func Boundaries(nodes []schema.Node, edges []schema.Edge, opt BoundaryOptions) *BoundaryReport {
	type portInfo struct {
		key   boundaryKey
		acc   *boundaryAcc
		valid bool
	}
	byID := map[string]portInfo{}
	accs := map[boundaryKey]*boundaryAcc{}
	mods := map[string]bool{}
	nPorts := 0

	for _, n := range nodes {
		if n.Kind != "port" {
			continue
		}
		nPorts++
		m := n.Metadata
		k := boundaryKey{m["direction"], m["transport"], m["identifier"]}
		a := accs[k]
		if a == nil {
			a = &boundaryAcc{modules: map[string]bool{}}
			accs[k] = a
		}
		if m["scope"] != "" {
			a.scope = m["scope"]
		}
		if m["sensitive"] == "true" {
			a.sensitive = true
		}
		if m["resolved"] == "dynamic" {
			a.dynamic = true
		}
		for mk, mv := range m {
			if strings.HasPrefix(mk, "otel.") {
				if a.otel == nil {
					a.otel = map[string]string{}
				}
				a.otel[mk] = mv
			}
		}
		if mod := moduleOf(n.ID); mod != "" {
			a.modules[mod] = true
			mods[mod] = true
		}
		byID[n.ID] = portInfo{k, a, true}
	}

	for _, e := range edges {
		if e.Relation != "provides" && e.Relation != "consumes" {
			continue
		}
		p, ok := byID[e.Target]
		if !ok {
			continue
		}
		p.acc.sites = append(p.acc.sites, BoundarySite{
			File: e.Source, Site: e.Metadata["site"], Rule: e.Metadata["rule"],
			Confidence: e.Confidence, Module: moduleOf(e.Target),
		})
	}

	// Fold accumulators into per-(direction,transport) groups.
	type groupKey struct{ direction, transport string }
	groups := map[groupKey]*BoundaryGroup{}
	dynamicTotal := 0

	for k, a := range accs {
		if a.dynamic {
			dynamicTotal++
		}
		if opt.Direction != "" && k.direction != opt.Direction {
			continue
		}
		if opt.Transport != "" && k.transport != opt.Transport &&
			!strings.HasPrefix(k.transport, opt.Transport+".") {
			continue
		}
		// --external is "everything not PROVEN internal". It cannot be
		// `scope == "external"`, because nothing emits that value any more.
		if opt.OnlyExt && a.scope == "internal" {
			continue
		}
		if opt.OnlySens && !a.sensitive {
			continue
		}
		gk := groupKey{k.direction, k.transport}
		g := groups[gk]
		if g == nil {
			g = &BoundaryGroup{Transport: k.transport}
			groups[gk] = g
		}
		g.Total++
		if a.scope == "internal" {
			g.Internal++
		}
		if a.sensitive {
			g.Sensitive++
		}
		if a.dynamic {
			g.Dynamic++
			if !opt.All {
				continue // summarised in the count; reachable with --all
			}
		}
		sortBoundarySites(a.sites)
		e := BoundaryEntry{
			Identifier: k.identifier, Scope: a.scope, Sensitive: a.sensitive,
			Dynamic: a.dynamic, Sites: len(a.sites), Otel: a.otel,
			Modules: sortedKeys(a.modules),
		}
		best, mixed := 3, false
		for _, s := range a.sites {
			r := tierRank(s.Confidence)
			if r != best && best != 3 {
				mixed = true
			}
			if r < best {
				best = r
			}
		}
		e.MixedTiers = mixed
		switch best {
		case 0:
			e.Tier = schema.Extracted
		case 1:
			e.Tier = schema.Inferred
		case 2:
			e.Tier = schema.Ambiguous
		}
		if len(a.sites) > 0 {
			e.Cite = a.sites[0].Site
			if e.Cite == "" {
				e.Cite = a.sites[0].File
			}
		}
		g.Entries = append(g.Entries, e)
	}

	r := &BoundaryReport{Ports: nPorts, Modules: sortedKeys(mods), DynamicTotal: dynamicTotal}
	per := opt.PerGroup
	if per <= 0 {
		per = defaultPerGroup
	}
	for gk, g := range groups {
		sortBoundaryEntries(g.Entries)
		if !opt.All && len(g.Entries) > per {
			g.Withheld = len(g.Entries) - per
			g.Entries = g.Entries[:per]
			r.Truncated = true
		}
		if gk.direction == "provides" {
			r.Provides = append(r.Provides, *g)
		} else {
			r.Consumes = append(r.Consumes, *g)
		}
	}
	sortBoundaryGroups(r.Consumes)
	sortBoundaryGroups(r.Provides)
	return r
}

// sortBoundaryEntries: the ones a reader needs first, deterministically.
// Sensitive leads (a secret is the thing you scan for), then not-proven-
// internal over internal (egress is the question people ask), then identifier.
func sortBoundaryEntries(es []BoundaryEntry) {
	sort.Slice(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.Sensitive != b.Sensitive {
			return a.Sensitive
		}
		if (a.Scope == "internal") != (b.Scope == "internal") {
			return b.Scope == "internal"
		}
		return a.Identifier < b.Identifier
	})
}

func sortBoundaryGroups(gs []BoundaryGroup) {
	sort.Slice(gs, func(i, j int) bool { return gs[i].Transport < gs[j].Transport })
}

func sortBoundarySites(s []BoundarySite) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].File != s[j].File {
			return s[i].File < s[j].File
		}
		return s[i].Site < s[j].Site
	})
}
