package code

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// routeEdges maps "source→target" → synthesized_by for handles edges.
func routeIndex(t *testing.T, batch *schema.Batch) (map[string]schema.Node, map[string]string) {
	t.Helper()
	nodes := map[string]schema.Node{}
	for _, n := range batch.Nodes {
		if n.Kind == "route" {
			nodes[n.ID] = n
		}
	}
	edges := map[string]string{}
	for _, e := range batch.Edges {
		if e.Relation != "handles" {
			continue
		}
		if e.Confidence != "INFERRED" {
			t.Errorf("handles edge %s→%s confidence = %s, want INFERRED", e.Source, e.Target, e.Confidence)
		}
		edges[e.Source+"→"+e.Target] = e.Metadata["synthesized_by"]
	}
	return nodes, edges
}

// Framework route recognition: FastAPI, Flask, Express, NestJS. Each case
// asserts exact node ids/labels and handles edges (INFERRED + synthesized_by),
// plus that near-miss patterns stay silent.
func TestFrameworkRoutes(t *testing.T) {
	cases := []struct {
		name        string
		files       map[string]string
		wantNodes   map[string]string // route node id → label
		wantEdges   map[string]string // "route→handler" → synthesized_by
		absentNodes []string          // ids that must NOT exist
	}{
		{
			name: "fastapi",
			files: map[string]string{
				"api.py": `from fastapi import FastAPI, APIRouter

app = FastAPI()
router = APIRouter()

@app.get("/users/{id}")
def read_user(id: int):
    return id

@router.post("/items")
def create_item():
    pass

@app.get(DYNAMIC_PATH)
def not_literal():
    pass
`,
			},
			wantNodes: map[string]string{
				"api.py::route:GET /users/{id}": "GET /users/{id}",
				"api.py::route:POST /items":     "POST /items",
			},
			wantEdges: map[string]string{
				"api.py::route:GET /users/{id}→api.py::read_user": "fastapi-route",
				"api.py::route:POST /items→api.py::create_item":   "fastapi-route",
			},
		},
		{
			name: "flask",
			files: map[string]string{
				"web.py": `from flask import Flask, Blueprint

app = Flask(__name__)
bp = Blueprint("bp", __name__)

@app.route("/health")
def health():
    return "ok"

@bp.route("/orders", methods=["GET", "POST"])
def orders():
    pass

@app.route("/bad", methods=[SOME_VERBS])
def bad():
    pass
`,
			},
			wantNodes: map[string]string{
				"web.py::route:GET /health":  "GET /health",
				"web.py::route:GET /orders":  "GET /orders",
				"web.py::route:POST /orders": "POST /orders",
			},
			wantEdges: map[string]string{
				"web.py::route:GET /health→web.py::health":  "flask-route",
				"web.py::route:GET /orders→web.py::orders":  "flask-route",
				"web.py::route:POST /orders→web.py::orders": "flask-route",
			},
			absentNodes: []string{"web.py::route:GET /bad"},
		},
		{
			name: "express",
			files: map[string]string{
				"server.js": `const express = require('express');
const app = express();
const router = express.Router();

app.get('/users/:id', auth, getUser);
router.post('/items', createItem);
app.delete('/items/:id', (req, res) => { res.send('gone'); });
app.get('/dup', twice);
app.put(pathVar, updateItem);

function getUser(req, res) {}
function createItem(req, res) {}
function updateItem(req, res) {}
function auth(req, res, next) { next(); }
`,
				// "twice" exists in two OTHER files → ambiguous → edge dropped.
				"a.js": "function twice() {}\n",
				"b.js": "function twice() {}\n",
			},
			wantNodes: map[string]string{
				"server.js::route:GET /users/:id":    "GET /users/:id",
				"server.js::route:POST /items":       "POST /items",
				"server.js::route:DELETE /items/:id": "DELETE /items/:id",
				"server.js::route:GET /dup":          "GET /dup",
			},
			wantEdges: map[string]string{
				"server.js::route:GET /users/:id→server.js::getUser": "express-route",
				"server.js::route:POST /items→server.js::createItem": "express-route",
			},
			absentNodes: []string{"server.js::route:PUT /items/:id"},
		},
		{
			name: "nestjs",
			files: map[string]string{
				"users.controller.ts": `import { Controller, Get, Post, Delete } from '@nestjs/common';

@Controller('users')
export class UsersController {
  @Get(':id')
  findOne(id: string) { return id; }

  @Post()
  create() {}

  @Delete(':id')
  remove(id: string) {}
}

@Controller()
export class RootController {
  @Get('health')
  health() { return 'ok'; }
}

export class NotAController {
  @Get('nope')
  orphan() {}
}
`,
			},
			wantNodes: map[string]string{
				"users.controller.ts::route:GET /users/:id":    "GET /users/:id",
				"users.controller.ts::route:POST /users":       "POST /users",
				"users.controller.ts::route:DELETE /users/:id": "DELETE /users/:id",
				"users.controller.ts::route:GET /health":       "GET /health",
			},
			wantEdges: map[string]string{
				"users.controller.ts::route:GET /users/:id→users.controller.ts::UsersController.findOne":   "nestjs-route",
				"users.controller.ts::route:POST /users→users.controller.ts::UsersController.create":       "nestjs-route",
				"users.controller.ts::route:DELETE /users/:id→users.controller.ts::UsersController.remove": "nestjs-route",
				"users.controller.ts::route:GET /health→users.controller.ts::RootController.health":        "nestjs-route",
			},
			absentNodes: []string{"users.controller.ts::route:GET /nope", "users.controller.ts::route:GET nope"},
		},
		{
			// Near-miss guard: patterns that LOOK like routes but are not.
			// Documented boundaries: Map.get lookalikes fail the ≥2-args or
			// receiver-name gate; identifier-callee decorators (@cached) and
			// non-verb attribute decorators (@obj.fetch) never match; a plain
			// X.route(...) CALL (not a decorator) never matches.
			name: "false-positive-guard",
			files: map[string]string{
				"notroutes.js": `const cache = new Map();
const v = cache.get('/users');
app.get('setting');
store.get('/key', fallback);
const tpl = router.get;

function fallback() {}
`,
				"notroutes.py": `class Registry:
    def route(self, path):
        return path

r = Registry()
r.route("/x")

@cached("/ttl")
def compute():
    pass

@obj.fetch("/data")
def fetch_data():
    pass
`,
				"notroutes.ts": `export class Widget {
  @Memoize(':id')
  render(id: string) { return id; }
}
`,
			},
			wantNodes: map[string]string{},
			wantEdges: map[string]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range tc.files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			batch, err := Extract(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := batch.Validate(); err != nil {
				t.Fatalf("batch failed the door: %v", err)
			}
			nodes, edges := routeIndex(t, batch)
			for id, label := range tc.wantNodes {
				n, ok := nodes[id]
				if !ok {
					t.Errorf("missing route node %s", id)
					continue
				}
				if n.Label != label {
					t.Errorf("%s label = %q, want %q", id, n.Label, label)
				}
				if n.Kind != "route" || n.FileType != "code" {
					t.Errorf("%s kind/file_type = %s/%s", id, n.Kind, n.FileType)
				}
				if n.Location == "" {
					t.Errorf("%s has no location", id)
				}
			}
			for _, id := range tc.absentNodes {
				if _, ok := nodes[id]; ok {
					t.Errorf("route node %s must not exist", id)
				}
			}
			if len(nodes) != len(tc.wantNodes) {
				t.Errorf("route node count = %d, want %d: %v", len(nodes), len(tc.wantNodes), keysOf(nodes))
			}
			for k, ch := range tc.wantEdges {
				if edges[k] != ch {
					t.Errorf("edge %s synthesized_by = %q, want %q", k, edges[k], ch)
				}
			}
			if len(edges) != len(tc.wantEdges) {
				t.Errorf("handles edge count = %d, want %d: %v", len(edges), len(tc.wantEdges), edges)
			}
		})
	}
}

func keysOf(m map[string]schema.Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Route locations cover the whole declaration (decorator line through the
// handler's end), so `card` cites a range an agent can open directly.
func TestRouteLocationAndDedup(t *testing.T) {
	root := t.TempDir()
	src := `@app.get("/one")
def one():
    pass

@app.get("/one")
def one_again():
    pass
`
	if err := os.WriteFile(filepath.Join(root, "d.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	batch, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Validate(); err != nil {
		t.Fatalf("duplicate route registration must still pass the door: %v", err)
	}
	nodes, edges := routeIndex(t, batch)
	n, ok := nodes["d.py::route:GET /one"]
	if !ok {
		t.Fatal("missing route node")
	}
	if n.Location != "L1-L3" { // first declaration wins, decorator row included
		t.Errorf("location = %q, want L1-L3", n.Location)
	}
	// Both registrations keep their handles edges — the node dedupes, the
	// truth about handlers does not.
	if len(edges) != 2 {
		t.Errorf("handles edges = %v, want 2", edges)
	}
}

// Zero recognizer hits on this repo's own internal/ tree — the measured
// false-positive check from the spec (Go code, one embedded html asset).
func TestNoRoutesInThisRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("parses the whole internal/ tree")
	}
	batch, err := Extract(filepath.Join("..", "..", "..", "internal"))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range batch.Nodes {
		if n.Kind == "route" {
			t.Errorf("unexpected route node in own repo: %s (%s)", n.ID, n.Label)
		}
	}
	for _, e := range batch.Edges {
		if e.Relation == "handles" {
			t.Errorf("unexpected handles edge in own repo: %s→%s", e.Source, e.Target)
		}
	}
}
