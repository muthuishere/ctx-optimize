package boundaries

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Hermetic: point the machine dirs somewhere empty so a developer's real
// ~/ctxoptimize/boundaries and ~/ctxoptimize/services never leak into a test.
func hermetic(t *testing.T) {
	t.Helper()
	t.Setenv("CTX_OPTIMIZE_BOUNDARIES", t.TempDir())
	t.Setenv("CTX_OPTIMIZE_SERVICES", t.TempDir())
}

func find(b *schema.Batch, id string) *schema.Node {
	for i := range b.Nodes {
		if b.Nodes[i].ID == id {
			return &b.Nodes[i]
		}
	}
	return nil
}

func TestDefaultsExtractEnvProcessAndFlagSensitive(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, "main.go", `package main
func main() {
	_ = os.Getenv("OPENAI_API_KEY")
	_ = os.Getenv("PLAIN_SETTING")
	_ = exec.Command("git", "log")
}`)
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	key := find(b, "port:config.env:>OPENAI_API_KEY")
	if key == nil || key.Metadata["sensitive"] != "true" {
		t.Fatalf("sensitive env not flagged: %+v", key)
	}
	if find(b, "port:config.env:>PLAIN_SETTING").Metadata["sensitive"] == "true" {
		t.Fatal("plain env wrongly flagged sensitive")
	}
	git := find(b, "port:process.exec:>git")
	if git == nil {
		t.Fatalf("process.exec git not found; nodes: %v", len(b.Nodes))
	}
	// Every edge carries its rule and site — the citation `verify` checks.
	for _, e := range b.Edges {
		if e.Metadata["rule"] == "" || e.Metadata["site"] == "" {
			t.Fatalf("edge missing rule/site provenance: %+v", e)
		}
	}
}

func TestScopeIsComputedByJoin(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	// A repo-local rule PROVIDES an identifier that a default rule CONSUMES:
	// the consumer must come out scope=internal; an unmatched one external.
	write(t, root, ".ctxoptimize/boundaries.json", `{"version":1,"boundaries":[
	  {"id":"routes-x","transport":"network.http","direction":"provides",
	   "when":{"ext":[".go"]},
	   "match":[{"re":"Handle\\(\"([^\"]+)\"","identifier":1}]}]}`)
	write(t, root, "s.go", `package s
func init() {
	mux.Handle("api.internal.test", h)
	_ = "https://api.internal.test/x"
	_ = "https://api.external.test/y"
}`)
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	in := find(b, "port:network.http:>api.internal.test")
	ex := find(b, "port:network.http:>api.external.test")
	if in == nil || in.Metadata["scope"] != "internal" {
		t.Fatalf("joined consumer not internal: %+v", in)
	}
	if ex == nil || ex.Metadata["scope"] != "external" {
		t.Fatalf("unjoined consumer not external: %+v", ex)
	}
}

func TestRepoRuleOverridesEmbeddedById(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	// Narrow the embedded env-go rule to nothing: same ID, impossible ext.
	write(t, root, ".ctxoptimize/boundaries.json", `{"version":1,"boundaries":[
	  {"id":"env-go","transport":"config.env","direction":"consumes",
	   "when":{"ext":[".nope"]},
	   "match":[{"re":"never([A-Z]+)","identifier":1}]}]}`)
	write(t, root, "main.go", `package main
func f() { _ = os.Getenv("SHOULD_NOT_APPEAR") }`)
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if find(b, "port:config.env:>SHOULD_NOT_APPEAR") != nil {
		t.Fatal("embedded rule ran despite repo override with the same id")
	}
}

func TestComputedIdentifierIsAmbiguousNeverAsserted(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, "a.ts", "localStorage.setItem(`ailab_${provider}`, v)\n")
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Edges) != 1 || b.Edges[0].Confidence != schema.Ambiguous {
		t.Fatalf("computed key must be AMBIGUOUS: %+v", b.Edges)
	}
}

func TestMalformedConfigFailsLoud(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, ".ctxoptimize/boundaries.json", `{"version":1,"boundaries":[
	  {"id":"bad","transport":"network.http","direction":"sideways",
	   "match":[{"re":"x","identifier":0}]}]}`)
	if _, err := Extract(root); err == nil {
		t.Fatal("invalid direction accepted silently")
	}
}

func TestVendoredTreesExcluded(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, "benchmarks/corpus/conf.py", `x = os.environ["FLASK_SECRET_KEY"]`)
	write(t, root, "real.py", `y = os.environ["REAL_VAR"]`)
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if find(b, "port:config.env:>FLASK_SECRET_KEY") != nil {
		t.Fatal("vendored corpus leaked into the boundary graph — the spike's precision failure")
	}
	if find(b, "port:config.env:>REAL_VAR") == nil {
		t.Fatal("real file missed")
	}
}

// The routes-* rules are the PROVIDES side of D3 — additive port coverage.
// The AST recognizers in internal/extract/code remain the EXTRACTED route
// truth (kind=route + handles edges); these rules ship INFERRED and never
// touch that surface (pinned by the byte-match evidence in the ADR).
func TestRouteRulesEmitProvidesPorts(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, "server.js", `const app = express();
app.get('/users', listUsers);
router.delete('/users/:id', h);
`)
	write(t, root, "api.py", `@app.get("/items")
def list_items(): pass
`)
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"port:network.http:</users", "port:network.http:</users/:id", "port:network.http:</items",
	} {
		n := find(b, id)
		if n == nil || n.Metadata["direction"] != "provides" {
			t.Fatalf("provides port missing or misdirected: %s → %+v", id, n)
		}
		if n.Metadata["otel.http.route"] != n.Metadata["identifier"] {
			t.Fatalf("otel.http.route not stamped: %+v", n)
		}
	}
	for _, e := range b.Edges {
		if e.Relation != "provides" {
			continue
		}
		if e.Confidence != schema.Inferred {
			t.Fatalf("route rules are regex-tier and must ship INFERRED: %+v", e)
		}
		if !strings.HasPrefix(e.Metadata["rule"], "routes-") {
			t.Fatalf("provides edge missing routes-* provenance: %+v", e)
		}
	}
}

func TestRouteRuleSkipsLookalikesAndComposedFrameworks(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	// cache.get: wrong receiver. /noargs: no second argument. f-string: not a
	// literal. Nest + JSX: composed paths a line-regex would get WRONG — no
	// rule ships for them; the AST recognizers own that ground.
	write(t, root, "a.js", `cache.get('/users', fallback);
app.get('/noargs');
`)
	write(t, root, "b.py", "@app.get(f\"/dyn/{x}\")\ndef dyn(): pass\n")
	write(t, root, "c.ts", `@Controller('users')
export class C { @Get(':id') one() {} }
`)
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range b.Nodes {
		if n.Metadata["direction"] == "provides" {
			t.Fatalf("no provides port should survive these fixtures: %+v", n)
		}
	}
}

func TestBatchPassesSchemaValidate(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, "m.go", `package m
func f() { _ = os.Getenv("SOME_VAR"); _ = exec.Command("sh") }`)
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("boundary batch rejected by the schema door: %v", err)
	}
}

func TestDeterministicOutput(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, "a.go", `package a
func f() { _ = os.Getenv("VAR_B"); _ = os.Getenv("VAR_A") }`)
	b1, _ := Extract(root)
	b2, _ := Extract(root)
	if len(b1.Nodes) != len(b2.Nodes) {
		t.Fatal("node count varies")
	}
	for i := range b1.Nodes {
		if b1.Nodes[i].ID != b2.Nodes[i].ID {
			t.Fatal("node order varies between runs")
		}
	}
}
