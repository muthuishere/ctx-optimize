package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression (ADR 2026-08-13 boundary-model): on a federated multi-module
// store, `nodes --kind port --where scope=external` returned "(0 nodes)" even
// though the port nodes carried metadata["scope"]="external" — the empty
// top-level Scope struct field shadowed the metadata key in the --where
// resolver. Port nodes are injected through the universal door so this test
// exercises exactly the store shape the boundaries lane produces, without
// depending on the boundaries extractor itself.
func TestWhereScopeMatchesPortMetadataFederated(t *testing.T) {
	repo := fakeMonorepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())

	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)

	// One port per module store, exactly as the boundaries producer shapes them.
	batches := map[string]string{
		"services/api": `{"producer":"boundaries","nodes":[
		  {"id":"port:network.http:>api.openai.com","label":"api.openai.com","kind":"port",
		   "file_type":"boundary","source":"port://network.http/api.openai.com",
		   "metadata":{"direction":"consumes","transport":"network.http",
		     "identifier":"api.openai.com","scope":"external"}}],"edges":[]}`,
		"services/worker": `{"producer":"boundaries","nodes":[
		  {"id":"port:config.env:>OPENAI_API_KEY","label":"OPENAI_API_KEY","kind":"port",
		   "file_type":"boundary","source":"port://config.env/OPENAI_API_KEY",
		   "metadata":{"direction":"consumes","transport":"config.env",
		     "identifier":"OPENAI_API_KEY","scope":"internal","sensitive":"true"}}],"edges":[]}`,
	}
	for mod, batch := range batches {
		bf := filepath.Join(t.TempDir(), "batch.json")
		if err := os.WriteFile(bf, []byte(batch), 0o644); err != nil {
			t.Fatal(err)
		}
		runCLI(t, 0, "add", "--json", bf, "--path", filepath.Join(repo, filepath.FromSlash(mod)))
	}

	// The exact command that missed: federated query from the repo root.
	out, _ := runCLI(t, 0, "nodes", "--kind", "port", "--where", "scope=external", "--path", repo, "--json")
	var got []struct {
		ID       string            `json:"id"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("nodes --json not parseable: %v\n%s", err, out)
	}
	// Federation prefixes ids with the module path (services/api/port:...).
	if len(got) != 1 || !strings.HasSuffix(got[0].ID, "port:network.http:>api.openai.com") {
		t.Fatalf("--where scope=external missed the external port: %s", out)
	}
	// internal port is filterable the same way, across the other module.
	out, _ = runCLI(t, 0, "nodes", "--kind", "port", "--where", "scope=internal", "--path", repo)
	if !strings.Contains(out, "OPENAI_API_KEY") || !strings.Contains(out, "(1 nodes)") {
		t.Fatalf("--where scope=internal miss: %s", out)
	}
	// deps --scope (the top-level-field consumer) must be untouched by the fix.
	out, _ = runCLI(t, 0, "deps", "--scope", "external", "--path", repo)
	if strings.Contains(out, "port:") {
		t.Fatalf("deps --scope must not surface port nodes: %s", out)
	}
}
