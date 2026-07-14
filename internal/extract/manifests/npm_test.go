package manifests

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

const fixturePackageJSON = `{
  "name": "api",
  "dependencies": {
    "express": "^4.18.2",
    "lodash": "~4.17.21"
  },
  "devDependencies": {
    "vitest": "^1.2.0"
  },
  "peerDependencies": {
    "react": ">=18"
  },
  "scripts": {
    "build": "tsc -p .",
    "test": "vitest run"
  }
}
`

func TestPackageJSONDependencies(t *testing.T) {
	b := extractFixture(t, map[string]string{"package.json": fixturePackageJSON})

	n := nodeByID(b, "dep:npm/express")
	if n == nil {
		t.Fatal("missing dep:npm/express node")
	}
	if n.Kind != "dependency" || n.FileType != "manifest" || n.Label != "express" {
		t.Fatalf("dep node shape wrong: %+v", n)
	}
	e := mustEdge(t, b, "package.json", "dep:npm/express", "declares", schema.Extracted)
	if e.Metadata["version_spec"] != "^4.18.2" || e.Metadata["scope"] != "dependencies" {
		t.Fatalf("declares metadata wrong: %v", e.Metadata)
	}
	dev := mustEdge(t, b, "package.json", "dep:npm/vitest", "declares", schema.Extracted)
	if dev.Metadata["scope"] != "devDependencies" {
		t.Fatalf("dev scope wrong: %v", dev.Metadata)
	}
	peer := mustEdge(t, b, "package.json", "dep:npm/react", "declares", schema.Extracted)
	if peer.Metadata["scope"] != "peerDependencies" || peer.Metadata["version_spec"] != ">=18" {
		t.Fatalf("peer metadata wrong: %v", peer.Metadata)
	}
}

func TestPackageJSONScriptsAreTasks(t *testing.T) {
	b := extractFixture(t, map[string]string{"package.json": fixturePackageJSON})
	task := nodeByID(b, "package.json::task:build")
	if task == nil {
		t.Fatal("missing task node package.json::task:build")
	}
	if task.Kind != "task" || task.Label != "npm:build" {
		t.Fatalf("task node shape wrong: %+v", task)
	}
	if task.Location != "L14" { // the "build" line in the fixture
		t.Fatalf("task line anchor = %s, want L14", task.Location)
	}
	if task.Metadata["command"] != "tsc -p ." {
		t.Fatalf("task command = %q", task.Metadata["command"])
	}
	mustEdge(t, b, "package.json", "package.json::task:build", "contains", schema.Extracted)
}

func TestNpmWorkspacesDependsOn(t *testing.T) {
	b := extractFixture(t, map[string]string{
		"package.json":              `{"workspaces": ["packages/*"]}`,
		"packages/a/package.json":   `{"name": "a"}`,
		"packages/b/package.json":   `{"name": "b"}`,
		"packages/no-manifest/x.md": "not a workspace member",
	})
	mustEdge(t, b, "package.json", "packages/a/package.json", "depends_on", schema.Extracted)
	mustEdge(t, b, "package.json", "packages/b/package.json", "depends_on", schema.Extracted)
	for _, e := range b.Edges {
		if e.Relation == "depends_on" && strings.Contains(e.Target, "no-manifest") {
			t.Fatal("dir without package.json must not become a workspace member")
		}
	}
}

// Two manifests declaring the same dep: ONE node, two declares edges each
// carrying its own version_spec — the version lives on the edge, not the id.
func TestSharedDepIsOneNodeTwoEdges(t *testing.T) {
	b := extractFixture(t, map[string]string{
		"a/package.json": `{"dependencies": {"express": "^4.18.2"}}`,
		"b/package.json": `{"dependencies": {"express": "^5.0.0"}}`,
	})
	count := 0
	for _, n := range b.Nodes {
		if n.ID == "dep:npm/express" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("dep:npm/express nodes = %d, want exactly 1", count)
	}
	ea := mustEdge(t, b, "a/package.json", "dep:npm/express", "declares", schema.Extracted)
	eb := mustEdge(t, b, "b/package.json", "dep:npm/express", "declares", schema.Extracted)
	if ea.Metadata["version_spec"] != "^4.18.2" || eb.Metadata["version_spec"] != "^5.0.0" {
		t.Fatalf("per-edge versions wrong: %v / %v", ea.Metadata, eb.Metadata)
	}
}
