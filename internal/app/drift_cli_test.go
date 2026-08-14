package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// End-to-end: port batches through the universal door, then `drift` must
// accuse the EXTRACTED dead contract, list the INFERRED one, and --strict
// must exit nonzero. Federated ids (module-prefixed) group by metadata.
func TestDriftCLI(t *testing.T) {
	repo := fakeMonorepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)

	batch := `{"producer":"boundaries","nodes":[
	  {"id":"port:network.http:>/dead-route","label":"/dead-route","kind":"port",
	   "file_type":"boundary","source":"port://network.http//dead-route",
	   "metadata":{"direction":"provides","transport":"network.http","identifier":"/dead-route"}},
	  {"id":"port:network.http:>/soft-route","label":"/soft-route","kind":"port",
	   "file_type":"boundary","source":"port://network.http//soft-route",
	   "metadata":{"direction":"provides","transport":"network.http","identifier":"/soft-route"}}],
	 "edges":[
	  {"source":"routes.go","target":"port:network.http:>/dead-route","relation":"provides",
	   "confidence":"EXTRACTED","metadata":{"rule":"routes-go","site":"routes.go:L10"}},
	  {"source":"routes.go","target":"port:network.http:>/soft-route","relation":"provides",
	   "confidence":"INFERRED","metadata":{"rule":"routes-go","site":"routes.go:L20"}}]}`
	bf := filepath.Join(t.TempDir(), "batch.json")
	if err := os.WriteFile(bf, []byte(batch), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, 0, "add", "--json", bf, "--path", filepath.Join(repo, "services", "api"))

	out, _ := runCLI(t, 0, "drift", "--path", repo)
	if !strings.Contains(out, "[dead-contract] network.http /dead-route") {
		t.Fatalf("EXTRACTED dead contract not accused:\n%s", out)
	}
	if !strings.Contains(out, "routes.go:L10 [EXTRACTED, rule routes-go]") {
		t.Fatalf("finding must cite site+rule:\n%s", out)
	}
	if !strings.Contains(out, "[lower-tier-orphan] network.http /soft-route") {
		t.Fatalf("INFERRED orphan must be listed, not accused:\n%s", out)
	}
	if strings.Contains(strings.SplitN(out, "OBSERVED", 2)[0], "/soft-route") {
		t.Fatalf("/soft-route leaked into FINDINGS:\n%s", out)
	}

	// --strict: nonzero exit while findings exist.
	runCLI(t, 1, "drift", "--strict", "--path", repo)

	// --json is machine-parseable and carries both sections.
	out, _ = runCLI(t, 0, "drift", "--json", "--path", repo)
	if !strings.Contains(out, `"findings"`) || !strings.Contains(out, `"observations"`) {
		t.Fatalf("drift --json missing sections:\n%s", out)
	}
}
