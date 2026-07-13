package app

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestFreshnessDetectsStaleStore proves the CEO use case: after `add`, the
// store is fresh vs git HEAD; a NEW commit makes it stale, and both `status`
// and the `fresh` gate (exit code 1) surface that so an agent re-adds instead
// of answering from a snapshot behind HEAD. Hermetic: real temp git repo +
// t.TempDir store. Skips only if git is unavailable.
func TestFreshnessDetectsStaleStore(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH; freshness needs a real repo to detect stale")
	}
	repo := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())

	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_AUTHOR_DATE=2026-01-01T00:00:00",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_COMMITTER_DATE=2026-01-01T00:00:00",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(wantCode int, args ...string) (string, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := Run(args, &out, &errb)
		if code != wantCode {
			t.Fatalf("%v: exit %d (want %d): out=%s err=%s", args, code, wantCode, out.String(), errb.String())
		}
		return out.String(), errb.String()
	}

	gitRun("init")
	gitRun("config", "user.email", "t@t")
	gitRun("config", "user.name", "t")
	write("design.md", "# Payment Service\n\n## Refund Flow\n\nSee the ledger.\n")
	gitRun("add", ".")
	gitRun("commit", "-m", "initial")

	run(0, "init", "--path", repo)
	run(0, "add", repo, "--path", repo)

	// source.json was written with the repo's HEAD.
	srcPath := filepath.Join(os.Getenv("CTX_OPTIMIZE_STORE"), filepath.Base(repo), "source.json")
	if _, err := os.Stat(srcPath); err != nil {
		t.Fatalf("add did not record source provenance: %v", err)
	}

	// FRESH right after add: status says up to date, `fresh` exits 0.
	statusOut, _ := run(0, "status", "--path", repo)
	if !strings.Contains(statusOut, "up to date") {
		t.Fatalf("status should report fresh right after add:\n%s", statusOut)
	}
	freshOut, _ := run(0, "fresh", "--path", repo) // exit 0 = fresh
	if !strings.Contains(freshOut, "up to date") {
		t.Fatalf("fresh verb should confirm fresh:\n%s", freshOut)
	}

	// A NEW commit moves HEAD; the store now predates the code.
	write("design.md", "# Payment Service\n\n## Refund Flow\n\nNow with disputes.\n")
	gitRun("add", ".")
	gitRun("commit", "-m", "second")

	// STALE: status names the SHAs and advises re-add; `fresh` exits 1.
	statusOut, _ = run(0, "status", "--path", repo)
	if !strings.Contains(statusOut, "STALE") || !strings.Contains(statusOut, "ctx-optimize add") {
		t.Fatalf("status should report STALE + advise re-add after a new commit:\n%s", statusOut)
	}
	run(1, "fresh", "--path", repo) // exit 1 = stale (the agent/hook gate)

	// The JSON gate exposes the machine-readable verdict for a hook.
	jsonOut, _ := freshCapture(t, "fresh", "--path", repo, "--json")
	var fj struct {
		Fresh     string `json:"fresh"`
		Freshness []struct {
			State       string `json:"state"`
			StoreHead   string `json:"store_head"`
			CurrentHead string `json:"current_head"`
		} `json:"freshness"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &fj); err != nil {
		t.Fatalf("fresh --json not parseable: %v: %s", err, jsonOut)
	}
	if fj.Fresh != "stale" {
		t.Fatalf("fresh --json state = %q, want stale", fj.Fresh)
	}
	if len(fj.Freshness) != 1 || fj.Freshness[0].StoreHead == fj.Freshness[0].CurrentHead {
		t.Fatalf("expected one source with differing store/current heads: %+v", fj.Freshness)
	}

	// A re-add refreshes the provenance → fresh again (the loop closes).
	run(0, "add", repo, "--path", repo)
	run(0, "fresh", "--path", repo) // exit 0 = fresh once more
}

// freshCapture runs `fresh` and returns stdout regardless of exit code
// (stale = exit 1 is expected), for JSON assertions.
func freshCapture(t *testing.T, args ...string) (string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	Run(args, &out, &errb)
	return out.String(), errb.String()
}
