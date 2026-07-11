package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// end-to-end through Run(): init → add (markdown) → add --json (the door) →
// query --json → remote init/push/pull → status. Hermetic via t.TempDir +
// CTX_OPTIMIZE_STORE.
func TestEndToEnd(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	md := "# Payment Service\n\n## Refund Flow\n\nSee [[Ledger]] for postings.\n"
	if err := os.WriteFile(filepath.Join(repo, "design.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(wantCode int, args ...string) (string, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := Run(args, &out, &errb)
		if code != wantCode {
			t.Fatalf("%v: exit %d (want %d): %s", args, code, wantCode, errb.String())
		}
		return out.String(), errb.String()
	}

	run(0, "init", "--path", repo)
	out, _ := run(0, "add", repo, "--path", repo)
	if !strings.Contains(out, "added") {
		t.Fatalf("add output: %s", out)
	}

	// The universal door: adapter JSON via file.
	batch := `{"producer":"pg-schema","nodes":[{"id":"pg://db/refunds","label":"refunds","kind":"table","file_type":"schema","source":"pg://db/refunds"}],"edges":[]}`
	bf := filepath.Join(t.TempDir(), "batch.json")
	os.WriteFile(bf, []byte(batch), 0o644)
	run(0, "add", "--json", bf, "--path", repo)

	// Door fails closed on garbage.
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte(`{"producer":"","nodes":[]}`), 0o644)
	_, errOut := run(1, "add", "--json", bad, "--path", repo)
	if !strings.Contains(errOut, "reject") {
		t.Fatalf("door did not fail closed: %s", errOut)
	}

	// Query finds both the markdown section and the adapter's table.
	out, _ = run(0, "query", "refund", "--path", repo, "--json")
	var res struct {
		Hits []struct {
			Node struct{ ID string }
		}
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("query --json not parseable: %v\n%s", err, out)
	}
	ids := map[string]bool{}
	for _, h := range res.Hits {
		ids[h.Node.ID] = true
	}
	if !ids["design.md::refund-flow"] || !ids["pg://db/refunds"] {
		t.Fatalf("query missed expected nodes, got %v", ids)
	}

	// `ask` is an alias for query.
	out, _ = run(0, "ask", "refund", "--path", repo)
	if !strings.Contains(out, "refund") && !strings.Contains(out, "Refund") {
		t.Fatalf("ask output: %s", out)
	}

	// push/pull take no URL — the config file is the single source of truth.
	_, errURL := run(1, "remote", "push", "file:///nope", "--path", repo)
	if !strings.Contains(errURL, "takes no URL") {
		t.Fatalf("expected URL rejection, got: %s", errURL)
	}

	// No remote at all → clear error, exit 1.
	_, errNoRemote := run(1, "remote", "pull", "--path", repo)
	if !strings.Contains(errNoRemote, "no remote") {
		t.Fatalf("expected no-remote error, got: %s", errNoRemote)
	}

	// Remote: init writes the committable repo file + push + fresh-machine pull.
	remoteDir := t.TempDir()
	run(0, "remote", "init", "file://"+remoteDir, "--path", repo)
	if _, err := os.Stat(filepath.Join(repo, ".ctxoptimize", "config.json")); err != nil {
		t.Fatalf("remote init did not write repo config: %v", err)
	}
	out, _ = run(0, "remote", "push", "--path", repo)
	if !strings.Contains(out, "transferred") {
		t.Fatalf("push output: %s", out)
	}
	// Simulate a teammate: fresh store root, same repo — the cloned
	// ctx-optimize.json carries the remote, so bare pull works with NO init.
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	run(0, "remote", "pull", "--path", repo)
	out, _ = run(0, "status", "--path", repo, "--json")
	var st struct{ Nodes int }
	json.Unmarshal([]byte(out), &st)
	if st.Nodes == 0 {
		t.Fatalf("pulled store empty: %s", out)
	}
}

// The committable .ctxoptimize/ dir: adapter scripts dropped in adapters/
// are discovered and run on `add`, the object remote (type/url/credentials)
// with ${VAR} placeholders drives bare push, and "name" picks the store
// folder — everything travels with the repo, no secret ever committed.
func TestRepoConfigAdaptersAndRemote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("adapter command uses sh")
	}
	repo := t.TempDir()
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	remoteDir := t.TempDir()
	t.Setenv("CTX_TEST_REMOTE_DIR", remoteDir)

	adaptersDir := filepath.Join(repo, ".ctxoptimize", "adapters")
	os.MkdirAll(adaptersDir, 0o755)
	batch := `{"producer":"kafka-topics","nodes":[{"id":"kafka://orders","label":"orders","kind":"topic","file_type":"messaging","source":"kafka://orders"}],"edges":[]}`
	// Discovery: a .sh dropped into adapters/ IS the registration.
	os.WriteFile(filepath.Join(adaptersDir, "kafka.sh"), []byte("echo '"+batch+"'\n"), 0o644)
	cfg := `{"name":"my-module",
	         "remote":{"type":"file","url":"file://${CTX_TEST_REMOTE_DIR}"}}`
	os.WriteFile(filepath.Join(repo, ".ctxoptimize", "config.json"), []byte(cfg), 0o644)

	run := func(wantCode int, args ...string) (string, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := Run(args, &out, &errb)
		if code != wantCode {
			t.Fatalf("%v: exit %d (want %d): %s", args, code, wantCode, errb.String())
		}
		return out.String(), errb.String()
	}

	// `add` = refresh the world: built-ins + every declared adapter.
	out, _ := run(0, "add", "--path", repo)
	if !strings.Contains(out, "adapter kafka: 1 nodes") {
		t.Fatalf("adapter did not run: %s", out)
	}
	// "name" overrides the store folder: ~store~/my-module/.
	if _, err := os.Stat(filepath.Join(storeRoot, "my-module", "graph", "nodes.ndjson")); err != nil {
		t.Fatalf("custom module name not used: %v", err)
	}
	out, _ = run(0, "query", "orders topic", "--path", repo)
	if !strings.Contains(out, "kafka://orders") {
		t.Fatalf("adapter node not queryable: %s", out)
	}

	// Bare push resolves ${CTX_TEST_REMOTE_DIR} at sync time; status shows
	// the RAW url (placeholder), never the resolved value.
	out, _ = run(0, "remote", "push", "--path", repo)
	if !strings.Contains(out, "transferred") {
		t.Fatalf("push via repo config: %s", out)
	}
	out, _ = run(0, "status", "--path", repo, "--json")
	var st struct {
		Remote     string `json:"remote"`
		RemoteFrom string `json:"remote_from"`
	}
	json.Unmarshal([]byte(out), &st)
	if st.RemoteFrom != ".ctxoptimize/config.json" {
		t.Fatalf("remote_from = %q, want .ctxoptimize/config.json", st.RemoteFrom)
	}
	if !strings.Contains(st.Remote, "${CTX_TEST_REMOTE_DIR}") {
		t.Fatalf("status leaked the resolved remote: %s", st.Remote)
	}
	if _, err := os.Stat(filepath.Join(remoteDir, "manifest.json")); err != nil {
		t.Fatalf("push did not reach the resolved remote: %v", err)
	}

	// An unset ${VAR} fails loudly, naming the variable.
	t.Setenv("CTX_TEST_REMOTE_DIR", "")
	_, errUnset := run(1, "remote", "push", "--path", repo)
	if !strings.Contains(errUnset, "CTX_TEST_REMOTE_DIR") {
		t.Fatalf("unset var not named: %s", errUnset)
	}

	// A broken adapter script fails the whole add, loudly.
	t.Setenv("CTX_TEST_REMOTE_DIR", remoteDir) // restore for the store open
	os.WriteFile(filepath.Join(adaptersDir, "boom.sh"), []byte("exit 3\n"), 0o644)
	_, errOut := run(1, "add", "--path", repo)
	if !strings.Contains(errOut, "adapter boom") {
		t.Fatalf("broken adapter not surfaced: %s", errOut)
	}
}

// merge combines module stores into a derived view (original producer
// metadata intact); export dumps it for other tools.
func TestMergeAndExport(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)

	run := func(wantCode int, args ...string) (string, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := Run(args, &out, &errb)
		if code != wantCode {
			t.Fatalf("%v: exit %d (want %d): %s", args, code, wantCode, errb.String())
		}
		return out.String(), errb.String()
	}

	// Two modules with one doc each.
	repoA, repoB := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(repoA, "a.md"), []byte("# Service A\n"), 0o644)
	os.WriteFile(filepath.Join(repoB, "b.md"), []byte("# Service B\n"), 0o644)
	run(0, "add", "--path", repoA)
	run(0, "add", "--path", repoB)

	keyA, _ := filepath.Abs(repoA)
	out, _ := run(0, "merge", filepath.Base(keyA), repoB, "--into", "everything")
	if !strings.Contains(out, "merged → ") {
		t.Fatalf("merge output: %s", out)
	}

	// The merged store is queryable and both modules are in it.
	var g struct {
		Nodes []struct {
			ID       string            `json:"id"`
			Metadata map[string]string `json:"metadata"`
		}
	}
	// export resolves the store by --path basename → the "everything" module.
	out, _ = run(0, "export", "--store", storeRoot, "--path", filepath.Join(storeRoot, "everything"))
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		t.Fatalf("export json: %v\n%s", err, out)
	}
	if len(g.Nodes) < 4 { // 2 docs + 2 sections
		t.Fatalf("merged export too small: %d nodes", len(g.Nodes))
	}
	for _, n := range g.Nodes {
		if n.Metadata["producer"] == "" || strings.HasPrefix(n.Metadata["producer"], "merge:") {
			t.Fatalf("merge clobbered provenance on %s: %q", n.ID, n.Metadata["producer"])
		}
	}

	// Unknown module fails clearly; dot export renders.
	_, errOut := run(1, "merge", "nope-not-a-module", "--into", "x")
	if !strings.Contains(errOut, "no module") {
		t.Fatalf("unknown module error: %s", errOut)
	}
	out, _ = run(0, "export", "--format", "dot", "--path", repoA)
	if !strings.Contains(out, "digraph ctxoptimize") {
		t.Fatalf("dot export: %s", out)
	}
}

func TestUnknownCommandExits2(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Run([]string{"bogus"}, &out, &errb); code != 2 {
		t.Fatalf("want exit 2, got %d", code)
	}
	if !strings.Contains(errb.String(), "unknown command") {
		t.Fatalf("stderr: %s", errb.String())
	}
}

func TestVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Run([]string{"version"}, &out, &errb); code != 0 {
		t.Fatal("version failed")
	}
	if !strings.Contains(out.String(), "ctx-optimize") {
		t.Fatalf("version output: %s", out.String())
	}
}
