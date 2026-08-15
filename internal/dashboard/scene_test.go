package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

// sceneServer builds a store with a real two-directory flow plus a SENSITIVE
// port whose metadata also carries a planted fake value, so the no-value gate
// has something to catch if the handler ever widens.
func sceneServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	s, err := store.Open(root, "myrepo")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Merge(&schema.Batch{
		Producer: "test",
		Nodes: []schema.Node{
			{ID: "api/h.go", Label: "h.go", Kind: "file", FileType: "code", Source: "api/h.go"},
			{ID: "api/h.go::Serve", Label: "Serve", Kind: "function", FileType: "code", Source: "api/h.go", Location: "L1-L9"},
			{ID: "db/d.go", Label: "d.go", Kind: "file", FileType: "code", Source: "db/d.go"},
			{ID: "db/d.go::Open", Label: "Open", Kind: "function", FileType: "code", Source: "db/d.go", Location: "L1-L9"},
			{ID: "port:config.env:>PGPASSWORD", Label: "PGPASSWORD", Kind: "port", FileType: "boundary",
				Source: "port://config.env/PGPASSWORD",
				Metadata: map[string]string{
					"transport": "config.env", "direction": "consumes",
					"sensitive": "true", "identifier": "PGPASSWORD",
					// a value-shaped field planted on the NODE: it must not travel
					"org.example": "hunter2-NOT-A-REAL-SECRET",
				}},
		},
		Edges: []schema.Edge{
			{Source: "api/h.go", Target: "db/d.go", Relation: "calls", Confidence: schema.Inferred},
			{Source: "db/d.go", Target: "port:config.env:>PGPASSWORD", Relation: "consumes", Confidence: schema.Inferred},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(root, nil))
	t.Cleanup(srv.Close)
	return srv, root
}

// TestSceneEndpointDrawsTheRealFlow: expectations written by hand from
// sceneServer's batch above, not captured from the endpoint.
func TestSceneEndpointDrawsTheRealFlow(t *testing.T) {
	srv, _ := sceneServer(t)
	var sc struct {
		Cards []struct {
			ID    string `json:"id"`
			Layer int    `json:"layer"`
			Hub   bool   `json:"hub"`
		} `json:"cards"`
		Links []struct {
			From, To, Label string
			Weight          int
		} `json:"links"`
		World []struct {
			Transport string `json:"transport"`
			Sensitive int    `json:"sensitive"`
			Sample    []struct {
				Label     string `json:"label"`
				Sensitive bool   `json:"sensitive"`
			} `json:"sample"`
		} `json:"world"`
		Notes []string `json:"notes"`
	}
	if err := json.Unmarshal(get(t, srv.URL+"/api/scene?module=myrepo", 200), &sc); err != nil {
		t.Fatal(err)
	}
	if len(sc.Cards) != 2 {
		t.Fatalf("cards = %d, want 2 (api, db)", len(sc.Cards))
	}
	var api, db = -1, -1
	for i, c := range sc.Cards {
		switch c.ID {
		case "api":
			api = i
		case "db":
			db = i
		}
	}
	if api < 0 || db < 0 {
		t.Fatalf("cards = %+v, want api and db", sc.Cards)
	}
	if sc.Cards[api].Layer >= sc.Cards[db].Layer {
		t.Errorf("api calls db, so api must be left of it: L%d vs L%d",
			sc.Cards[api].Layer, sc.Cards[db].Layer)
	}
	if !sc.Cards[db].Hub {
		t.Errorf("db is called and calls nothing — it must be the hub: %+v", sc.Cards[db])
	}
	var codeLink, worldLink bool
	for _, l := range sc.Links {
		if l.From == "api" && l.To == "db" && l.Label == "CALLS" && l.Weight == 1 {
			codeLink = true
		}
		if l.From == "db" && l.To == "world:config.env" && l.Label == "CONSUMES" {
			worldLink = true
		}
	}
	if !codeLink {
		t.Errorf("no CALLS api->db link drawn: %+v", sc.Links)
	}
	if !worldLink {
		t.Errorf("no CONSUMES db->config.env link drawn: %+v", sc.Links)
	}
	if len(sc.World) != 1 || sc.World[0].Transport != "config.env" || sc.World[0].Sensitive != 1 {
		t.Fatalf("world = %+v, want one config.env group with 1 sensitive", sc.World)
	}
	if len(sc.World[0].Sample) != 1 || sc.World[0].Sample[0].Label != "PGPASSWORD" || !sc.World[0].Sample[0].Sensitive {
		t.Errorf("door sample = %+v, want the flagged NAME PGPASSWORD", sc.World[0].Sample)
	}
	if len(sc.Notes) == 0 {
		t.Error("the scene must print what it is sampling")
	}
}

// TestSceneEndpointHasNoValueKey walks the WHOLE decoded response and fails on
// any key that could carry a credential, and on the planted fake secret.
func TestSceneEndpointHasNoValueKey(t *testing.T) {
	srv, _ := sceneServer(t)
	body := get(t, srv.URL+"/api/scene?module=myrepo", 200)
	if strings.Contains(string(body), "hunter2") {
		t.Fatal("a value-shaped node field reached the /api/scene payload")
	}
	var v interface{}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	walkNoValue(t, v, "$")
	// and the NAME is there — the gate must not pass by shipping nothing
	if !strings.Contains(string(body), "PGPASSWORD") {
		t.Fatal("port names missing: the endpoint would pass the no-value gate by saying nothing")
	}
}

var banned = map[string]bool{
	"value": true, "values": true, "secret": true, "secrets": true,
	"password": true, "token": true, "credential": true, "credentials": true,
	"dsn": true, "url": true, "uri": true, "conn": true, "connection_string": true,
}

func walkNoValue(t *testing.T, v interface{}, path string) {
	t.Helper()
	switch x := v.(type) {
	case map[string]interface{}:
		for k, sub := range x {
			if banned[strings.ToLower(k)] {
				t.Errorf("%s.%s: /api/scene must never carry a key named %q", path, k, k)
			}
			walkNoValue(t, sub, path+"."+k)
		}
	case []interface{}:
		for _, sub := range x {
			walkNoValue(t, sub, path+"[]")
		}
	}
}

// TestSceneEndpointDoesNotCreateStore: the read path must never lay out a
// store dir for a key that does not exist.
func TestSceneEndpointDoesNotCreateStore(t *testing.T) {
	srv, root := sceneServer(t)
	get(t, srv.URL+"/api/scene?module=ghost", 404)
	if _, err := os.Stat(filepath.Join(root, "ghost")); !os.IsNotExist(err) {
		t.Fatalf("/api/scene created %s (err=%v) — reads must never create store dirs",
			filepath.Join(root, "ghost"), err)
	}
	// and a traversal key must not escape the root either
	get(t, srv.URL+"/api/scene?module=..%2F..%2Fetc", 404)
}

// TestSceneEndpointIsAReadRoute: answers without X-Ctx-Token, and writes no
// audit row. This pins the posture so a later refactor cannot quietly make a
// read route mutable.
func TestSceneEndpointIsAReadRoute(t *testing.T) {
	srv, root := sceneServer(t)
	resp, err := http.Get(srv.URL + "/api/scene?module=myrepo")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d without a token, want 200 (this is a read route)", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(root, "audit.ndjson")); err == nil {
		b, _ := os.ReadFile(filepath.Join(root, "audit.ndjson"))
		if len(strings.TrimSpace(string(b))) > 0 {
			t.Fatalf("/api/scene wrote audit rows — a read must not be audited:\n%s", b)
		}
	}
}
