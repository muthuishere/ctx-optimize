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

	// ID is namespace-scoped, so it agrees with the label (S4).
	task := nodeByID(b, "ci.build.xml::task:internal:compile")
	if task == nil {
		t.Fatalf("missing xml-derived task node: %v", b.Nodes)
	}
	// namespace defaults to the pack name for task labels.
	if task.Label != "internal:compile" {
		t.Fatalf("task label: %s", task.Label)
	}
	if task.Location != "L1" {
		t.Fatalf("task location: %q, want L1 (single-line fixture)", task.Location)
	}
	if nodeByID(b, "ci.build.xml::task:internal:package") == nil {
		t.Fatal("second target missing")
	}
}

// S3: every pack-emitted node carries a Location — a node without file:line
// cannot be cited or passed to verify. Covers all three formats at once.
func TestPackNodesAlwaysCarryLocation(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".ctxoptimize/manifests/all.json": `{"name": "all", "rules": [
		  {"file": "*.tasks.json", "format": "json", "path": "tasks.*", "emit": "task", "namespace": "j"},
		  {"file": "*.tasks.yaml", "format": "yaml", "path": "tasks.*", "emit": "task", "namespace": "y"},
		  {"file": "*.build.xml", "format": "xml", "path": "project/target/@name", "emit": "task", "namespace": "x"}]}`,
		"a.tasks.json": "{\n  \"tasks\": {\n    \"build\": \"go build\"\n  }\n}\n",
		"a.tasks.yaml": "version: 1\ntasks:\n  lint: golangci-lint run\n  test: go test ./...\n",
		"a.build.xml":  "<project name=\"demo\">\n  <target name=\"clean\"/>\n  <target\n      name=\"compile\"/>\n</project>\n",
	})
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, n := range b.Nodes {
		if n.Kind != "task" {
			continue
		}
		seen++
		if n.Location == "" {
			t.Errorf("pack node %s has no Location", n.ID)
		}
	}
	if seen != 5 {
		t.Fatalf("task nodes = %d, want 5 (1 json + 2 yaml + 2 xml): %v", seen, b.Nodes)
	}
	// Exact lines: yaml comes from the shared walker, xml from the decoder
	// offset of the matched START element.
	for id, want := range map[string]string{
		"a.tasks.yaml::task:y:lint":   "L3",
		"a.tasks.yaml::task:y:test":   "L4",
		"a.build.xml::task:x:clean":   "L2",
		"a.build.xml::task:x:compile": "L3", // the element OPENS on L3
		"a.tasks.json::task:j:build":  "L1", // json has no positions: honest L1
	} {
		n := nodeByID(b, id)
		if n == nil {
			t.Errorf("missing node %s", id)
			continue
		}
		if n.Location != want {
			t.Errorf("%s location = %q, want %q", id, n.Location, want)
		}
	}
}

// S4: two rules yielding the SAME name for the same file must survive as two
// distinct nodes. Before the namespace-scoped ID they collided and one was
// silently dropped by the collector's dedup.
func TestPackNodeIDsAreNamespaceScoped(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".ctxoptimize/manifests/logback.json": `{"name": "logback", "rules": [
		  {"file": "logback.xml", "format": "xml", "path": "configuration/appender/@name", "emit": "task", "namespace": "appender"},
		  {"file": "logback.xml", "format": "xml", "path": "configuration/root/appender-ref/@ref", "emit": "task", "namespace": "appender-ref"}]}`,
		"logback.xml": "<configuration>\n  <appender name=\"STDOUT\"/>\n  <root>\n    <appender-ref ref=\"STDOUT\"/>\n  </root>\n</configuration>\n",
	})
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	var tasks []schema.Node
	for _, n := range b.Nodes {
		if n.Kind == "task" {
			tasks = append(tasks, n)
		}
	}
	if len(tasks) != 2 {
		t.Fatalf("task nodes = %d, want 2 (same name, two namespaces): %+v", len(tasks), tasks)
	}
	for id, wantLoc := range map[string]string{
		"logback.xml::task:appender:STDOUT":     "L2",
		"logback.xml::task:appender-ref:STDOUT": "L4",
	} {
		n := nodeByID(b, id)
		if n == nil {
			t.Fatalf("missing %s: %+v", id, tasks)
		}
		if n.Location != wantLoc {
			t.Errorf("%s location = %q, want %q", id, n.Location, wantLoc)
		}
	}
}

// S5: the package doc's example must actually work. `project/target/@name`
// matches a real Ant build file; the old documented `target/@name` matched
// nothing, because selectors are root-anchored and exact-depth.
func TestDocumentedXMLExampleIsRootAnchored(t *testing.T) {
	ant := []byte(`<project name="demo" default="dist">
  <target name="clean"/>
  <target name="compile" depends="clean"/>
  <target name="dist" depends="compile"/>
</project>`)
	got := xmlSelect(ant, "project/target/@name")
	var names []string
	for _, h := range got {
		names = append(names, h.name)
	}
	if strings.Join(names, ",") != "clean,compile,dist" {
		t.Fatalf("documented selector yielded %v", names)
	}
	if h := xmlSelect(ant, "target/@name"); len(h) != 0 {
		t.Fatalf("non-root-anchored selector must yield nothing, got %+v", h)
	}
	// `*` matches exactly one level — never zero, never many.
	if h := xmlSelect(ant, "*/target/@name"); len(h) != 3 {
		t.Fatalf("`*` must match exactly one level: %+v", h)
	}
	if h := xmlSelect(ant, "*/*/target/@name"); len(h) != 0 {
		t.Fatalf("`*` must not match zero levels: %+v", h)
	}
}

// Selector line reporting, at unit level.
func TestSelectorLines(t *testing.T) {
	y := yamlSelect("tools:\n  golangci: 1.55.0\n  gofumpt: 0.6.0\n", "tools.*")
	if len(y) != 2 || y[0].line != 2 || y[1].line != 3 {
		t.Fatalf("yaml lines: %+v", y)
	}
	// list items are located on their own lines, not the key's.
	yl := yamlSelect("deps:\n  - alpha\n  - beta\n", "deps")
	if len(yl) != 2 || yl[0].line != 2 || yl[1].line != 3 {
		t.Fatalf("yaml list lines: %+v", yl)
	}
	// element CONTENT is located on the line the element opens on.
	x := xmlSelect([]byte("<deps>\n  <dep>alpha</dep>\n\n  <dep>beta</dep>\n</deps>"), "deps/dep")
	if len(x) != 2 || x[0].line != 2 || x[1].line != 4 {
		t.Fatalf("xml content lines: %+v", x)
	}
	// json reports no line; locOf turns that into the honest file-level L1.
	j := jsonSelect([]byte("{\n \"a\": \"1\"\n}"), "a")
	if len(j) != 1 || j[0].line != 0 {
		t.Fatalf("json lines: %+v", j)
	}
	if got := locOf(j[0]); got != "L1" {
		t.Fatalf("locOf fallback = %q, want L1", got)
	}
	if got := locOf(pair{name: "x", line: 42}); got != "L42" {
		t.Fatalf("locOf = %q, want L42", got)
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
