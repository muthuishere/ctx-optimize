package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Co-change lane through the CLI: a real (scripted) git history produces
// co_changed_with edges that `card` surfaces as neighbors, and re-adding is
// idempotent (producer-scoped Replace restates, never accumulates).
func TestAddSurfacesCoChangeEdges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)

	gitRun := func(args ...string) {
		t.Helper()
		base := []string{"-C", repo,
			"-c", "user.name=test", "-c", "user.email=test@example.com",
			"-c", "commit.gpgsign=false"}
		cmd := exec.Command("git", append(base, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	commit := func(msg string, files ...string) {
		t.Helper()
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(repo, f), []byte("# "+msg+" "+f+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		gitRun(append([]string{"add", "--"}, files...)...)
		gitRun("commit", "-q", "-m", msg)
	}
	gitRun("init", "-q", "-b", "main")
	commit("c1", "guide.md", "api.md")
	commit("c2", "guide.md", "api.md")
	commit("c3", "guide.md", "api.md")

	run := func(wantCode int, args ...string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := Run(args, &out, &errb)
		if code != wantCode {
			t.Fatalf("%v: exit %d (want %d): %s", args, code, wantCode, errb.String())
		}
		return out.String()
	}
	run(0, "init", "--path", repo)
	out := run(0, "add", repo, "--path", repo)
	if !strings.Contains(out, "git-history: 1 co-change edges") {
		t.Fatalf("add did not report the co-change lane: %s", out)
	}

	card := run(0, "card", "guide.md", "--path", repo)
	if !strings.Contains(card, "co_changed_with") || !strings.Contains(card, "api.md") {
		t.Fatalf("card must surface the co-change neighbor:\n%s", card)
	}

	edgesFile := filepath.Join(storeRoot, filepath.Base(repo), "graph", "edges.ndjson")
	before, err := os.ReadFile(edgesFile)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(before, []byte("co_changed_with")); n != 1 {
		t.Fatalf("want exactly 1 co_changed_with edge, got %d", n)
	}
	// Re-add: idempotent — same edges, no accumulation.
	run(0, "add", repo, "--path", repo)
	after, err := os.ReadFile(edgesFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("re-add changed edges.ndjson:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}
