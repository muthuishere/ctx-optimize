package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
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
	srv := httptest.NewServer(NewHandler(root))
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

func TestDashboard(t *testing.T) {
	srv, _ := testServer(t)

	// The page itself is embedded and self-contained.
	page := string(get(t, srv.URL+"/", 200))
	if !strings.Contains(page, "ctx-optimize") {
		t.Fatal("index page missing")
	}
	for _, external := range []string{"https://cdn", "http://cdn", "unpkg.com", "jsdelivr"} {
		if strings.Contains(page, external) {
			t.Fatalf("dashboard must not load external resources: found %s", external)
		}
	}

	// Modules list the store folders with counts.
	var mods []Module
	json.Unmarshal(get(t, srv.URL+"/api/modules", 200), &mods)
	if len(mods) != 1 || mods[0].Key != "myrepo" || mods[0].Nodes != 2 || mods[0].Edges != 1 {
		t.Fatalf("modules: %+v", mods)
	}

	// Graph returns the full ndjson content as JSON.
	var g struct {
		Nodes []schema.Node `json:"nodes"`
		Edges []schema.Edge `json:"edges"`
	}
	json.Unmarshal(get(t, srv.URL+"/api/graph?module=myrepo", 200), &g)
	if len(g.Nodes) != 2 || len(g.Edges) != 1 {
		t.Fatalf("graph: %d nodes %d edges", len(g.Nodes), len(g.Edges))
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
