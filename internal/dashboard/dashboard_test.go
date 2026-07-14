package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/audit"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
	"github.com/muthuishere/ctx-optimize/internal/usage"
)

func testServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	s, err := store.Open(root, "myrepo")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Merge(&schema.Batch{
		Producer: "test",
		Nodes: []schema.Node{
			{ID: "a.md::refund-flow", Label: "Refund Flow", Kind: "section", FileType: "markdown", Source: "a.md"},
			{ID: "pg://db/refunds", Label: "refunds", Kind: "table", FileType: "schema", Source: "pg://db/refunds"},
		},
		Edges: []schema.Edge{
			{Source: "a.md::refund-flow", Target: "pg://db/refunds", Relation: "references", Confidence: "EXTRACTED"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(root, nil))
	t.Cleanup(srv.Close)
	return srv, root
}

func get(t *testing.T, url string, wantCode int) []byte {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantCode {
		t.Fatalf("%s: status %d (want %d): %s", url, resp.StatusCode, wantCode, body)
	}
	return body
}

func token(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	var tok struct {
		Token string `json:"token"`
	}
	json.Unmarshal(get(t, srv.URL+"/api/token", 200), &tok)
	if tok.Token == "" {
		t.Fatal("no token")
	}
	return tok.Token
}

// send issues a mutation with the given token header.
func send(t *testing.T, method, url, tok, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if tok != "" {
		req.Header.Set("X-Ctx-Token", tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestDashboard(t *testing.T) {
	srv, _ := testServer(t)

	// The page itself is embedded and self-contained.
	page := string(get(t, srv.URL+"/", 200))
	if !strings.Contains(page, "ctx-optimize") {
		t.Fatal("index page missing")
	}

	// Modules list the store folders with counts.
	var mods []Module
	json.Unmarshal(get(t, srv.URL+"/api/modules", 200), &mods)
	if len(mods) != 1 || mods[0].Key != "myrepo" || mods[0].Nodes != 2 || mods[0].Edges != 1 {
		t.Fatalf("modules: %+v", mods)
	}

	// Graph returns the (budgeted) content with totals.
	var g struct {
		Nodes      []schema.Node `json:"nodes"`
		Edges      []schema.Edge `json:"edges"`
		TotalNodes int           `json:"total_nodes"`
		Truncated  bool          `json:"truncated"`
	}
	json.Unmarshal(get(t, srv.URL+"/api/graph?module=myrepo", 200), &g)
	if len(g.Nodes) != 2 || len(g.Edges) != 1 || g.TotalNodes != 2 || g.Truncated {
		t.Fatalf("graph: %d nodes %d edges total %d trunc %v", len(g.Nodes), len(g.Edges), g.TotalNodes, g.Truncated)
	}

	// Center expansion: the neighborhood door.
	json.Unmarshal(get(t, srv.URL+"/api/graph?module=myrepo&center=pg://db/refunds&depth=1", 200), &g)
	if len(g.Nodes) != 2 {
		t.Fatalf("center graph: %d nodes", len(g.Nodes))
	}
	get(t, srv.URL+"/api/graph?module=myrepo&center=nope", 404)

	// Budget: limit caps nodes, truncated says so.
	json.Unmarshal(get(t, srv.URL+"/api/graph?module=myrepo&limit=1", 200), &g)
	if len(g.Nodes) != 1 || !g.Truncated {
		t.Fatalf("limited graph: %d nodes trunc %v", len(g.Nodes), g.Truncated)
	}

	// Query runs the same engine as the CLI.
	var res struct {
		Hits []struct {
			Node schema.Node `json:"node"`
		} `json:"hits"`
	}
	json.Unmarshal(get(t, srv.URL+"/api/query?module=myrepo&q=refund", 200), &res)
	if len(res.Hits) == 0 {
		t.Fatal("query returned no hits")
	}

	// Read-only: an unknown module 404s and must NOT create store layout.
	get(t, srv.URL+"/api/graph?module=nope", 404)
	get(t, srv.URL+"/api/query?module=myrepo", 400) // missing q
	get(t, srv.URL+"/nope", 404)
}

func TestUnknownModuleDoesNotCreateDirs(t *testing.T) {
	srv, root := testServer(t)
	get(t, srv.URL+"/api/graph?module=typo", 404)
	if _, err := store.Open(root, "check"); err != nil {
		t.Fatal(err)
	}
	// "typo" must not exist as a module now.
	var mods []Module
	json.Unmarshal(get(t, srv.URL+"/api/modules", 200), &mods)
	for _, m := range mods {
		if m.Key == "typo" {
			t.Fatal("read path created a store folder")
		}
	}
}

// TestEmbeddedBundleNoExternalRefs scans every embedded UI file for external
// asset references — the zero-external-requests contract, checked at the
// bundle level. String mentions (license comments, error-message URLs) are
// fine; loading anything (src/href/url()/import) from http(s) is not.
func TestEmbeddedBundleNoExternalRefs(t *testing.T) {
	bad := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bsrc\s*=\s*["']https?://`),
		regexp.MustCompile(`(?i)\bhref\s*=\s*["']https?://`),
		regexp.MustCompile(`(?i)url\(\s*["']?https?://`),
		regexp.MustCompile(`(?i)@import\s+["']https?://`),
		regexp.MustCompile(`(?i)\bimport\(\s*["']https?://`),
		regexp.MustCompile(`(?i)\bfrom\s*["']https?://`),
		regexp.MustCompile(`(?i)\bfetch\(\s*["']https?://`),
	}
	seen := 0
	err := fs.WalkDir(uiFS, "ui", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch filepath.Ext(path) {
		case ".html", ".js", ".css", ".mjs", ".svg":
		default:
			return nil
		}
		seen++
		data, err := uiFS.ReadFile(path)
		if err != nil {
			return err
		}
		for _, re := range bad {
			if loc := re.FindIndex(data); loc != nil {
				lo, hi := max(loc[0]-40, 0), min(loc[1]+80, len(data))
				t.Errorf("%s: external asset ref: …%s…", path, data[lo:hi])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen == 0 {
		t.Fatal("no UI files embedded — is internal/dashboard/ui/ committed?")
	}
}

func TestMutationGuards(t *testing.T) {
	srv, root := testServer(t)
	tok := token(t, srv)

	// No token → 403; wrong method → 405.
	resp, _ := send(t, "DELETE", srv.URL+"/api/store", "", `{"key":"myrepo","confirm":true}`)
	if resp.StatusCode != 403 {
		t.Fatalf("tokenless mutation: %d", resp.StatusCode)
	}
	resp, _ = send(t, "GET", srv.URL+"/api/store", tok, "")
	if resp.StatusCode != 405 {
		t.Fatalf("wrong method: %d", resp.StatusCode)
	}

	// Non-loopback peer → 403 even WITH the token (RemoteAddr, not headers).
	h := NewHandler(root, nil)
	req := httptest.NewRequest("DELETE", "/api/store", strings.NewReader(`{"key":"myrepo","confirm":true}`))
	req.RemoteAddr = "203.0.113.7:55555"
	req.Header.Set("X-Ctx-Token", tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "loopback") {
		t.Fatalf("non-loopback mutation: %d %s", rec.Code, rec.Body.String())
	}

	// The token itself is loopback-only too.
	req = httptest.NewRequest("GET", "/api/token", nil)
	req.RemoteAddr = "203.0.113.7:55555"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("non-loopback token fetch: %d", rec.Code)
	}

	// Reads still answer for a non-loopback peer (--host may widen reads).
	req = httptest.NewRequest("GET", "/api/modules", nil)
	req.RemoteAddr = "203.0.113.7:55555"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("read for non-loopback peer: %d", rec.Code)
	}
}

func TestConfigSetValidatedAndAudited(t *testing.T) {
	srv, root := testServer(t)
	tok := token(t, srv)

	// Invalid value → the CLI validator's own loud error, nothing written.
	resp, body := send(t, "PUT", srv.URL+"/api/config", tok, `{"level":"global","key":"instructions","value":"BOGUS"}`)
	if resp.StatusCode != 400 || !strings.Contains(body, "instructions") {
		t.Fatalf("invalid value: %d %s", resp.StatusCode, body)
	}

	// Valid set → file written + audit line with hashes.
	resp, body = send(t, "PUT", srv.URL+"/api/config", tok, `{"level":"global","key":"instructions","value":"claude"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("config set: %d %s", resp.StatusCode, body)
	}
	gcfg, err := store.LoadGlobalConfig(root)
	if err != nil || gcfg.Instructions != "CLAUDE" {
		t.Fatalf("global config after set: %+v %v", gcfg, err)
	}
	lines, _ := audit.List(root)
	if len(lines) != 1 || lines[0].Actor != "dashboard" ||
		!strings.Contains(lines[0].Action, "config.set instructions=CLAUDE") || lines[0].AfterHash == "" {
		t.Fatalf("audit after config set: %+v", lines)
	}

	// Project level needs a real dir; validated the same way.
	repo := t.TempDir()
	resp, body = send(t, "PUT", srv.URL+"/api/config", tok,
		`{"level":"project","path":`+strconvQuote(repo)+`,"key":"hooks","value":"none"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("project config set: %d %s", resp.StatusCode, body)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".ctxoptimize", "config.json"))
	if !strings.Contains(string(data), `"hooks": "NONE"`) {
		t.Fatalf("project config file: %s", data)
	}

	// The /api/audit feed serves both lines.
	feed := get(t, srv.URL+"/api/audit", 200)
	var got []audit.Line
	json.Unmarshal(feed, &got)
	if len(got) != 2 {
		t.Fatalf("audit feed: %d lines", len(got))
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestPackAddGuardsAndAudit drives /api/pack with a STUB Ops.AddPack: the
// mutation guards (token, method), the request validation (axis, source,
// scope/path), the loud-error passthrough, and the audit line all get checked
// without touching the real pack installer (that lives in the app-layer test).
func TestPackAddGuardsAndAudit(t *testing.T) {
	root := t.TempDir()
	var gotAxis, gotSource string
	var gotGlobal bool
	ops := &Ops{AddPack: func(axis, path, source string, global bool, out io.Writer) error {
		if source == "bad name" {
			return fmt.Errorf("route pack name %q: letters, digits, - and _ only", source)
		}
		gotAxis, gotSource, gotGlobal = axis, source, global
		fmt.Fprintln(out, "scaffolded")
		return nil
	}}
	srv := httptest.NewServer(NewHandler(root, ops))
	t.Cleanup(srv.Close)
	tok := token(t, srv)

	// Guards: no token → 403; wrong method → 405.
	resp, _ := send(t, "POST", srv.URL+"/api/pack", "", `{"axis":"routes","scope":"global","source":"myroutes"}`)
	if resp.StatusCode != 403 {
		t.Fatalf("tokenless pack add: %d", resp.StatusCode)
	}
	resp, _ = send(t, "GET", srv.URL+"/api/pack", tok, "")
	if resp.StatusCode != 405 {
		t.Fatalf("wrong method: %d", resp.StatusCode)
	}

	// Validation: bad axis, empty source, project scope w/ bogus dir.
	resp, body := send(t, "POST", srv.URL+"/api/pack", tok, `{"axis":"nope","scope":"global","source":"x"}`)
	if resp.StatusCode != 400 || !strings.Contains(body, "axis") {
		t.Fatalf("bad axis: %d %s", resp.StatusCode, body)
	}
	resp, _ = send(t, "POST", srv.URL+"/api/pack", tok, `{"axis":"routes","scope":"global","source":"  "}`)
	if resp.StatusCode != 400 {
		t.Fatalf("empty source: %d", resp.StatusCode)
	}
	resp, _ = send(t, "POST", srv.URL+"/api/pack", tok, `{"axis":"routes","scope":"project","path":"/no/such/dir","source":"x"}`)
	if resp.StatusCode != 400 {
		t.Fatalf("bogus project path: %d", resp.StatusCode)
	}

	// The installer's loud error surfaces verbatim as a 400.
	resp, body = send(t, "POST", srv.URL+"/api/pack", tok, `{"axis":"routes","scope":"global","source":"bad name"}`)
	if resp.StatusCode != 400 || !strings.Contains(body, "letters, digits") {
		t.Fatalf("ops error passthrough: %d %s", resp.StatusCode, body)
	}

	// Happy path → 200, right args passed, audited as "<axis>.pack.add <src>".
	resp, body = send(t, "POST", srv.URL+"/api/pack", tok, `{"axis":"manifests","scope":"global","source":"deps"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("pack add: %d %s", resp.StatusCode, body)
	}
	if gotAxis != "manifests" || gotSource != "deps" || !gotGlobal {
		t.Fatalf("ops args: axis=%q source=%q global=%v", gotAxis, gotSource, gotGlobal)
	}
	lines, _ := audit.List(root)
	if len(lines) == 0 {
		t.Fatal("no audit line for pack add")
	}
	last := lines[len(lines)-1]
	if last.Actor != "dashboard" || last.Action != "manifests.pack.add deps" {
		t.Fatalf("audit line: %+v", last)
	}
}

// TestGraphBudgetIncludesSpecialKindsAndProducers: under a tight degree budget
// that keeps only the well-connected code, (1) a degree-0 special-kind node (a
// route) still survives, (2) a whole ADAPTER/DOC producer whose nodes are all
// low-degree still gets a visible sample so the Viewer's producer filter has
// something to show, and (3) every emitted node carries its producer tag.
func TestGraphBudgetIncludesSpecialKindsAndProducers(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(root, "m")
	if err != nil {
		t.Fatal(err)
	}
	// The well-connected core — producer "code".
	var nodes []schema.Node
	var edges []schema.Edge
	for i := 0; i < 5; i++ { // a clique of well-connected functions
		nodes = append(nodes, schema.Node{ID: fmt.Sprintf("f%d", i), Label: fmt.Sprintf("f%d", i), Kind: "function", FileType: "go", Source: "a.go"})
	}
	for i := 0; i < 5; i++ {
		for j := i + 1; j < 5; j++ {
			edges = append(edges, schema.Edge{Source: fmt.Sprintf("f%d", i), Target: fmt.Sprintf("f%d", j), Relation: "calls", Confidence: "INFERRED"})
		}
	}
	// One isolated route node (degree 0) — the top-N-by-degree cut would drop it.
	nodes = append(nodes, schema.Node{ID: "r1", Label: "GET /x", Kind: "route", FileType: "python", Source: "routes.py"})
	if _, _, err := s.Merge(&schema.Batch{Producer: "code", Nodes: nodes, Edges: edges}); err != nil {
		t.Fatal(err)
	}
	// A postgres adapter's tables — all degree 0, a producer with NO node in
	// the degree cut. Its filter must still have nodes to show.
	if _, _, err := s.Merge(&schema.Batch{Producer: "postgres-schema", Nodes: []schema.Node{
		{ID: "pg://db/orders", Label: "orders", Kind: "table", FileType: "schema", Source: "pg://db/orders"},
	}}); err != nil {
		t.Fatal(err)
	}
	// A plain doc, likewise low-degree, different producer.
	if _, _, err := s.Merge(&schema.Batch{Producer: "markdown", Nodes: []schema.Node{
		{ID: "README.md::intro", Label: "Intro", Kind: "section", FileType: "markdown", Source: "README.md"},
	}}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(root, nil))
	t.Cleanup(srv.Close)

	var g struct {
		Nodes []schema.Node `json:"nodes"`
	}
	json.Unmarshal(get(t, srv.URL+"/api/graph?module=m&limit=3", 200), &g)
	have := map[string]schema.Node{}
	for _, n := range g.Nodes {
		have[n.ID] = n
		if n.Metadata["producer"] == "" { // every node carries its provenance tag
			t.Fatalf("node %s emitted without a producer: %+v", n.ID, n)
		}
	}
	if _, ok := have["r1"]; !ok {
		t.Fatalf("special-kind route node dropped by the degree budget: %+v", g.Nodes)
	}
	if n, ok := have["pg://db/orders"]; !ok {
		t.Fatalf("adapter (postgres-schema) node starved by the budget: %+v", g.Nodes)
	} else if n.Metadata["producer"] != "postgres-schema" {
		t.Fatalf("adapter node producer not carried: %+v", n)
	}
	if _, ok := have["README.md::intro"]; !ok {
		t.Fatalf("document (markdown) node starved by the budget: %+v", g.Nodes)
	}
}

// TestStoresUsageRollup: /api/stores carries each store's served-counter, and
// summing across two fixture stores gives the global roll-up the Overview
// shows. A store with no metrics contributes zero, never an error.
func TestStoresUsageRollup(t *testing.T) {
	root := t.TempDir()
	mk := func(key string, events int) {
		s, err := store.Open(root, key)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Merge(&schema.Batch{Producer: "code", Nodes: []schema.Node{
			{ID: key + ":n", Label: "n", Kind: "function", FileType: "go", Source: "a.go"},
		}}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < events; i++ {
			usage.Record(filepath.Join(root, key), usage.Event{Verb: "query", Bytes: 400})
		}
	}
	mk("repoA", 2)
	mk("repoB", 3)
	// A third store with no usage at all — must roll up as zero, not error.
	mk("repoC", 0)

	srv := httptest.NewServer(NewHandler(root, nil))
	t.Cleanup(srv.Close)

	var stores []StoreInfo
	json.Unmarshal(get(t, srv.URL+"/api/stores", 200), &stores)
	if len(stores) != 3 {
		t.Fatalf("stores: %d", len(stores))
	}
	var served, saved int
	var usd float64
	for _, st := range stores {
		if st.Usage == nil {
			t.Fatalf("store %s missing usage summary", st.Key)
		}
		served += st.Usage.Total
		saved += st.Usage.EstSaved
		usd += st.Usage.EstUSD
	}
	// 5 events total, each Bytes=400 → saved = 7600 − 400/4 = 7500 tokens.
	if served != 5 {
		t.Fatalf("rolled-up answers served: %d (want 5)", served)
	}
	if saved != 5*7500 {
		t.Fatalf("rolled-up tokens saved: %d (want %d)", saved, 5*7500)
	}
	if usd <= 0 {
		t.Fatalf("rolled-up $ saved not positive: %v", usd)
	}
}

func TestStoreDeleteConfirmGated(t *testing.T) {
	srv, root := testServer(t)
	tok := token(t, srv)

	resp, body := send(t, "DELETE", srv.URL+"/api/store", tok, `{"key":"myrepo"}`)
	if resp.StatusCode != 400 || !strings.Contains(body, "confirm") {
		t.Fatalf("unconfirmed delete: %d %s", resp.StatusCode, body)
	}
	resp, _ = send(t, "DELETE", srv.URL+"/api/store", tok, `{"key":"nope","confirm":true}`)
	if resp.StatusCode != 404 {
		t.Fatalf("delete unknown store: %d", resp.StatusCode)
	}
	resp, _ = send(t, "DELETE", srv.URL+"/api/store", tok, `{"key":"myrepo","confirm":true}`)
	if resp.StatusCode != 200 {
		t.Fatalf("confirmed delete: %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(root, "myrepo")); !os.IsNotExist(err) {
		t.Fatal("store dir survived delete")
	}
	lines, _ := audit.List(root)
	if len(lines) != 1 || lines[0].Action != "store.delete" || lines[0].Target != "myrepo" {
		t.Fatalf("audit after delete: %+v", lines)
	}
}

func TestStoresEndpoint(t *testing.T) {
	srv, _ := testServer(t)
	var stores []StoreInfo
	json.Unmarshal(get(t, srv.URL+"/api/stores", 200), &stores)
	if len(stores) != 1 || stores[0].Key != "myrepo" || stores[0].Nodes != 2 {
		t.Fatalf("stores: %+v", stores)
	}
	if stores[0].Fresh != "unknown" { // no source.json in the fixture
		t.Fatalf("freshness: %q", stores[0].Fresh)
	}
	if stores[0].Producers["test"] != 2 {
		t.Fatalf("producers: %+v", stores[0].Producers)
	}
	// No source.json ⇒ no repo dir known ⇒ no open-source links, no error.
	if stores[0].Links != nil {
		t.Fatalf("links without a source path: %+v", stores[0].Links)
	}
}

// TestGithubBaseNormalization pins the origin→blob-base normalization the
// Viewer's GitHub link rides on: both remote forms git prints (scp-style and
// https) collapse to the same blob base; a non-GitHub origin and a missing
// branch yield no base.
func TestGithubBaseNormalization(t *testing.T) {
	const want = "https://github.com/owner/repo/blob/main"
	cases := []struct {
		origin, branch, want string
	}{
		{"git@github.com:owner/repo.git", "main", want},
		{"https://github.com/owner/repo.git", "main", want},
		{"https://github.com/owner/repo", "main", want},
		{"ssh://git@github.com/owner/repo.git", "main", want},
		{"git@github.com:owner/repo.git", "feature/x", "https://github.com/owner/repo/blob/feature/x"},
		{"git@gitlab.com:owner/repo.git", "main", ""},           // non-github host
		{"https://example.com/owner/repo.git", "main", ""},      // non-github host
		{"git@github.com:owner/repo.git", "", ""},               // detached HEAD
		{"https://github.com/owner/repo/extra.git", "main", ""}, // not owner/repo
		{"", "main", ""},
	}
	for _, c := range cases {
		if got := githubBase(c.origin, c.branch); got != c.want {
			t.Errorf("githubBase(%q, %q) = %q, want %q", c.origin, c.branch, got, c.want)
		}
	}
}

// TestStoresOpenSourceLinks drives /api/stores end-to-end against a store whose
// source.json points at a REAL git repo: RepoAbs is always the repo dir, and
// GithubBase tracks the live origin — a GitHub origin (either form) yields the
// blob base; a non-GitHub origin yields none; neither is ever an error.
func TestStoresOpenSourceLinks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	s, err := store.Open(root, "linked")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Merge(&schema.Batch{Producer: "code", Nodes: []schema.Node{
		{ID: "a.go::F", Label: "F", Kind: "function", FileType: "go", Source: "a.go"},
	}}); err != nil {
		t.Fatal(err)
	}
	// A real repo dir with a git history, so gitinfo.Remote/branch resolve.
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("-c", "init.defaultBranch=main", "init")
	git("remote", "add", "origin", "git@github.com:owner/repo.git")
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "init")
	// Point the store at that repo (freshness.Source shape: [{path}]).
	if err := os.WriteFile(filepath.Join(root, "linked", "source.json"),
		[]byte(`[{"path":`+strconvQuote(repo)+`}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	linksFor := func() *StoreLinks {
		var stores []StoreInfo
		json.Unmarshal(get(t, srvURL(t, root)+"/api/stores", 200), &stores)
		for _, st := range stores {
			if st.Key == "linked" {
				return st.Links
			}
		}
		t.Fatal("store 'linked' not listed")
		return nil
	}

	// scp-style github origin → repo dir + blob base.
	l := linksFor()
	if l == nil || l.RepoAbs != repo {
		t.Fatalf("repo_abs: %+v (want %s)", l, repo)
	}
	if l.GithubBase != "https://github.com/owner/repo/blob/main" {
		t.Fatalf("github_base (scp origin): %q", l.GithubBase)
	}

	// https origin → same blob base.
	git("remote", "set-url", "origin", "https://github.com/owner/repo.git")
	if l := linksFor(); l.GithubBase != "https://github.com/owner/repo/blob/main" {
		t.Fatalf("github_base (https origin): %q", l.GithubBase)
	}

	// non-github origin → repo dir but NO github base, still no error.
	git("remote", "set-url", "origin", "git@gitlab.com:owner/repo.git")
	if l := linksFor(); l.RepoAbs != repo || l.GithubBase != "" {
		t.Fatalf("non-github links: %+v", l)
	}
}

// srvURL spins a throwaway handler over root for a single assertion block.
func srvURL(t *testing.T, root string) string {
	t.Helper()
	srv := httptest.NewServer(NewHandler(root, nil))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestSetupEndpoint(t *testing.T) {
	srv, root := testServer(t)
	// No repo grammars leak in from the machine: pin the global grammar dir.
	t.Setenv("CTX_OPTIMIZE_GRAMMARS", filepath.Join(root, "no-such-grammars"))

	var setup struct {
		Global struct {
			File string `json:"file"`
		} `json:"global"`
		Effective []configKV       `json:"effective"`
		Axes      []map[string]any `json:"axes"`
	}
	json.Unmarshal(get(t, srv.URL+"/api/setup", 200), &setup)
	if setup.Global.File == "" || len(setup.Effective) != 3 {
		t.Fatalf("setup: %+v", setup)
	}
	for _, kv := range setup.Effective {
		if kv.Value != "ALL" || kv.Source != "default" {
			t.Fatalf("effective default: %+v", kv)
		}
	}
	axes := map[string]bool{}
	for _, a := range setup.Axes {
		axes[a["axis"].(string)] = true
	}
	for _, want := range []string{"grammars", "routes", "manifests", "adapters"} {
		if !axes[want] {
			t.Fatalf("axis %s missing: %v", want, axes)
		}
	}

	// With a repo path: project half + adapters axis + the repo's OWN route
	// pack all show. Pin the store root so machine packs stay out.
	t.Setenv("CTX_OPTIMIZE_STORE", filepath.Join(root, "no-machine-packs"))
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".ctxoptimize", "adapters"), 0o755)
	os.WriteFile(filepath.Join(repo, ".ctxoptimize", "adapters", "kafka.js"), []byte("//"), 0o644)
	os.MkdirAll(filepath.Join(repo, ".ctxoptimize", "routes"), 0o755)
	os.WriteFile(filepath.Join(repo, ".ctxoptimize", "routes", "myroutes.json"),
		[]byte(`{"name":"myroutes","rules":[{"call":"registerRoute","path_arg":0,"handler_arg":1,"method":"GET"}]}`), 0o644)
	body := get(t, srv.URL+"/api/setup?path="+url.QueryEscape(repo), 200)
	if !strings.Contains(string(body), "kafka") {
		t.Fatalf("setup with path missing adapter: %s", body)
	}
	// The project-scoped route pack surfaces under the routes axis.
	var withPath struct {
		Axes []struct {
			Axis  string           `json:"axis"`
			Core  []string         `json:"core"`
			Packs []map[string]any `json:"packs"`
		} `json:"axes"`
	}
	json.Unmarshal(body, &withPath)
	foundPack, foundCore := false, false
	for _, a := range withPath.Axes {
		if a.Axis != "routes" {
			continue
		}
		if len(a.Core) > 0 {
			foundCore = true
		}
		for _, p := range a.Packs {
			if p["name"] == "myroutes" {
				foundPack = true
			}
		}
	}
	if !foundPack {
		t.Fatalf("project route pack missing from setup: %s", body)
	}
	if !foundCore {
		t.Fatalf("routes axis missing core recognizer list: %s", body)
	}
}
