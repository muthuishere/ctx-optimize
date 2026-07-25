package query

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// The problem this file pins, measured on ctx-optimize's own store before the
// fix: prose repeats a question's words far more often than code does, so on a
// docs-heavy repo lexical scoring answers "where is X implemented" with the ADR
// ABOUT X. Doc nodes were 40% of that graph (1,315 section + 278 document of
// 3,963) and took 15 of 30 top-3 slots across 10 code-intent queries, holding
// #1 for 5 of 10 — README.md ranked above the function being asked about.
//
// The store used here is whatever CTX_OPTIMIZE_DOCDEMOTE_STORE points at (a
// gathered <store>/<key>/graph/ dir). Absent, these tests skip: the fixture is
// a real repo's graph, too big to commit, and the golden + judged tiers are the
// hermetic guard. The sweep below is how docDemote's value was chosen; it is
// kept so the next person can re-run it instead of trusting the number.
func loadStore(t *testing.T) ([]schema.Node, []schema.Edge) {
	t.Helper()
	dir := os.Getenv("CTX_OPTIMIZE_DOCDEMOTE_STORE")
	if dir == "" {
		t.Skip("set CTX_OPTIMIZE_DOCDEMOTE_STORE=<store>/<key>/graph to run the doc-demote measurement")
	}
	read := func(name string, fn func([]byte) error) {
		f, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(f), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if err := fn([]byte(line)); err != nil {
				t.Fatal(err)
			}
		}
	}
	var nodes []schema.Node
	var edges []schema.Edge
	read("nodes.ndjson", func(b []byte) error {
		var n schema.Node
		if err := json.Unmarshal(b, &n); err != nil {
			return err
		}
		nodes = append(nodes, n)
		return nil
	})
	read("edges.ndjson", func(b []byte) error {
		var e schema.Edge
		if err := json.Unmarshal(b, &e); err != nil {
			return err
		}
		edges = append(edges, e)
		return nil
	})
	return nodes, edges
}

// codeIntentQuestions: real "where is this implemented" questions about this
// codebase, each with the symbol a correct answer must surface in the top 3.
var codeIntentQuestions = []struct {
	q    string
	want string // substring of the expected node id
}{
	{"redact secret value line", "redactSensitiveLines"},
	{"resolve call to unique name", "internal/extract/code/code.go"},
	{"prune stale nodes on add", "internal/store/store.go"},
	{"parse wasm instance grammar", "internal/extract/code/wasm.go"},
	{"validate batch schema node", "internal/schema/schema.go"},
	{"budget query hits ranking", "internal/query/query.go"},
	{"audit append only log", "internal/audit/audit.go"},
	{"dotenv ladder machine global", "internal/sources"},
	{"compose service depends on", "dockercompose.go"},
	{"shortest path between nodes", "internal/analyze"},
}

// docIntentQuestions: questions that ARE about prose. The demote must not fire
// here — a fix that buries the docs when you asked for them is not a fix.
var docIntentQuestions = []string{
	"adr decision record sources",
	"readme quickstart docs",
	"changelog release notes",
}

func topKIDs(nodes []schema.Node, edges []schema.Edge, q string, k int) []string {
	r := Run(nodes, edges, q, 4000)
	var ids []string
	for i, h := range r.Hits {
		if i >= k {
			break
		}
		ids = append(ids, h.Node.ID)
	}
	return ids
}

func codeHitsAt3(nodes []schema.Node, edges []schema.Edge) int {
	n := 0
	for _, c := range codeIntentQuestions {
		for _, id := range topKIDs(nodes, edges, c.q, 3) {
			if strings.Contains(id, c.want) {
				n++
				break
			}
		}
	}
	return n
}

func docsSurvive(nodes []schema.Node, edges []schema.Edge) int {
	n := 0
	for _, q := range docIntentQuestions {
		for _, id := range topKIDs(nodes, edges, q, 3) {
			if strings.HasSuffix(id, ".md") || strings.Contains(id, ".md::") {
				n++
				break
			}
		}
	}
	return n
}

// TestDocDemoteChosenByMeasurement is the sweep, not an assertion about one
// number: it prints code-intent recall@3 and doc-intent survival at each
// candidate multiplier so the shipped value is a reviewed data point. 1.0 is
// the pre-fix baseline.
func TestDocDemoteChosenByMeasurement(t *testing.T) {
	nodes, edges := loadStore(t)
	orig := docDemote
	defer func() { docDemote = orig }()
	t.Logf("corpus: %d nodes, %d edges", len(nodes), len(edges))
	for _, k := range []float64{1.0, 0.75, 0.6, 0.5, 0.35, 0.25} {
		docDemote = k
		label := ""
		if k == 1.0 {
			label = "  (pre-fix baseline)"
		} else if k == orig {
			label = "  (SHIPPED)"
		}
		t.Logf("docDemote=%.2f  code-intent recall@3: %2d/%d   doc-intent survival: %d/%d%s",
			k, codeHitsAt3(nodes, edges), len(codeIntentQuestions),
			docsSurvive(nodes, edges), len(docIntentQuestions), label)
	}
}

// TestDocDemoteBeatsBaseline is the assertion: the shipped multiplier must
// answer strictly more code-intent questions than no demote at all, and must
// not cost a single doc-intent question.
func TestDocDemoteBeatsBaseline(t *testing.T) {
	nodes, edges := loadStore(t)
	orig := docDemote
	defer func() { docDemote = orig }()

	docDemote = 1.0
	baseCode, baseDocs := codeHitsAt3(nodes, edges), docsSurvive(nodes, edges)
	docDemote = orig
	gotCode, gotDocs := codeHitsAt3(nodes, edges), docsSurvive(nodes, edges)

	if gotCode <= baseCode {
		t.Errorf("docDemote=%.2f answers %d/%d code-intent questions, baseline answers %d — no improvement, do not ship it",
			orig, gotCode, len(codeIntentQuestions), baseCode)
	}
	if gotDocs < baseDocs {
		t.Errorf("doc-intent regressed: %d/%d vs baseline %d — the demote is firing on questions that ASK for prose",
			gotDocs, len(docIntentQuestions), baseDocs)
	}
}

// docIntent must turn the demote off, or "which ADR covers sources" loses the
// ADR. Hermetic — no store needed.
func TestDocIntentDisablesDemote(t *testing.T) {
	doc := schema.Node{ID: "a.md::x", Kind: "section", Label: "x", Source: "a.md"}
	plain := intentAdjust(&doc, 1.0, false, false, false)
	asked := intentAdjust(&doc, 1.0, false, false, true)
	if plain >= asked {
		t.Errorf("doc demote did not fire: %.2f vs %.2f", plain, asked)
	}
	if asked != 1.0 {
		t.Errorf("doc-intent question still demoted prose: %.2f", asked)
	}
	for _, q := range []string{"which adr covers this", "readme setup", "the design proposal"} {
		if !docIntent(Tokenize(q)) {
			t.Errorf("docIntent missed %q", q)
		}
	}
	for _, q := range []string{"redact secret value", "parse wasm grammar"} {
		if docIntent(Tokenize(q)) {
			t.Errorf("docIntent fired on a code question: %q", q)
		}
	}
}

// Code kinds must never be demoted — the demote is scoped to prose.
func TestDemoteScopedToProse(t *testing.T) {
	for _, kind := range []string{"function", "method", "type", "class", "config_key", "service"} {
		n := schema.Node{ID: "f.go::X", Kind: kind, Label: "X", Source: "f.go"}
		if got := intentAdjust(&n, 1.0, false, false, false); got != 1.0 {
			t.Errorf("kind %q was demoted (%.2f) — only section/document may be", kind, got)
		}
	}
}
