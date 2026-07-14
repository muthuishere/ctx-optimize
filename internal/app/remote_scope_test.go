package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupSyncedMonorepo builds the fake monorepo, gathers it, and wires a
// file:// remote — the common front half of the tree-sync tests.
func setupSyncedMonorepo(t *testing.T) (repo, remoteDir string) {
	t.Helper()
	repo = fakeMonorepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)
	remoteDir = t.TempDir()
	runCLI(t, 0, "remote", "init", "file://"+remoteDir, "--path", repo)
	return repo, remoteDir
}

func TestRemoteTreeRootPushCoversWholeTree(t *testing.T) {
	repo, remoteDir := setupSyncedMonorepo(t)
	out, _ := runCLI(t, 0, "remote", "push", "--path", repo)
	if !strings.Contains(out, "push: ") {
		t.Fatalf("push output: %s", out)
	}
	for _, p := range []string{
		"manifest.json",                     // root store
		"navigator.md",                      // the root artifact travels
		"modules.json",                      // navigator machine map
		"stores.json",                       // tree index
		"services/api/manifest.json",        // module store
		"services/api/graph/nodes.ndjson",   // module graph
		"services/api/plugin/manifest.json", // NESTED module store
		"services/worker/graph/nodes.ndjson",
	} {
		if _, err := os.Stat(filepath.Join(remoteDir, filepath.FromSlash(p))); err != nil {
			t.Fatalf("root push missing %s: %v", p, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(remoteDir, "stores.json"))
	if err != nil {
		t.Fatal(err)
	}
	var idx struct {
		Stores []string `json:"stores"`
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}
	want := []string{"", "services/api", "services/api/plugin", "services/worker"}
	if len(idx.Stores) != len(want) {
		t.Fatalf("index stores = %v, want %v", idx.Stores, want)
	}
	for i := range want {
		if idx.Stores[i] != want[i] {
			t.Fatalf("index stores = %v, want %v", idx.Stores, want)
		}
	}
}

func TestRemoteTreeRootPullOntoFreshStore(t *testing.T) {
	repo, _ := setupSyncedMonorepo(t)
	runCLI(t, 0, "remote", "push", "--path", repo)

	fresh := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", fresh)
	out, _ := runCLI(t, 0, "remote", "pull", "--path", repo)
	if !strings.Contains(out, "pull: ") {
		t.Fatalf("pull output: %s", out)
	}
	key := filepath.Base(repo)
	for _, p := range []string{
		key + "/navigator.md",
		key + "/services/api/graph/nodes.ndjson",
		key + "/services/api/plugin/graph/nodes.ndjson",
		key + "/services/worker/graph/nodes.ndjson",
	} {
		if _, err := os.Stat(filepath.Join(fresh, filepath.FromSlash(p))); err != nil {
			t.Fatalf("pull missing %s: %v", p, err)
		}
	}
	// The pulled tree answers — module scope, from the module dir.
	out, _ = runCLI(t, 0, "query", "RunPayrollJob", "--path", filepath.Join(repo, "services", "worker"))
	if !strings.Contains(out, "RunPayrollJob") {
		t.Fatalf("pulled store does not answer: %s", out)
	}
}

func TestRemoteTreeModulePushOnlyItsPrefix(t *testing.T) {
	repo, remoteDir := setupSyncedMonorepo(t)
	apiDir := filepath.Join(repo, "services", "api")
	runCLI(t, 0, "remote", "push", "--path", apiDir)

	// The module's prefix (nested module included) is there…
	for _, p := range []string{
		"services/api/manifest.json",
		"services/api/plugin/manifest.json",
		"stores.json",
	} {
		if _, err := os.Stat(filepath.Join(remoteDir, filepath.FromSlash(p))); err != nil {
			t.Fatalf("module push missing %s: %v", p, err)
		}
	}
	// …and NOTHING else: no sibling module, no root store manifest.
	if _, err := os.Stat(filepath.Join(remoteDir, "services", "worker")); !os.IsNotExist(err) {
		t.Fatal("module push leaked the sibling module")
	}
	if _, err := os.Stat(filepath.Join(remoteDir, "manifest.json")); !os.IsNotExist(err) {
		t.Fatal("module push leaked the root store")
	}
}

func TestRemoteTreeModulePullOnlyItsPrefix(t *testing.T) {
	repo, _ := setupSyncedMonorepo(t)
	runCLI(t, 0, "remote", "push", "--path", repo)

	fresh := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", fresh)
	workerDir := filepath.Join(repo, "services", "worker")
	runCLI(t, 0, "remote", "pull", "--path", workerDir)

	key := filepath.Base(repo)
	if _, err := os.Stat(filepath.Join(fresh, filepath.FromSlash(key+"/services/worker/graph/nodes.ndjson"))); err != nil {
		t.Fatalf("module pull missing its own graph: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fresh, filepath.FromSlash(key+"/services/api/graph/nodes.ndjson"))); !os.IsNotExist(err) {
		t.Fatal("module pull fetched a sibling module")
	}
}

func TestRemoteTreePullPrefixAgainstSingleStoreRemoteFailsLoudly(t *testing.T) {
	repo, _ := setupSyncedMonorepo(t)
	// Simulate a pre-tree remote: single-store push layout, no stores.json —
	// by pushing only a plain single-module repo to the same URL shape.
	single := t.TempDir()
	if err := os.WriteFile(filepath.Join(single, "notes.md"), []byte("# solo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, 0, "init", "--path", single)
	runCLI(t, 0, "add", "--path", single)
	soloRemote := t.TempDir()
	runCLI(t, 0, "remote", "init", "file://"+soloRemote, "--path", single)
	runCLI(t, 0, "remote", "push", "--path", single)
	if _, err := os.Stat(filepath.Join(soloRemote, "stores.json")); !os.IsNotExist(err) {
		t.Fatal("single-module push must stay index-free (byte-identical old behavior)")
	}

	// Point the monorepo at that single-store remote and pull from a module
	// dir: must error loudly, not silently pull the wrong thing.
	runCLI(t, 0, "remote", "init", "file://"+soloRemote, "--path", repo)
	out, errOut := runCLI(t, 1, "remote", "pull", "--path", filepath.Join(repo, "services", "api"))
	if !strings.Contains(out+errOut, "stores.json") {
		t.Fatalf("expected loud index-missing error, got: %s%s", out, errOut)
	}
}

// TestInitOnCloneRoutesToPull: a fresh clone already carries the committed
// .ctxoptimize/config.json (with a remote) but has no local store. `init` must
// recognize that and point at `remote pull` — NOT scaffold-and-rebuild — so the
// team's prebuilt graph is fetched, not re-derived. --force overrides.
func TestInitOnCloneRoutesToPull(t *testing.T) {
	// Producer: build + publish a single-project store.
	repo := t.TempDir()
	writeFiles(t, repo, map[string]string{"main.go": "package main\n\nfunc Boot() {}\n"})
	remoteDir := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)
	runCLI(t, 0, "remote", "init", "file://"+remoteDir, "--path", repo)
	runCLI(t, 0, "remote", "push", "--path", repo)

	// Consumer clone: identical committed config + source, empty store root.
	clone := t.TempDir()
	cfgData, err := os.ReadFile(filepath.Join(repo, ".ctxoptimize", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, clone, map[string]string{
		"main.go":                  "package main\n\nfunc Boot() {}\n",
		".ctxoptimize/config.json": string(cfgData),
	})
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir()) // fresh, empty

	out, _ := runCLI(t, 0, "init", "--path", clone)
	if !strings.Contains(out, "remote pull") || !strings.Contains(out, "already configured") {
		t.Fatalf("init on a clone must route to remote pull, got:\n%s", out)
	}
	if strings.Contains(out, "store ready") {
		t.Fatalf("init on a clone must NOT claim a fresh scaffold:\n%s", out)
	}

	// The pull then populates the store from the prebuilt remote.
	runCLI(t, 0, "remote", "pull", "--path", clone)
	st, _ := runCLI(t, 0, "status", "--json", "--path", clone)
	var status struct {
		Nodes int `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(st), &status); err != nil {
		t.Fatalf("status --json: %v\n%s", err, st)
	}
	if status.Nodes == 0 {
		t.Fatalf("pull after the init hint should have populated the store:\n%s", st)
	}
}
