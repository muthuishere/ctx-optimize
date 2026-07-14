package markdown

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// yamlRouteIndex collects route nodes, contains edges into routes, and
// handles edges from a batch.
func yamlRouteIndex(t *testing.T, b *schema.Batch) (nodes map[string]schema.Node, contains map[string]bool, handles map[string]string) {
	t.Helper()
	nodes = map[string]schema.Node{}
	for _, n := range b.Nodes {
		if n.Kind == "route" {
			nodes[n.ID] = n
		}
	}
	contains = map[string]bool{}
	handles = map[string]string{}
	for _, e := range b.Edges {
		switch e.Relation {
		case "contains":
			if _, ok := nodes[e.Target]; ok {
				if e.Confidence != schema.Extracted {
					t.Errorf("contains %s→%s confidence = %s, want EXTRACTED", e.Source, e.Target, e.Confidence)
				}
				contains[e.Source+"→"+e.Target] = true
			}
		case "handles":
			if e.Confidence != schema.Inferred {
				t.Errorf("handles %s→%s confidence = %s, want INFERRED", e.Source, e.Target, e.Confidence)
			}
			handles[e.Source+"→"+e.Target] = e.Metadata["synthesized_by"]
		}
	}
	return nodes, contains, handles
}

func extractOne(t *testing.T, name, content string) *schema.Batch {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("batch failed the door: %v", err)
	}
	return b
}

func TestOpenAPIRoutes(t *testing.T) {
	b := extractOne(t, "openapi.yaml", `openapi: "3.0.0"
info:
  title: Users API
paths:
  /users/{id}:
    get:
      operationId: read_user
      summary: One user
    delete:
      summary: No operationId here
  /items:
    post:
      operationId: create_item
components:
  schemas:
    User:
      type: object
`)
	nodes, contains, handles := yamlRouteIndex(t, b)
	want := map[string]string{
		"openapi.yaml::route:GET /users/{id}":    "GET /users/{id}",
		"openapi.yaml::route:DELETE /users/{id}": "DELETE /users/{id}",
		"openapi.yaml::route:POST /items":        "POST /items",
	}
	for id, label := range want {
		n, ok := nodes[id]
		if !ok {
			t.Errorf("missing route node %s", id)
			continue
		}
		if n.Label != label || n.FileType != "config" || n.Metadata["synthesized_by"] != "openapi-route" {
			t.Errorf("%s = %q/%s/%s", id, n.Label, n.FileType, n.Metadata["synthesized_by"])
		}
		if !contains["openapi.yaml→"+id] {
			t.Errorf("missing contains edge doc→%s", id)
		}
	}
	if len(nodes) != len(want) {
		t.Errorf("route count = %d, want %d: %v", len(nodes), len(want), nodes)
	}
	// Locations point at the method lines.
	if loc := nodes["openapi.yaml::route:GET /users/{id}"].Location; loc != "L6" {
		t.Errorf("GET location = %s, want L6", loc)
	}
	wantHandles := map[string]string{
		"openapi.yaml::route:GET /users/{id}→read_user": "openapi-route",
		"openapi.yaml::route:POST /items→create_item":   "openapi-route",
	}
	for k, ch := range wantHandles {
		if handles[k] != ch {
			t.Errorf("handles %s = %q, want %q", k, handles[k], ch)
		}
	}
	if len(handles) != len(wantHandles) {
		t.Errorf("handles count = %d, want %d: %v", len(handles), len(wantHandles), handles)
	}
}

func TestDrupalRoutingRoutes(t *testing.T) {
	b := extractOne(t, "mymodule.routing.yml", `mymodule.content:
  path: '/mymodule/content'
  defaults:
    _controller: 'Drupal\mymodule\Controller\ContentController::render'
    _title: 'Content'
  requirements:
    _permission: 'access content'
mymodule.api:
  path: '/mymodule/api'
  methods: [GET, POST]
  defaults:
    _controller: 'Drupal\mymodule\Controller\ApiController::handle'
mymodule.noroute:
  defaults:
    _title: 'No path here'
`)
	nodes, contains, handles := yamlRouteIndex(t, b)
	want := map[string]string{
		"mymodule.routing.yml::route:ROUTE /mymodule/content": "ROUTE /mymodule/content",
		"mymodule.routing.yml::route:GET /mymodule/api":       "GET /mymodule/api",
		"mymodule.routing.yml::route:POST /mymodule/api":      "POST /mymodule/api",
	}
	for id, label := range want {
		n, ok := nodes[id]
		if !ok {
			t.Errorf("missing route node %s", id)
			continue
		}
		if n.Label != label || n.Metadata["synthesized_by"] != "drupal-route" {
			t.Errorf("%s = %q/%s", id, n.Label, n.Metadata["synthesized_by"])
		}
		if !contains["mymodule.routing.yml→"+id] {
			t.Errorf("missing contains edge doc→%s", id)
		}
	}
	if len(nodes) != len(want) {
		t.Errorf("route count = %d, want %d: %v", len(nodes), len(want), nodes)
	}
	wantHandles := map[string]string{
		"mymodule.routing.yml::route:ROUTE /mymodule/content→render": "drupal-route",
		"mymodule.routing.yml::route:GET /mymodule/api→handle":       "drupal-route",
		"mymodule.routing.yml::route:POST /mymodule/api→handle":      "drupal-route",
	}
	for k, ch := range wantHandles {
		if handles[k] != ch {
			t.Errorf("handles %s = %q, want %q", k, handles[k], ch)
		}
	}
	if len(handles) != len(wantHandles) {
		t.Errorf("handles count = %d, want %d: %v", len(handles), len(wantHandles), handles)
	}
}

func TestIngressRoutes(t *testing.T) {
	b := extractOne(t, "ingress.yaml", `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: web
spec:
  rules:
  - host: example.com
    http:
      paths:
      - path: /api
        pathType: Prefix
        backend:
          service:
            name: api-svc
            port:
              number: 80
      - path: /web
        pathType: Prefix
        backend:
          service:
            name: web-svc
            port:
              number: 80
`)
	nodes, contains, handles := yamlRouteIndex(t, b)
	want := map[string]string{
		"ingress.yaml::route:ROUTE /api": "ROUTE /api",
		"ingress.yaml::route:ROUTE /web": "ROUTE /web",
	}
	for id, label := range want {
		n, ok := nodes[id]
		if !ok {
			t.Errorf("missing route node %s", id)
			continue
		}
		if n.Label != label || n.Metadata["synthesized_by"] != "ingress-route" {
			t.Errorf("%s = %q/%s", id, n.Label, n.Metadata["synthesized_by"])
		}
		if !contains["ingress.yaml→"+id] {
			t.Errorf("missing contains edge doc→%s", id)
		}
	}
	if len(nodes) != len(want) {
		t.Errorf("route count = %d, want %d: %v", len(nodes), len(want), nodes)
	}
	wantHandles := map[string]string{
		"ingress.yaml::route:ROUTE /api→api-svc": "ingress-route",
		"ingress.yaml::route:ROUTE /web→web-svc": "ingress-route",
	}
	for k, ch := range wantHandles {
		if handles[k] != ch {
			t.Errorf("handles %s = %q, want %q", k, handles[k], ch)
		}
	}
	if len(handles) != len(wantHandles) {
		t.Errorf("handles count = %d, want %d: %v", len(handles), len(wantHandles), handles)
	}
}

// A multi-document file where only one document is an Ingress: routes come
// from that document alone.
func TestIngressMultiDoc(t *testing.T) {
	b := extractOne(t, "stack.yaml", `apiVersion: v1
kind: Service
metadata:
  name: api-svc
spec:
  ports:
  - path: /not-an-ingress-path
---
kind: Ingress
spec:
  rules:
  - http:
      paths:
      - path: /only
        backend:
          service:
            name: api-svc
`)
	nodes, _, _ := yamlRouteIndex(t, b)
	if len(nodes) != 1 {
		t.Fatalf("route count = %d, want 1: %v", len(nodes), nodes)
	}
	if _, ok := nodes["stack.yaml::route:ROUTE /only"]; !ok {
		t.Errorf("missing ROUTE /only: %v", nodes)
	}
}

// False-positive guards: plain data/config yaml must yield ZERO route nodes —
// docker-compose, Taskfile, goreleaser-style files all have path-ish keys.
func TestYAMLNoFalsePositiveRoutes(t *testing.T) {
	files := map[string]string{
		"docker-compose.yml": `version: "3"
services:
  web:
    image: nginx
    ports:
      - "8080:80"
    volumes:
      - ./web:/usr/share/nginx/html
  api:
    build:
      context: .
      dockerfile: Dockerfile
`,
		"Taskfile.yml": `version: "3"
tasks:
  ci:
    cmds:
      - go vet ./...
      - go test ./...
`,
		"release.yaml": `project_name: ctx-optimize
builds:
  - id: default
    main: ./cmd/ctx-optimize
    binary: ctx-optimize
paths:
  /this/is/not: openapi
`,
	}
	for name, content := range files {
		t.Run(name, func(t *testing.T) {
			b := extractOne(t, name, content)
			for _, n := range b.Nodes {
				if n.Kind == "route" {
					t.Errorf("unexpected route node %s in %s", n.ID, name)
				}
			}
			for _, e := range b.Edges {
				if e.Relation == "handles" {
					t.Errorf("unexpected handles edge %s→%s in %s", e.Source, e.Target, name)
				}
			}
		})
	}
}

// Zero route hits across this repo's own tree (Taskfile.yml, .goreleaser.yaml,
// openspec docs…) — the measured false-positive check, config-lane edition.
func TestNoYAMLRoutesInThisRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("walks the whole repo")
	}
	b, err := Extract(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range b.Nodes {
		if n.Kind == "route" {
			t.Errorf("unexpected route node in own repo: %s (%s)", n.ID, n.Label)
		}
	}
	for _, e := range b.Edges {
		if e.Relation == "handles" {
			t.Errorf("unexpected handles edge in own repo: %s→%s", e.Source, e.Target)
		}
	}
}
