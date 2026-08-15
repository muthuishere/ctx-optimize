package golden

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

// TestGoldenBoundaryRepo is the regression net for the boundary lane (ADR
// 2026-08-15-question-classes D4).
//
// WHY IT EXISTS: the lane had unit tests and no SCORED fact anywhere, so a rule
// that stopped matching, a services entry that vanished, or a tier that quietly
// downgraded moved no number at all. That is the same shape as the perf gate
// that recorded its own measurement and could never fail. A capability without
// a failing test is a promise.
//
// PER-CLASS, NOT BLENDED (D4): each boundary class asserts separately, so a
// failure names the class that broke — "boundary/process" rather than a diff
// of 40 lines. Deliberately NOT folded into the 20-question judged sets, whose
// 16.5/13.0 floors measure code-locate and must not move for a boundary reason.
//
// HERMETIC: runs in the normal `task golden` tier with no corpus clone. linux
// has no HTTP egress and Newtonsoft has no routes, so neither could host these
// classes; the corpus tier measures BREADTH separately without being the gate.
func TestGoldenBoundaryRepo(t *testing.T) {
	repo := t.TempDir()
	copyTree(t, filepath.Join("testdata", "repos", "boundary"), repo)
	storeRoot := t.TempDir()

	gatherWithin(t, 3*time.Second, repo, storeRoot)

	ports := boundaryPorts(t, storeRoot)
	edges := boundaryEdges(t, storeRoot)

	// ---- class: boundary/config ------------------------------------------
	// Two env reads that must be told apart. SERVICE_TIER exists precisely to
	// prove the sensitive flag DISCRIMINATES: a rule that flags everything is
	// as broken as one that flags nothing, and only a negative case catches it.
	classPort(t, ports, "boundary/config", "port:config.env:>PAYMENTS_API_KEY", portFact{
		Direction: "consumes", Transport: "config.env",
		Identifier: "PAYMENTS_API_KEY", Scope: "external", Sensitive: "true",
	})
	classPort(t, ports, "boundary/config", "port:config.env:>OPENAI_API_KEY", portFact{
		Direction: "consumes", Transport: "config.env",
		Identifier: "OPENAI_API_KEY", Scope: "external", Sensitive: "true",
	})
	classPort(t, ports, "boundary/config", "port:config.env:>SERVICE_TIER", portFact{
		Direction: "consumes", Transport: "config.env",
		Identifier: "SERVICE_TIER", Scope: "external", Sensitive: "",
	})

	// ---- class: boundary/egress ------------------------------------------
	// A literal host, and an SDK host reached with no host literal anywhere in
	// the file — the case only the services registry can see.
	classPort(t, ports, "boundary/egress", "port:network.http:>api.weather.example", portFact{
		Direction: "consumes", Transport: "network.http",
		Identifier: "api.weather.example", Scope: "external",
	})
	classPort(t, ports, "boundary/egress", "port:network.http:>api.openai.com", portFact{
		Direction: "consumes", Transport: "network.http",
		Identifier: "api.openai.com", Scope: "external",
	})

	// ---- class: boundary/process -----------------------------------------
	classPort(t, ports, "boundary/process", "port:process.exec:>git", portFact{
		Direction: "consumes", Transport: "process.exec",
		Identifier: "git", Scope: "external",
	})

	// ---- class: boundary/storage -----------------------------------------
	classPort(t, ports, "boundary/storage", "port:storage.local:>session_token", portFact{
		Direction: "consumes", Transport: "storage.local",
		Identifier: "session_token", Scope: "external",
	})

	// ---- class: api-surface ----------------------------------------------
	// Routes are PROVIDES — the direction that makes scope-by-join possible.
	for _, route := range []string{"/healthz", "/orders", "/status", "/upload"} {
		classPort(t, ports, "api-surface", "port:network.http:<"+route, portFact{
			Direction: "provides", Transport: "network.http", Identifier: route,
		})
	}

	// The HTTP METHOD lives on the AST route node, not on the port (the port
	// keys on path so provider and consumer can join). Both halves are pinned
	// because "what routes does it expose, and with which methods" needs both.
	snap := snapshot(t, storeRoot)
	mustContain(t, snap, "api-surface: express GET carries its method",
		"N web/app.ts::route:GET /status | route | web/app.ts | L5-L5")
	mustContain(t, snap, "api-surface: express POST carries its method",
		"N web/app.ts::route:POST /upload | route | web/app.ts | L6-L6")

	// ---- class: tier honesty ---------------------------------------------
	// The tier IS the claim, so it is gated like any other fact. A silent
	// upgrade (AMBIGUOUS -> INFERRED) would be a lie the graph tells with
	// confidence, which is worse than a miss.
	classEdge(t, edges, "tier/process-is-ambiguous",
		"api/main.go", "port:process.exec:>git", schema.Ambiguous, "process-go")
	classEdge(t, edges, "tier/sdk-callsite-is-extracted",
		"ai/agent.py", "port:network.http:>api.openai.com", schema.Extracted, "service:openai")
	classEdge(t, edges, "tier/dep-declaration-is-inferred",
		"pyproject.toml", "port:network.http:>api.openai.com", schema.Inferred, "service:openai")
	classEdge(t, edges, "tier/env-is-inferred",
		"api/main.go", "port:config.env:>PAYMENTS_API_KEY", schema.Inferred, "env-go")

	// The two-tier SDK finding is the services registry's whole point: the call
	// site is EXTRACTED evidence, the manifest is INFERRED, and they converge on
	// ONE port rather than two.
	if n := countPortEdges(edges, "port:network.http:>api.openai.com"); n != 2 {
		t.Errorf("boundary/egress: SDK host must be reached by exactly 2 edges "+
			"(call site EXTRACTED + manifest INFERRED), got %d", n)
	}

	// ---- provenance ------------------------------------------------------
	// Every boundary edge cites the rule that made it and the site it saw.
	// Without this a wrong fact cannot be traced to the rule that invented it.
	for _, e := range edges {
		if e.Rule == "" || e.Site == "" {
			t.Errorf("provenance: boundary edge lacks rule/site: %s -> %s (rule=%q site=%q)",
				e.Source, e.Target, e.Rule, e.Site)
		}
	}

	// ---- the exact snapshot is the contract ------------------------------
	checkGolden(t, "boundary", renderBoundary(ports, edges))

	// ---- retrieval (D5) --------------------------------------------------
	// Phrased as a developer asks, not as our schema reads. Only phrasings
	// that genuinely retrieve today are gated — see the report in
	// openspec/changes/2026-08-15-question-classes for the concept-level
	// phrasings ("what does it shell out to") that the lexical query cannot
	// yet answer. Gating those would ship a red gate, which is how the perf
	// gate became decoration.
	//
	// NOT gated, and the reason is a finding rather than an omission: querying
	// the exact host `api.weather.example` ranks its port SEVENTH, behind four
	// nodes of api/main.go — and every one of them ties at score 1.51. An exact
	// identifier match earns no boost over a file that merely shares the token
	// "api", so the tie is broken arbitrarily. Adding this fixture's README (the
	// other fixtures all have one) was enough to flip it. That is an
	// internal/query weakness, not a boundary one; gating it here would pin
	// noise and hide the real defect.
	for _, probe := range []struct{ q, want string }{
		{"git process exec", "port:process.exec:>git"},
		{"session token storage", "port:storage.local:>session_token"},
		{"orders route", "port:network.http:</orders"},
	} {
		top := queryTop(t, storeRoot, repo, probe.q, 3)
		if !contains(top, probe.want) {
			t.Errorf("retrieval: %q did not surface %s in top-3, got %v", probe.q, probe.want, top)
		}
	}

	// The `boundaries` verb (ADR 2026-08-15) is the surface a user actually
	// meets, so the fixture gates the ANSWER and not only the facts behind it.
	// A rule can keep emitting correct ports while the summary stops showing
	// them — grouping, the direction split, the secret flag and the citation
	// are each their own failure mode.
	summary := runCLI(t, "boundaries", "--path", repo, "--store", storeRoot)
	for _, want := range []struct{ class, text string }{
		{"direction-split", "CONSUMES"},
		{"direction-split", "PROVIDES"},
		{"boundary/config", "PAYMENTS_API_KEY"},
		{"boundary/config", "SECRET"},
		{"boundary/egress", "api.weather.example"},
		{"boundary/egress", "api.openai.com"},
		{"boundary/process", "git"},
		{"boundary/storage", "session_token"},
		{"api-surface", "/orders"},
	} {
		if !strings.Contains(summary, want.text) {
			t.Errorf("%s: `boundaries` summary is missing %q — the facts may still be in the graph, but the answer stopped showing them\n%s",
				want.class, want.text, summary)
		}
	}
	// The negative case, same as the port facts: a plain env var must not be
	// dressed as a secret in the rendered answer either.
	for _, line := range strings.Split(summary, "\n") {
		if strings.Contains(line, "SERVICE_TIER") && strings.Contains(line, "SECRET") {
			t.Errorf("boundary/config: plain env var rendered as SECRET: %q", line)
		}
	}
}

// portFact is the closed set of reserved metadata a port must carry. Empty
// Scope or Sensitive means "must be absent" — the negative case is half the
// value (a rule that flags every env var passes a positive-only test).
type portFact struct {
	Direction  string
	Transport  string
	Identifier string
	Scope      string
	Sensitive  string
}

// classPort asserts one port's full reserved shape and names the CLASS on
// failure, so a broken rule reports as "boundary/process" rather than as an
// anonymous diff.
func classPort(t *testing.T, ports map[string]schema.Node, class, id string, want portFact) {
	t.Helper()
	n, ok := ports[id]
	if !ok {
		t.Errorf("%s: port %s is MISSING — the rule that produces it stopped matching", class, id)
		return
	}
	m := n.Metadata
	for _, f := range []struct{ field, got, want string }{
		{"direction", m["direction"], want.Direction},
		{"transport", m["transport"], want.Transport},
		{"identifier", m["identifier"], want.Identifier},
		{"scope", m["scope"], want.Scope},
		{"sensitive", m["sensitive"], want.Sensitive},
	} {
		if f.got != f.want {
			t.Errorf("%s: %s metadata.%s = %q, want %q", class, id, f.field, f.got, f.want)
		}
	}
}

// classEdge asserts a boundary edge exists with an exact tier and rule.
func classEdge(t *testing.T, edges []boundaryEdge, class, src, dst, tier, rule string) {
	t.Helper()
	for _, e := range edges {
		if e.Source == src && e.Target == dst {
			if e.Confidence != tier {
				t.Errorf("%s: %s -> %s is %s, want %s — a tier change is a change of CLAIM",
					class, src, dst, e.Confidence, tier)
			}
			if e.Rule != rule {
				t.Errorf("%s: %s -> %s attributed to rule %q, want %q", class, src, dst, e.Rule, rule)
			}
			return
		}
	}
	t.Errorf("%s: no boundary edge %s -> %s", class, src, dst)
}

type boundaryEdge struct {
	Source, Target, Relation, Confidence, Rule, Site string
}

func boundaryPorts(t *testing.T, storeRoot string) map[string]schema.Node {
	t.Helper()
	out := map[string]schema.Node{}
	for _, key := range storeKeys(t, storeRoot) {
		st, err := store.Open(storeRoot, key)
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := st.Nodes()
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range nodes {
			if n.Kind == "port" {
				out[n.ID] = n
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no port nodes at all — the boundary lane produced NOTHING")
	}
	return out
}

func boundaryEdges(t *testing.T, storeRoot string) []boundaryEdge {
	t.Helper()
	var out []boundaryEdge
	for _, key := range storeKeys(t, storeRoot) {
		st, err := store.Open(storeRoot, key)
		if err != nil {
			t.Fatal(err)
		}
		es, err := st.Edges()
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range es {
			if e.Relation != "consumes" && e.Relation != "provides" {
				continue
			}
			out = append(out, boundaryEdge{
				Source: e.Source, Target: e.Target, Relation: e.Relation,
				Confidence: e.Confidence,
				Rule:       e.Metadata["rule"], Site: e.Metadata["site"],
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Target < out[j].Target
	})
	return out
}

func countPortEdges(edges []boundaryEdge, port string) int {
	n := 0
	for _, e := range edges {
		if e.Target == port {
			n++
		}
	}
	return n
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// renderBoundary renders ONLY the boundary contract — every port with its full
// reserved metadata, every boundary edge with tier, rule and site. Separate
// from snapshot() on purpose: a boundary regression shows up as a boundary
// diff, not buried in a whole-graph one.
func renderBoundary(ports map[string]schema.Node, edges []boundaryEdge) string {
	var b strings.Builder
	ids := make([]string, 0, len(ports))
	for id := range ports {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	fmt.Fprintf(&b, "== ports (%d)\n", len(ids))
	for _, id := range ids {
		m := ports[id].Metadata
		fmt.Fprintf(&b, "P %s | dir=%s | transport=%s | identifier=%s | scope=%s | sensitive=%s\n",
			id, m["direction"], m["transport"], m["identifier"],
			orDash(m["scope"]), orDash(m["sensitive"]))
	}
	fmt.Fprintf(&b, "== boundary edges (%d)\n", len(edges))
	for _, e := range edges {
		fmt.Fprintf(&b, "E %s -%s-> %s | %s | rule=%s | site=%s\n",
			e.Source, e.Relation, e.Target, e.Confidence, e.Rule, e.Site)
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
