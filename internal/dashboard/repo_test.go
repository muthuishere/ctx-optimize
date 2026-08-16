package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/navigator"
	"github.com/muthuishere/ctx-optimize/internal/scene"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

// repoServer builds the shape the spike found in 30 monorepos: a UI module
// that declares the package the API module publishes, plus one external
// package and one shared external host.
func repoServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	root := t.TempDir()

	write := func(key string, b *schema.Batch) {
		s, err := store.Open(root, key)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Merge(b); err != nil {
			t.Fatal(err)
		}
	}
	write("acme/apps/api", &schema.Batch{
		Producer: "test",
		Nodes: []schema.Node{
			{ID: "src/s.go", Label: "s.go", Kind: "file", FileType: "code", Source: "src/s.go"},
			{ID: "src/t.go", Label: "t.go", Kind: "file", FileType: "code", Source: "src/t.go"},
			{ID: "dep:npm/@acme/api", Label: "@acme/api", Kind: "dependency", FileType: "config", Source: "dep:npm/@acme/api"},
			{ID: "port:network.http:>api.openai.com", Label: "api.openai.com", Kind: "port", FileType: "boundary",
				Source:   "port://network.http/api.openai.com",
				Metadata: map[string]string{"transport": "network.http", "direction": "consumes", "identifier": "api.openai.com"}},
		},
		Edges: []schema.Edge{
			{Source: "package.json", Target: "dep:npm/@acme/api", Relation: "publishes", Confidence: schema.Extracted},
			{Source: "src/s.go", Target: "src/t.go", Relation: "imports", Confidence: schema.Extracted},
			{Source: "src/s.go", Target: "port:network.http:>api.openai.com", Relation: "consumes", Confidence: schema.Inferred},
		},
	})
	write("acme/apps/ui", &schema.Batch{
		Producer: "test",
		Nodes: []schema.Node{
			{ID: "src/a.ts", Label: "a.ts", Kind: "file", FileType: "code", Source: "src/a.ts"},
			{ID: "dep:npm/@acme/api", Label: "@acme/api", Kind: "dependency", FileType: "config", Source: "dep:npm/@acme/api"},
			{ID: "dep:npm/react", Label: "react", Kind: "dependency", FileType: "config", Source: "dep:npm/react"},
			{ID: "dep:npm/@acme/ui", Label: "@acme/ui", Kind: "dependency", FileType: "config", Source: "dep:npm/@acme/ui"},
			{ID: "port:network.http:>api.openai.com", Label: "api.openai.com", Kind: "port", FileType: "boundary",
				Source:   "port://network.http/api.openai.com",
				Metadata: map[string]string{"transport": "network.http", "direction": "consumes", "identifier": "api.openai.com"}},
		},
		Edges: []schema.Edge{
			{Source: "package.json", Target: "dep:npm/@acme/ui", Relation: "publishes", Confidence: schema.Extracted},
			{Source: "package.json", Target: "dep:npm/@acme/api", Relation: "declares", Confidence: schema.Extracted},
			{Source: "package.json", Target: "dep:npm/react", Relation: "declares", Confidence: schema.Extracted},
			{Source: "src/a.ts", Target: "dep:npm/react", Relation: "imports", Confidence: schema.Extracted},
		},
	})
	// The repo root needs a store dir for modules.json to live beside, exactly
	// as a real multi-module gather leaves it.
	if _, err := store.Open(root, "acme"); err != nil {
		t.Fatal(err)
	}
	idx := &navigator.Index{Version: 1, Root: "acme", Modules: []navigator.ModuleEntry{
		{Name: "api", Path: "apps/api", Store: "acme/apps/api", Nodes: 4, Edges: 3},
		{Name: "ui", Path: "apps/ui", Store: "acme/apps/ui", Nodes: 5, Edges: 4},
	}}
	if err := idx.Write(filepath.Join(root, "acme")); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(root, nil))
	t.Cleanup(srv.Close)
	return srv, root
}

type repoScene struct {
	Level  string `json:"level"`
	Module string `json:"module"`
	Cards  []struct {
		ID, Label, Dir, Detail string
		In, Out, Children      int
		Hub                    bool
	} `json:"cards"`
	Links []struct {
		From, To, Relation string
		Weight             int
	} `json:"links"`
	Notes []string `json:"notes"`
}

func getRepoScene(t *testing.T, srv *httptest.Server, repo string) repoScene {
	t.Helper()
	res, err := http.Get(srv.URL + "/api/repo/scene?repo=" + repo)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var sc repoScene
	if err := json.NewDecoder(res.Body).Decode(&sc); err != nil {
		t.Fatal(err)
	}
	return sc
}

// The end-to-end claim of ADR 22: a `declares` edge in one module store meets
// a `publishes` edge in another, and becomes a drawn arrow.
func TestRepoSceneDrawsTheCrossModuleDependency(t *testing.T) {
	srv, _ := repoServer(t)
	sc := getRepoScene(t, srv, "acme")
	if sc.Level != "module" {
		t.Fatalf("level = %q, want module", sc.Level)
	}
	if len(sc.Cards) != 2 {
		t.Fatalf("cards = %d, want 2", len(sc.Cards))
	}
	if len(sc.Links) != 1 {
		t.Fatalf("links = %+v, want exactly ui -> api", sc.Links)
	}
	l := sc.Links[0]
	if l.From != "acme/apps/ui" || l.To != "acme/apps/api" || l.Relation != "depends" || l.Weight != 1 {
		t.Fatalf("link = %+v, want ui -> api depends 1", l)
	}
	// Both modules consume api.openai.com, but a real dependency already says
	// something stronger about this pair — `shares` must not double the line.
	for _, l := range sc.Links {
		if l.Relation == "shares" {
			t.Fatalf("shares drawn over a depends: %+v", l)
		}
	}
	// react is declared by ui and published by nobody: it is an external
	// package and must never become a module.
	for _, c := range sc.Cards {
		if strings.Contains(c.ID, "react") {
			t.Fatalf("external package became a card: %+v", c)
		}
	}
}

// A card is a store key because that is what a click opens.
func TestRepoSceneCardIsAStoreKey(t *testing.T) {
	srv, root := repoServer(t)
	sc := getRepoScene(t, srv, "acme")
	for _, c := range sc.Cards {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(c.ID), "graph")); err != nil {
			t.Fatalf("card id %q does not name a store the viewer can open: %v", c.ID, err)
		}
	}
}

// A repo with no module index is not a monorepo, and saying so is the answer:
// an empty module scene would look like a broken feature.
func TestRepoSceneRefusesASingleStore(t *testing.T) {
	srv, _ := sceneServer(t)
	res, err := http.Get(srv.URL + "/api/repo/scene?repo=myrepo")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404 for a single store", res.StatusCode)
	}
}

// The read path must never create store layout for a name that does not exist,
// and must never be talked into walking out of the store root.
func TestRepoSceneIsReadOnlyAndTraversalSafe(t *testing.T) {
	srv, root := repoServer(t)
	for _, bad := range []string{"nope", "..", "../..", "acme/apps/ui"} {
		res, err := http.Get(srv.URL + "/api/repo/scene?repo=" + bad)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode == 200 {
			t.Fatalf("repo=%q returned a scene", bad)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "nope")); !os.IsNotExist(err) {
		t.Fatalf("read route created layout for a name that does not exist")
	}
}

// A re-gather of ONE module has to invalidate the repo picture. Keying the
// cache on the repo root alone would serve yesterday's arrows.
func TestRepoSceneCacheNoticesOneModuleChanging(t *testing.T) {
	srv, root := repoServer(t)
	if n := len(getRepoScene(t, srv, "acme").Links); n != 1 {
		t.Fatalf("links = %d, want 1", n)
	}
	// ui stops declaring the api package: the arrow must disappear.
	p := filepath.Join(root, "acme", "apps", "ui", "graph", "edges.ndjson")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if !strings.Contains(line, `"dep:npm/@acme/api"`) {
			kept = append(kept, line)
		}
	}
	if err := os.WriteFile(p, []byte(strings.Join(kept, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Counting links would not prove it: with the dependency gone, the two
	// modules are left sharing api.openai.com, so a `shares` link takes the
	// vacated slot and the total stays 1. The claim is about the DEPENDS arrow.
	after := getRepoScene(t, srv, "acme")
	for _, l := range after.Links {
		if l.Relation == "depends" {
			t.Fatalf("depends arrow survived the declaration being removed (%+v); the cache is keyed on the wrong thing", l)
		}
	}
	if len(after.Links) != 1 || after.Links[0].Relation != "shares" {
		t.Fatalf("links = %+v, want the pair to fall back to the shares link", after.Links)
	}
}

// A vendored manifest names an UPSTREAM package, not one this repo produces.
// Letting it publish would make third_party/goproxywss the repo-wide owner of
// github.com/elazarl/goproxy and draw every real consumer into a vendored
// copy — which is exactly the four "cross-module links" the spike found in
// agent-proxy and had to discount.
func TestRepoSceneVendoredModuleNeverOwnsAPackage(t *testing.T) {
	srv, root := repoServer(t)
	s, err := store.Open(root, "acme/third_party/goproxy")
	if err != nil {
		t.Fatal(err)
	}
	dep := "dep:go/github.com/elazarl/goproxy"
	if _, _, err := s.Merge(&schema.Batch{
		Producer: "test",
		Nodes: []schema.Node{
			{ID: "proxy.go", Label: "proxy.go", Kind: "file", FileType: "code", Source: "proxy.go"},
			{ID: dep, Label: "github.com/elazarl/goproxy", Kind: "dependency", FileType: "config", Source: dep},
		},
		Edges: []schema.Edge{
			{Source: "go.mod", Target: dep, Relation: "publishes", Confidence: schema.Extracted,
				Metadata: map[string]string{"vendored": "true"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// api declares the upstream package, as any real consumer would.
	api, err := store.Open(root, "acme/apps/api")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := api.Merge(&schema.Batch{
		Producer: "test2",
		Nodes:    []schema.Node{{ID: dep, Label: "github.com/elazarl/goproxy", Kind: "dependency", FileType: "config", Source: dep}},
		Edges: []schema.Edge{
			{Source: "go.mod", Target: dep, Relation: "declares", Confidence: schema.Extracted},
		},
	}); err != nil {
		t.Fatal(err)
	}
	idx := &navigator.Index{Version: 1, Root: "acme", Modules: []navigator.ModuleEntry{
		{Name: "api", Path: "apps/api", Store: "acme/apps/api"},
		{Name: "ui", Path: "apps/ui", Store: "acme/apps/ui"},
		{Name: "goproxy", Path: "third_party/goproxy", Store: "acme/third_party/goproxy"},
	}}
	if err := idx.Write(filepath.Join(root, "acme")); err != nil {
		t.Fatal(err)
	}
	sc := getRepoScene(t, srv, "acme")
	for _, l := range sc.Links {
		if strings.Contains(l.To, "third_party") {
			t.Fatalf("a vendored upstream copy was drawn as a module this repo depends on: %+v", l)
		}
	}
	marked := false
	for _, c := range sc.Cards {
		if c.ID == "acme/third_party/goproxy" && strings.Contains(c.Detail, "vendored") {
			marked = true
		}
	}
	if !marked {
		t.Fatalf("vendored module not marked on its card; cards = %+v", sc.Cards)
	}
}

// The port scan must find ports without depending on JSON key order.
func TestRepoSceneScanSurvivesKeyReordering(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nodes.ndjson")
	// `kind` last, `port` also appearing inside a label — both are cases a
	// substring-only reader would get wrong.
	body := `{"label":"port-forwarder","id":"src/x.go","source":"src/x.go","kind":"file"}
{"metadata":{"transport":"network.http"},"source":"port://network.http/example.com","label":"example.com","id":"port:network.http:>example.com","kind":"port"}
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var rm scene.RepoModule
	if !scanPorts(p, &rm) {
		t.Fatal("scanPorts reported failure on a readable file")
	}
	if len(rm.Ports) != 1 || rm.Ports[0] != "port:network.http:>example.com" {
		t.Fatalf("ports = %v, want exactly the one real port", rm.Ports)
	}
}
