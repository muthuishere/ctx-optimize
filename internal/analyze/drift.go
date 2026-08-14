// Drift (ADR 2026-08-13 boundary-model-and-defaults, D6 — verb half): where
// provides, consumes and declared disagree on the boundary surface. A dead
// contract (provided, never consumed), an env key read but declared nowhere —
// the disagreements a review should see before they ship.
//
// The gate: only EXTRACTED×EXTRACTED evidence yields a FINDING; anything
// resting on an INFERRED or AMBIGUOUS leg is an OBSERVATION — listed, never
// accused. And absence of instrumentation is never evidence: env-undeclared
// fires only when the store proves declarations ARE tracked (at least one
// config.env provides port exists); otherwise a missing declaration means
// nobody taught the store about declarations, not that the code is wrong.
//
// Identifiers are compared through boundaries.Normalize — the SAME fold the
// producer applies at emit, so drift and the emitter can never disagree.
// Boundary-produced ports arrive pre-normalized (their would-joins became
// silent joins at emit); ports injected raw through the --json door still
// get folded here for comparison, surfacing as would-join observations.
package analyze

import (
	"sort"

	"github.com/muthuishere/ctx-optimize/internal/boundaries"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// DriftSite is one cited leg of evidence: which file touches the port, under
// which rule, at what confidence. Site is "file:LN" when the producer stamped
// one.
type DriftSite struct {
	File       string `json:"file"`
	Site       string `json:"site,omitempty"`
	Rule       string `json:"rule,omitempty"`
	Confidence string `json:"confidence"`
}

// DriftFinding is an accusation the gate allows: every load-bearing leg is
// EXTRACTED. Kind: dead-contract | env-undeclared.
type DriftFinding struct {
	Kind        string      `json:"kind"`
	Transport   string      `json:"transport"`
	Identifier  string      `json:"identifier"`
	Detail      string      `json:"detail"`
	Providers   []DriftSite `json:"providers,omitempty"`
	Consumers   []DriftSite `json:"consumers,omitempty"`
	Identifiers []string    `json:"identifiers,omitempty"` // raw spellings when they differ
}

// DriftObservation is listed, never accused: the same shapes as findings but
// with an INFERRED/AMBIGUOUS leg, plus would-join pairs the pending
// normalization slice will merge. Kind: possibly-consumed | lower-tier-orphan
// | would-join.
type DriftObservation = DriftFinding

// DriftReport is the whole surface: counts first, then findings (accusable),
// then observations (listed).
type DriftReport struct {
	Ports        int                `json:"ports"`
	Groups       int                `json:"groups"`
	Findings     []DriftFinding     `json:"findings"`
	Observations []DriftObservation `json:"observations"`
}

// driftGroup accumulates one (transport, normalized-identifier) contract.
type driftGroup struct {
	transport string
	raws      map[string]bool
	providers []DriftSite
	consumers []DriftSite
	provRaws  map[string]bool
	consRaws  map[string]bool
}

// Drift walks port nodes and their provides/consumes edges. Federated stores
// carry the same logical port once per module (ids are module-prefixed), so
// grouping is by metadata transport+identifier — never by node id.
func Drift(nodes []schema.Node, edges []schema.Edge) *DriftReport {
	ports := map[string]struct{ transport, ident string }{}
	envDeclared := false // do we have ANY declaration-tier env provides?
	nPorts := 0
	for _, n := range nodes {
		if n.Kind != "port" {
			continue
		}
		nPorts++
		t, id := n.Metadata["transport"], n.Metadata["identifier"]
		ports[n.ID] = struct{ transport, ident string }{t, id}
		if t == "config.env" && n.Metadata["direction"] == "provides" {
			envDeclared = true
		}
	}

	groups := map[string]*driftGroup{}
	grp := func(transport, raw string) *driftGroup {
		k := transport + "\x00" + boundaries.Normalize(transport, raw)
		g := groups[k]
		if g == nil {
			g = &driftGroup{transport: transport, raws: map[string]bool{},
				provRaws: map[string]bool{}, consRaws: map[string]bool{}}
			groups[k] = g
		}
		g.raws[raw] = true
		return g
	}
	for _, e := range edges {
		p, ok := ports[e.Target]
		if !ok || (e.Relation != "provides" && e.Relation != "consumes") {
			continue
		}
		g := grp(p.transport, p.ident)
		s := DriftSite{File: e.Source, Site: e.Metadata["site"],
			Rule: e.Metadata["rule"], Confidence: e.Confidence}
		if e.Relation == "provides" {
			g.providers = append(g.providers, s)
			g.provRaws[p.ident] = true
		} else {
			g.consumers = append(g.consumers, s)
			g.consRaws[p.ident] = true
		}
	}

	r := &DriftReport{Ports: nPorts, Groups: len(groups)}
	for _, g := range groups {
		sortSites(g.providers)
		sortSites(g.consumers)
		ident := firstKey(g.raws)
		base := DriftFinding{Transport: g.transport, Identifier: ident,
			Providers: g.providers, Consumers: g.consumers}
		if len(g.raws) > 1 {
			base.Identifiers = sortedKeys(g.raws)
		}
		switch {
		case len(g.providers) > 0 && len(g.consumers) == 0:
			f := base
			f.Kind = "dead-contract"
			f.Detail = "provided, never consumed anywhere in the store"
			if anyExtracted(g.providers) {
				r.Findings = append(r.Findings, f)
			} else {
				f.Kind = "lower-tier-orphan"
				f.Detail = "provided (INFERRED/AMBIGUOUS only), never consumed — listed, not accused"
				r.Observations = append(r.Observations, f)
			}
		case len(g.providers) > 0 && len(g.consumers) > 0 && !anyExtracted(g.consumers):
			f := base
			f.Kind = "possibly-consumed"
			f.Detail = "every consumer leg is INFERRED/AMBIGUOUS — not accusable as dead, not provably alive"
			r.Observations = append(r.Observations, f)
		case g.transport == "config.env" && envDeclared &&
			len(g.providers) == 0 && anyExtracted(g.consumers):
			f := base
			f.Kind = "env-undeclared"
			f.Detail = "read in code (EXTRACTED) but declared nowhere the store can see"
			r.Findings = append(r.Findings, f)
		}
		// would-join: provider and consumer spell the identifier differently
		// but normalize equal — visible now, silent once emit-time
		// normalization (the deferred D6 half) lands.
		if len(g.provRaws) > 0 && len(g.consRaws) > 0 && !sameKeys(g.provRaws, g.consRaws) {
			f := base
			f.Kind = "would-join"
			f.Detail = "provides and consumes spell this identifier differently; they join only through normalization"
			f.Identifiers = sortedKeys(g.raws)
			r.Observations = append(r.Observations, f)
		}
	}
	sortFindings(r.Findings)
	sortFindings(r.Observations)
	return r
}

func anyExtracted(sites []DriftSite) bool {
	for _, s := range sites {
		if s.Confidence == schema.Extracted {
			return true
		}
	}
	return false
}

func sortSites(s []DriftSite) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].File != s[j].File {
			return s[i].File < s[j].File
		}
		return s[i].Site < s[j].Site
	})
}

func sortFindings(f []DriftFinding) {
	sort.Slice(f, func(i, j int) bool {
		if f[i].Kind != f[j].Kind {
			return f[i].Kind < f[j].Kind
		}
		if f[i].Transport != f[j].Transport {
			return f[i].Transport < f[j].Transport
		}
		return f[i].Identifier < f[j].Identifier
	})
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func firstKey(m map[string]bool) string {
	ks := sortedKeys(m)
	if len(ks) == 0 {
		return ""
	}
	return ks[0]
}

func sameKeys(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
