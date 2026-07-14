package manifests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func TestParseManifestPackValidation(t *testing.T) {
	good := `{"name": "internal", "rules": [
	  {"file": "*.deps.json", "format": "json", "path": "libraries.*", "emit": "dependency", "namespace": "internal"}]}`
	if _, err := ParseManifestPack([]byte(good), "good.json"); err != nil {
		t.Fatalf("valid pack refused: %v", err)
	}
	for name, bad := range map[string]string{
		"no name":    `{"rules": [{"file": "*.x", "format": "json", "path": "a", "emit": "task"}]}`,
		"no rules":   `{"name": "x", "rules": []}`,
		"no file":    `{"name": "x", "rules": [{"format": "json", "path": "a", "emit": "task"}]}`,
		"bad format": `{"name": "x", "rules": [{"file": "*.x", "format": "toml", "path": "a", "emit": "task"}]}`,
		"no path":    `{"name": "x", "rules": [{"file": "*.x", "format": "json", "emit": "task"}]}`,
		"bad emit":   `{"name": "x", "rules": [{"file": "*.x", "format": "json", "path": "a", "emit": "route"}]}`,
		"bad glob":   `{"name": "x", "rules": [{"file": "[", "format": "json", "path": "a", "emit": "task"}]}`,
		"not json":   `{`,
	} {
		if _, err := ParseManifestPack([]byte(bad), name); err == nil {
			t.Errorf("%s: must fail loudly", name)
		}
	}
}

// Discovery precedence: repo pack beats machine pack of the same name;
// distinct names coexist.
func TestManifestPackPrecedence(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()

	machineDir := filepath.Join(storeRoot, "manifests")
	repoDir := filepath.Join(repo, ".ctxoptimize", "manifests")
	os.MkdirAll(machineDir, 0o755)
	os.MkdirAll(repoDir, 0o755)
	os.WriteFile(filepath.Join(machineDir, "shared.json"),
		[]byte(`{"name": "shared", "rules": [{"file": "*.machine.json", "format": "json", "path": "a", "emit": "task"}]}`), 0o644)
	os.WriteFile(filepath.Join(machineDir, "only-machine.json"),
		[]byte(`{"name": "only-machine", "rules": [{"file": "*.m.json", "format": "json", "path": "a", "emit": "task"}]}`), 0o644)
	os.WriteFile(filepath.Join(repoDir, "shared.json"),
		[]byte(`{"name": "shared", "rules": [{"file": "*.repo.json", "format": "json", "path": "a", "emit": "task"}]}`), 0o644)

	packs, err := LoadManifestPacks(repo)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ManifestPack{}
	for _, p := range packs {
		byName[p.Name] = p
	}
	if len(packs) != 2 {
		t.Fatalf("packs = %d, want 2 (shared deduped)", len(packs))
	}
	if byName["shared"].Rules[0].File != "*.repo.json" {
		t.Fatalf("repo pack must win the name collision: %+v", byName["shared"])
	}
	if _, ok := byName["only-machine"]; !ok {
		t.Fatal("machine-only pack must still load")
	}

	// Malformed machine pack fails discovery loudly, naming the file.
	os.WriteFile(filepath.Join(machineDir, "broken.json"), []byte(`{"name": "broken"}`), 0o644)
	if _, err := LoadManifestPacks(repo); err == nil || !strings.Contains(err.Error(), "broken.json") {
		t.Fatalf("malformed pack must fail loudly naming the file: %v", err)
	}
}

// A pack rule drives extraction end-to-end: json map entries become deps
// with versions; xml attr path becomes tasks.
func TestPackRuleExtraction(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".ctxoptimize/manifests/internal.json": `{"name": "internal", "rules": [
		  {"file": "*.deps.json", "format": "json", "path": "libraries.*", "emit": "dependency", "namespace": "internal"},
		  {"file": "*.build.xml", "format": "xml", "path": "project/target/@name", "emit": "task"}]}`,
		"svc.deps.json": `{"libraries": {"corelib": "2.1.0", "authlib": "1.0.0"}}`,
		"ci.build.xml":  `<project><target name="compile"/><target name="package"/></project>`,
	})
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}

	dep := nodeByID(b, "dep:internal/corelib")
	if dep == nil {
		t.Fatal("missing pack-derived dep node")
	}
	e := mustEdge(t, b, "svc.deps.json", "dep:internal/corelib", "declares", schema.Extracted)
	if e.Metadata["version_spec"] != "2.1.0" || e.Metadata["synthesized_by"] != "manifest-pack:internal" {
		t.Fatalf("pack declares metadata: %v", e.Metadata)
	}

	task := nodeByID(b, "ci.build.xml::task:compile")
	if task == nil {
		t.Fatal("missing xml-derived task node")
	}
	// namespace defaults to the pack name for task labels.
	if task.Label != "internal:compile" {
		t.Fatalf("task label: %s", task.Label)
	}
	if nodeByID(b, "ci.build.xml::task:package") == nil {
		t.Fatal("second target missing")
	}
}

// The yaml selector over the shared walker: trailing * yields (key, value)
// entries; a concrete path yields list items.
func TestPackYAMLSelector(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".ctxoptimize/manifests/conda.json": `{"name": "conda", "rules": [
		  {"file": "environment.dep.yaml", "format": "yaml", "path": "dependencies", "emit": "dependency", "namespace": "conda"}]}`,
		"environment.dep.yaml": "name: myenv\ndependencies:\n  - numpy\n  - pandas\n",
	})
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if nodeByID(b, "dep:conda/numpy") == nil || nodeByID(b, "dep:conda/pandas") == nil {
		t.Fatalf("yaml list selector missed items: %v", b.Nodes)
	}
}

// Selector unit coverage: the tiny language, nothing more.
func TestSelectorSemantics(t *testing.T) {
	// json: * over object yields key+version; concrete path yields value.
	hits := jsonSelect([]byte(`{"libs": {"a": "1.0", "b": {"deep": true}}, "main": "x"}`), "libs.*")
	if len(hits) != 2 || hits[0].name != "a" || hits[0].version != "1.0" || hits[1].name != "b" || hits[1].version != "" {
		t.Fatalf("json * over object: %+v", hits)
	}
	if h := jsonSelect([]byte(`{"main": "x"}`), "main"); len(h) != 1 || h[0].name != "x" {
		t.Fatalf("json scalar: %+v", h)
	}
	if h := jsonSelect([]byte(`{"items": ["p", "q"]}`), "items"); len(h) != 2 {
		t.Fatalf("json string array: %+v", h)
	}
	if h := jsonSelect([]byte(`{"a": {"b": {"c": "v"}}}`), "a.*.c"); len(h) != 1 || h[0].name != "v" {
		t.Fatalf("json mid-path wildcard: %+v", h)
	}
	if h := jsonSelect([]byte(`not json`), "a"); h != nil {
		t.Fatalf("malformed user json must yield nothing: %+v", h)
	}
	// yaml: * yields mapping entries.
	if h := yamlSelect("tools:\n  golangci: 1.55.0\n  gofumpt: 0.6.0\n", "tools.*"); len(h) != 2 || h[0].name != "golangci" || h[0].version != "1.55.0" {
		t.Fatalf("yaml * entries: %+v", h)
	}
	// xml: element content without @attr.
	if h := xmlSelect([]byte(`<deps><dep>alpha</dep><dep>beta</dep></deps>`), "deps/dep"); len(h) != 2 || h[0].name != "alpha" {
		t.Fatalf("xml content: %+v", h)
	}
}
