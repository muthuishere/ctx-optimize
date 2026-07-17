package connectors

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/sources"
)

// petstoreV3 is a small OpenAPI 3 spec with a planted security-scheme example
// value that must NEVER reach the store (M6).
const petstoreV3 = `{
  "openapi": "3.0.3",
  "info": {"title": "Petstore", "version": "1.2.0"},
  "paths": {
    "/pets": {
      "parameters": [{"name": "tenant", "in": "header"}],
      "get": {
        "summary": "List pets",
        "parameters": [{"name": "limit", "in": "query"}],
        "responses": {"200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Pet"}}}}}
      },
      "post": {
        "requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/NewPet"}}}},
        "responses": {"201": {"description": "created"}, "400": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Error"}}}}}
      }
    },
    "/health": {"get": {"summary": "Ping", "responses": {"200": {"description": "ok"}}}}
  },
  "components": {
    "schemas": {
      "Pet": {"type": "object", "properties": {"name": {"type": "string"}, "id": {"type": "integer"}, "owner": {"$ref": "#/components/schemas/Owner"}}},
      "NewPet": {"type": "object", "properties": {"name": {"type": "string"}}},
      "Error": {"type": "object", "properties": {"message": {"type": "string"}}},
      "Owner": {"type": "object"}
    },
    "securitySchemes": {
      "apiKey": {"type": "apiKey", "in": "header", "name": "X-API-Key", "example": "sekret-example-token-9911", "x-example": "sekret-example-token-9911"}
    }
  }
}`

const petstoreV2 = `{
  "swagger": "2.0",
  "info": {"title": "Legacy", "version": "0.9"},
  "paths": {
    "/orders": {
      "post": {
        "parameters": [{"name": "body", "in": "body", "schema": {"$ref": "#/definitions/Order"}}],
        "responses": {"200": {"schema": {"$ref": "#/definitions/Order"}}}
      }
    }
  },
  "definitions": {"Order": {"type": "object", "properties": {"sku": {"type": "string"}}}},
  "securityDefinitions": {"basic": {"type": "basic", "example": "hunter2-planted"}}
}`

func captureOpenAPI(t *testing.T, value string) *schema.Batch {
	t.Helper()
	b, err := openapiConnector{}.Capture(context.Background(), value)
	if err != nil {
		t.Fatalf("Capture(%s): %v", value, err)
	}
	b.Producer = openapiProducer
	if err := b.Validate(); err != nil {
		t.Fatalf("batch invalid: %v", err)
	}
	return b
}

func batchJSON(t *testing.T, b *schema.Batch) string {
	t.Helper()
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func nodeByID(b *schema.Batch, id string) *schema.Node {
	for i := range b.Nodes {
		if b.Nodes[i].ID == id {
			return &b.Nodes[i]
		}
	}
	return nil
}

func hasEdge(b *schema.Batch, src, dst, rel string) bool {
	for _, e := range b.Edges {
		if e.Source == src && e.Target == dst && e.Relation == rel {
			return true
		}
	}
	return false
}

func TestOpenAPICaptureV3(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(petstoreV3))
	}))
	defer ts.Close()

	start := time.Now()
	// The raw value carries query params that MUST be stripped from ids (M2).
	b := captureOpenAPI(t, ts.URL+"/openapi.json?token=SHOULDSTRIP&sig=alsostrip")
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("hermetic capture took %v, want < 50ms", d)
	}

	base := ts.URL + "/openapi.json"
	root := nodeByID(b, base)
	if root == nil {
		t.Fatalf("no api root node %q; ids: %v", base, nodeIDs(b))
	}
	if root.Kind != "api" || root.Metadata["title"] != "Petstore" || root.Metadata["version"] != "1.2.0" {
		t.Errorf("root = %+v", root)
	}
	pathID := base + "#/paths/pets"
	opID := pathID + "/get"
	petID := base + "#/components/schemas/Pet"
	for _, id := range []string{pathID, opID, petID, base + "#/paths/health/get"} {
		if nodeByID(b, id) == nil {
			t.Errorf("missing node %q", id)
		}
	}
	op := nodeByID(b, opID)
	if op.Metadata["summary"] != "List pets" {
		t.Errorf("op summary = %q", op.Metadata["summary"])
	}
	if got := op.Metadata["params"]; got != "tenant, limit" {
		t.Errorf("op params = %q, want path-level + op-level", got)
	}
	if got := op.Metadata["schemas"]; got != "Pet" {
		t.Errorf("op schemas = %q", got)
	}
	pet := nodeByID(b, petID)
	if got := pet.Metadata["properties"]; got != "id:integer, name:string, owner:Owner" {
		t.Errorf("Pet properties = %q (want sorted name:type)", got)
	}
	if !hasEdge(b, base, pathID, "contains") || !hasEdge(b, pathID, opID, "contains") {
		t.Error("contains chain api→path→operation missing")
	}
	if !hasEdge(b, opID, petID, "uses") {
		t.Error("GET /pets → Pet uses edge missing")
	}
	if !hasEdge(b, pathID+"/post", base+"#/components/schemas/NewPet", "uses") {
		t.Error("POST /pets → NewPet uses edge missing")
	}

	out := batchJSON(t, b)
	if strings.Contains(out, "SHOULDSTRIP") || strings.Contains(out, "alsostrip") {
		t.Error("query params leaked into stored ids — https ids must strip ALL query params")
	}
	if strings.Contains(out, "sekret-example-token-9911") {
		t.Error("security-scheme example value leaked into the batch")
	}
	if sec := nodeByID(b, base+"#/securitySchemes/apiKey"); sec == nil {
		t.Error("securityScheme node missing")
	} else if sec.Metadata["type"] != "apiKey" || sec.Metadata["name"] != "X-API-Key" {
		t.Errorf("securityScheme metadata = %v", sec.Metadata)
	}
}

func TestOpenAPICaptureSwagger2(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(petstoreV2))
	}))
	defer ts.Close()

	b := captureOpenAPI(t, ts.URL+"/swagger.json")
	base := ts.URL + "/swagger.json"
	if root := nodeByID(b, base); root == nil || root.Metadata["spec"] != "swagger 2.0" {
		t.Fatalf("swagger root = %+v", root)
	}
	orderID := base + "#/definitions/Order"
	if nodeByID(b, orderID) == nil {
		t.Fatalf("missing definitions node; ids: %v", nodeIDs(b))
	}
	if !hasEdge(b, base+"#/paths/orders/post", orderID, "uses") {
		t.Error("swagger body-param $ref uses edge missing")
	}
	if strings.Contains(batchJSON(t, b), "hunter2-planted") {
		t.Error("swagger securityDefinitions example leaked")
	}
}

func TestOpenAPIBasicAuth(t *testing.T) {
	const user, pass = "svc-reader", "basic-pw-3141"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != pass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(petstoreV3))
	}))
	defer ts.Close()

	hostPart := strings.TrimPrefix(ts.URL, "http://")
	b := captureOpenAPI(t, "http://"+user+":"+pass+"@"+hostPart+"/spec.json")
	out := batchJSON(t, b)
	if strings.Contains(out, pass) || strings.Contains(out, user+":") {
		t.Error("userinfo leaked into the batch")
	}
	if nodeByID(b, "http://"+hostPart+"/spec.json") == nil {
		t.Errorf("root id must be the sanitized URL (userinfo stripped); ids: %v", nodeIDs(b))
	}

	// Wrong password: the error must name the sanitized URL, never the creds.
	_, err := openapiConnector{}.Capture(context.Background(), "http://"+user+":wrong-pw-secret@"+hostPart+"/spec.json")
	if err == nil {
		t.Fatal("expected HTTP 401 error")
	}
	if strings.Contains(err.Error(), "wrong-pw-secret") || strings.Contains(err.Error(), user) {
		t.Errorf("credentials leaked into error: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should carry the status: %v", err)
	}
}

func TestOpenAPINonSpecContent(t *testing.T) {
	cases := []struct {
		name, body, wantHint string
	}{
		{"html", "<html><body>login</body></html>", "adapter-script lane"},
		{"yaml", "openapi: 3.0.0\ninfo:\n  title: Y\npaths: {}\n", "YAML"},
		{"json-not-spec", `{"hello": "world"}`, "adapter-script lane"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(c.body))
			}))
			defer ts.Close()
			_, err := openapiConnector{}.Capture(context.Background(), ts.URL+"/thing")
			if err == nil {
				t.Fatal("expected an error for non-spec content")
			}
			if !strings.Contains(err.Error(), c.wantHint) {
				t.Errorf("error %q should mention %q", err, c.wantHint)
			}
			if c.name != "yaml" && !strings.Contains(err.Error(), "adapter-script lane") {
				t.Errorf("error must point at the adapter lane: %v", err)
			}
		})
	}
}

func TestOpenAPIFileLane(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(p, []byte(petstoreV3), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	b := captureOpenAPI(t, p)
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("file-lane capture took %v, want < 50ms", d)
	}
	id := filepath.ToSlash(p)
	if nodeByID(b, id) == nil {
		t.Fatalf("file-lane root id should be the path %q; ids: %v", id, nodeIDs(b))
	}
	// Route must send a no-scheme path to this connector.
	if name, err := sources.Route(p); err != nil || name != "openapi" {
		t.Errorf("sources.Route(%s) = %q, %v", p, name, err)
	}

	// Missing file is a plain loud error.
	if _, err := (openapiConnector{}).Capture(context.Background(), filepath.Join(dir, "nope.json")); err == nil {
		t.Error("expected error for a missing spec file")
	}
}

func TestOpenAPIDeterministic(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(petstoreV3))
	}))
	defer ts.Close()
	a := batchJSON(t, captureOpenAPI(t, ts.URL+"/o.json"))
	b := batchJSON(t, captureOpenAPI(t, ts.URL+"/o.json"))
	if a != b {
		t.Error("two captures of the same spec differ — output must be deterministic")
	}
}

func nodeIDs(b *schema.Batch) []string {
	ids := make([]string, len(b.Nodes))
	for i, n := range b.Nodes {
		ids[i] = n.ID
	}
	return ids
}
