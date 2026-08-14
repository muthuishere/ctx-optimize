package code

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// The boundary lane's end-to-end tests live HERE, not in internal/boundaries,
// because since ADR 2026-08-14 the shipped rules are AST shapes: producing a
// port requires a parse, and the parser lives in this package. internal/
// boundaries keeps the loader/normalize/verify unit tests, which need no AST.

func bwrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// bnd runs the real gather path and returns the boundary batch. Hermetic: the
// machine rule/service dirs are pointed at empty temp dirs so a developer's
// own ~/ctxoptimize never leaks in.
func bnd(t *testing.T, files map[string]string) *schema.Batch {
	t.Helper()
	t.Setenv("CTX_OPTIMIZE_BOUNDARIES", t.TempDir())
	t.Setenv("CTX_OPTIMIZE_SERVICES", t.TempDir())
	root := t.TempDir()
	for rel, content := range files {
		bwrite(t, root, rel, content)
	}
	_, b, err := ExtractPathsWithBoundaries(root, []string{root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func port(b *schema.Batch, id string) *schema.Node {
	for i := range b.Nodes {
		if b.Nodes[i].ID == id {
			return &b.Nodes[i]
		}
	}
	return nil
}

func edgeBetween(b *schema.Batch, src, tgt string) *schema.Edge {
	for i := range b.Edges {
		if b.Edges[i].Source == src && b.Edges[i].Target == tgt {
			return &b.Edges[i]
		}
	}
	return nil
}

func TestASTDefaultsExtractEnvProcessAndFlagSensitive(t *testing.T) {
	b := bnd(t, map[string]string{"main.go": `package main
func main() {
	_ = os.Getenv("OPENAI_API_KEY")
	_ = os.Getenv("PLAIN_SETTING")
	_ = exec.Command("git", "log")
}`})
	key := port(b, "port:config.env:>OPENAI_API_KEY")
	if key == nil || key.Metadata["sensitive"] != "true" {
		t.Fatalf("sensitive env not flagged: %+v", key)
	}
	if p := port(b, "port:config.env:>PLAIN_SETTING"); p == nil || p.Metadata["sensitive"] == "true" {
		t.Fatalf("plain env missing or wrongly flagged: %+v", p)
	}
	if port(b, "port:process.exec:>git") == nil {
		t.Fatalf("process.exec git not found; nodes: %d", len(b.Nodes))
	}
	for _, e := range b.Edges {
		if e.Metadata["rule"] == "" || e.Metadata["site"] == "" {
			t.Fatalf("edge missing rule/site provenance: %+v", e)
		}
	}
}

// The headline of ADR 2026-08-14: a non-literal argument is NOT a miss. The
// regex lane could not see `os.Getenv(name)` at all — process-py measured
// 0.00 and reported "this repo spawns nothing", which was a lie.
func TestDynamicArgumentIsVisibleAndAmbiguous(t *testing.T) {
	b := bnd(t, map[string]string{
		"main.go": `package main
func f(name string, bin string) {
	_ = os.Getenv(name)
	_ = exec.Command(bin, "log")
}`,
		"run.py": `import subprocess
def g(argv):
    subprocess.run(argv)
`,
	})
	var dyn int
	for _, n := range b.Nodes {
		if n.Metadata["resolved"] == "dynamic" {
			dyn++
		}
	}
	if dyn < 3 {
		t.Fatalf("dynamic sites must be visible, got %d dynamic ports of %d: %+v", dyn, len(b.Nodes), b.Nodes)
	}
	for _, e := range b.Edges {
		if e.Confidence != schema.Ambiguous {
			t.Fatalf("a computed identifier must never be asserted: %+v", e)
		}
	}
}

func TestASTComputedTemplateIsAmbiguous(t *testing.T) {
	b := bnd(t, map[string]string{"a.ts": "localStorage.setItem(`ailab_${provider}`, v)\n"})
	if len(b.Edges) != 1 || b.Edges[0].Confidence != schema.Ambiguous {
		t.Fatalf("computed key must be AMBIGUOUS: %+v", b.Edges)
	}
}

func TestASTVendoredTreesExcluded(t *testing.T) {
	b := bnd(t, map[string]string{
		"benchmarks/corpus/conf.py": `x = os.environ["FLASK_SECRET_KEY"]`,
		"real.py":                   `y = os.environ["REAL_VAR"]`,
	})
	if port(b, "port:config.env:>FLASK_SECRET_KEY") != nil {
		t.Fatal("vendored corpus leaked into the boundary graph — the spike's precision failure")
	}
	if port(b, "port:config.env:>REAL_VAR") == nil {
		t.Fatalf("real file missed; nodes: %d", len(b.Nodes))
	}
}

func TestASTScopeIsComputedByJoin(t *testing.T) {
	b := bnd(t, map[string]string{
		"s.go": `package s
func init() {
	mux.HandleFunc("/internal", h)
	_ = "https://api.external.test/y"
}`,
	})
	ex := port(b, "port:network.http:>api.external.test")
	if ex == nil || ex.Metadata["scope"] != "external" {
		t.Fatalf("unjoined consumer not external: %+v", ex)
	}
	in := port(b, "port:network.http:</internal")
	if in == nil || in.Metadata["direction"] != "provides" {
		t.Fatalf("provides port missing: %+v", in)
	}
}

func TestASTRouteRulesEmitProvidesPorts(t *testing.T) {
	b := bnd(t, map[string]string{
		"server.js": `const app = express();
app.get('/users', listUsers);
router.delete('/users/:id', h);
`,
		"api.py": `@app.get("/items")
def list_items(): pass
`,
		"C.java": `class C {
  @GetMapping("/java")
  public String h(){ return ""; }
  @RequestMapping(path="/named")
  public String g(){ return ""; }
}`,
	})
	for _, id := range []string{
		"port:network.http:</users", "port:network.http:</users/*",
		"port:network.http:</items", "port:network.http:</java",
		// @RequestMapping(path=…) — the named-argument form a regex never got.
		"port:network.http:</named",
	} {
		n := port(b, id)
		if n == nil || n.Metadata["direction"] != "provides" {
			t.Fatalf("provides port missing or misdirected: %s → %+v", id, n)
		}
	}
	param := port(b, "port:network.http:</users/*")
	if param.Metadata["raw"] != "/users/:id" || param.Metadata["otel.http.route"] != "/users/:id" {
		t.Fatalf("raw spelling not recoverable after normalization: %+v", param)
	}
}

// A python route decorator is a route; the same call at runtime is not.
func TestASTDecoratorPositionIsRequiredForPyRoutes(t *testing.T) {
	b := bnd(t, map[string]string{"c.py": `import httpx
def go():
    client.get("/not-a-route")
`})
	if p := port(b, "port:network.http:</not-a-route"); p != nil {
		t.Fatalf("a runtime call must not become a route: %+v", p)
	}
}

// routepacks matches the callee's LAST identifier only, so a rule for
// os.Getenv would also fire on a bare Getenv(). The AST has the receiver.
func TestASTReceiverGateBeatsLastIdentifierMatching(t *testing.T) {
	b := bnd(t, map[string]string{"main.go": `package main
func Getenv(s string) string { return s }
func f() { _ = Getenv("NOT_AN_ENV_READ") }`})
	if p := port(b, "port:config.env:>NOT_AN_ENV_READ"); p != nil {
		t.Fatalf("local Getenv() must not be an env read: %+v", p)
	}
}

func TestASTServiceDepTierAndConfigHint(t *testing.T) {
	b := bnd(t, map[string]string{
		"package.json": `{"name":"x","dependencies":{"firebase":"^10.0.0"}}`,
		"app.js":       "const k = process.env.VITE_FIREBASE_API_KEY\n",
	})
	p := port(b, "port:network.http:>firebase")
	if p == nil {
		t.Fatalf("dep-declared service port missing; nodes: %d", len(b.Nodes))
	}
	if p.Metadata["svc.id"] != "firebase" || p.Metadata["scope"] != "external" {
		t.Fatalf("service port metadata wrong: %+v", p.Metadata)
	}
	if port(b, "port:config.env:>VITE_FIREBASE_API_KEY") == nil {
		t.Fatal("process.env.X member shape did not produce an env port")
	}
	ref := edgeBetween(b, "port:config.env:>VITE_FIREBASE_API_KEY", "port:network.http:>firebase")
	if ref == nil || ref.Relation != "references" {
		t.Fatalf("config_hint edge missing: %+v", ref)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("boundary batch rejected by the schema door: %v", err)
	}
}

func TestASTBatchIsDeterministic(t *testing.T) {
	files := map[string]string{
		"a.go": `package a
func f() { _ = os.Getenv("VAR_B"); _ = os.Getenv("VAR_A") }`,
		"b.ts": "const x = process.env.Z;\nconst y = process.env.A;\n",
	}
	b1 := bnd(t, files)
	b2 := bnd(t, files)
	if len(b1.Nodes) != len(b2.Nodes) || len(b1.Edges) != len(b2.Edges) {
		t.Fatalf("batch size varies: %d/%d vs %d/%d", len(b1.Nodes), len(b1.Edges), len(b2.Nodes), len(b2.Edges))
	}
	for i := range b1.Nodes {
		if b1.Nodes[i].ID != b2.Nodes[i].ID {
			t.Fatalf("node order varies at %d: %s vs %s", i, b1.Nodes[i].ID, b2.Nodes[i].ID)
		}
	}
	for i := range b1.Edges {
		if b1.Edges[i].Source != b2.Edges[i].Source || b1.Edges[i].Target != b2.Edges[i].Target {
			t.Fatalf("edge order varies at %d", i)
		}
	}
}

// The rules are data and the loader is fail-closed: a shape that cannot use a
// field must be rejected at add time, not silently ignored.
func TestASTMalformedRuleFailsLoud(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_BOUNDARIES", t.TempDir())
	root := t.TempDir()
	bwrite(t, root, ".ctxoptimize/boundaries.json", `{"version":1,"boundaries":[
	  {"id":"bad","transport":"config.env","direction":"consumes",
	   "ast":[{"shape":"member","name":"nope"}]}]}`)
	if _, _, err := ExtractPathsWithBoundaries(root, []string{root}, nil); err == nil {
		t.Fatal("a field the shape cannot use was accepted silently")
	}
}
