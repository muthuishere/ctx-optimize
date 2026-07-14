package manifests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// extractFixture writes files (slash-relative paths) into a temp repo and
// extracts. CTX_OPTIMIZE_STORE is pinned to a temp dir so pack discovery
// never touches the real machine store — hermetic.
func extractFixture(t *testing.T, files map[string]string) *schema.Batch {
	t.Helper()
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	root := t.TempDir()
	writeTree(t, root, files)
	b, err := Extract(root)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("batch failed the door: %v", err)
	}
	if b.Producer != ProducerName {
		t.Fatalf("producer = %q, want %q", b.Producer, ProducerName)
	}
	return b
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func nodeByID(b *schema.Batch, id string) *schema.Node {
	for i := range b.Nodes {
		if b.Nodes[i].ID == id {
			return &b.Nodes[i]
		}
	}
	return nil
}

func findEdge(b *schema.Batch, source, target, relation string) *schema.Edge {
	for i := range b.Edges {
		e := &b.Edges[i]
		if e.Source == source && e.Target == target && e.Relation == relation {
			return e
		}
	}
	return nil
}

func mustEdge(t *testing.T, b *schema.Batch, source, target, relation, confidence string) *schema.Edge {
	t.Helper()
	e := findEdge(b, source, target, relation)
	if e == nil {
		t.Fatalf("missing edge %s --%s--> %s", source, relation, target)
	}
	if e.Confidence != confidence {
		t.Fatalf("edge %s --%s--> %s confidence = %s, want %s", source, relation, target, e.Confidence, confidence)
	}
	return e
}

// The walk classifier: manifests vs lockfiles vs secrets vs noise.
func TestManifestKindAndRefusals(t *testing.T) {
	for name, want := range map[string]string{
		"package.json": "npm", "pom.xml": "pom", "go.mod": "gomod",
		"build.gradle": "gradle", "build.gradle.kts": "gradle",
		"Api.csproj": "csproj", "All.sln": "sln",
		"deploy.yaml": "yaml", "deploy.yml": "yaml",
		"README.md": "", "main.go": "", "settings.gradle": "",
	} {
		if got := manifestKind(name); got != want {
			t.Errorf("manifestKind(%s) = %q, want %q", name, got, want)
		}
	}
	for _, lock := range []string{"package-lock.json", "yarn.lock", "go.sum", "Cargo.lock", "pnpm-lock.yaml", "custom.lock"} {
		if !isLockfile(lock) {
			t.Errorf("%s must be a lockfile", lock)
		}
	}
	for _, s := range []string{"secrets.yaml", "db-credentials.json", ".env.production", "password.properties"} {
		if !secretName(s) {
			t.Errorf("%s must be refused as secret-smelling", s)
		}
	}
}

// Lockfiles and secret-named files yield nothing even when their content is
// a perfectly parseable manifest shape.
func TestLockfilesAndSecretsSkipped(t *testing.T) {
	b := extractFixture(t, map[string]string{
		"package-lock.json": `{"dependencies": {"express": {"version": "4.18.2"}}}`,
		"yarn.lock":         `express@^4.18.2:\n  version "4.18.2"`,
		"secrets.yaml":      "kind: Secret\napiVersion: v1\nmetadata:\n  name: prod-creds\n",
	})
	if len(b.Nodes) != 0 || len(b.Edges) != 0 {
		t.Fatalf("lockfiles/secret-named files must yield nothing, got %d nodes %d edges", len(b.Nodes), len(b.Edges))
	}
}

// A manifest inside pruned build output is generated, not intent.
func TestPrunedDirsSkipped(t *testing.T) {
	b := extractFixture(t, map[string]string{
		"node_modules/express/package.json": `{"dependencies": {"accepts": "~1.3.8"}}`,
		"dist/package.json":                 `{"dependencies": {"lodash": "^4.17.21"}}`,
	})
	if len(b.Nodes) != 0 {
		t.Fatalf("pruned dirs must not contribute, got %v", b.Nodes)
	}
}

// This repo's own real config files must gain NO dep/k8s nodes — the
// standing false-positive sweep from the spec's success checks.
func TestRepoSweepNoFalsePositives(t *testing.T) {
	files := map[string]string{}
	for _, name := range []string{"Taskfile.yml", ".goreleaser.yaml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", name))
		if err != nil {
			t.Fatalf("read repo %s: %v", name, err)
		}
		files[name] = string(data)
	}
	b := extractFixture(t, files)
	for _, n := range b.Nodes {
		if strings.HasPrefix(n.ID, "dep:") || strings.HasPrefix(n.ID, "k8s://") {
			t.Errorf("repo config file produced a semantic node: %s (source %s)", n.ID, n.Source)
		}
	}
}
