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

	// Ad-hoc remote URL works with NO configured remote.
	adhoc := t.TempDir()
	out, _ = run(0, "remote", "push", "file://"+adhoc, "--path", repo)
	if !strings.Contains(out, "transferred") {
		t.Fatalf("ad-hoc push output: %s", out)
	}

	// No remote at all → clear error, exit 1.
	_, errNoRemote := run(1, "remote", "pull", "--path", repo)
	if !strings.Contains(errNoRemote, "no remote") {
		t.Fatalf("expected no-remote error, got: %s", errNoRemote)
	}

	// Remote: init writes the committable repo file + push + fresh-machine pull.
	remoteDir := t.TempDir()
	run(0, "remote", "init", "file://"+remoteDir, "--path", repo)
	if _, err := os.Stat(filepath.Join(repo, "ctx-optimize.json")); err != nil {
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

// Repo-level ctx-optimize.json: declared adapters run on `add`, and the
// declared remote drives bare push — the file travels with the repo so
// nothing is configured per machine.
func TestRepoConfigAdaptersAndRemote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("adapter command uses sh")
	}
	repo := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	remoteDir := t.TempDir()

	os.MkdirAll(filepath.Join(repo, "hooks"), 0o755)
	batch := `{"producer":"kafka-topics","nodes":[{"id":"kafka://orders","label":"orders","kind":"topic","file_type":"messaging","source":"kafka://orders"}],"edges":[]}`
	os.WriteFile(filepath.Join(repo, "hooks", "kafka.json"), []byte(batch), 0o644)
	cfg := `{"remote":"file://` + remoteDir + `","adapters":[{"name":"kafka","run":"cat hooks/kafka.json"}]}`
	os.WriteFile(filepath.Join(repo, "ctx-optimize.json"), []byte(cfg), 0o644)

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
	out, _ = run(0, "query", "orders topic", "--path", repo)
	if !strings.Contains(out, "kafka://orders") {
		t.Fatalf("adapter node not queryable: %s", out)
	}

	// Bare push uses the repo file's remote; status reports the source.
	out, _ = run(0, "remote", "push", "--path", repo)
	if !strings.Contains(out, "transferred") {
		t.Fatalf("push via repo config: %s", out)
	}
	out, _ = run(0, "status", "--path", repo, "--json")
	var st struct {
		RemoteFrom string `json:"remote_from"`
	}
	json.Unmarshal([]byte(out), &st)
	if st.RemoteFrom != "ctx-optimize.json" {
		t.Fatalf("remote_from = %q, want ctx-optimize.json", st.RemoteFrom)
	}

	// A broken adapter fails the whole add, loudly.
	bad := `{"remote":"","adapters":[{"name":"boom","run":"exit 3"}]}`
	os.WriteFile(filepath.Join(repo, "ctx-optimize.json"), []byte(bad), 0o644)
	_, errOut := run(1, "add", "--path", repo)
	if !strings.Contains(errOut, "adapter boom") {
		t.Fatalf("broken adapter not surfaced: %s", errOut)
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
