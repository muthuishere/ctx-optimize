package githistory

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	base := []string{"-C", dir,
		"-c", "user.name=test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false"}
	cmd := exec.Command("git", append(base, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// commit writes each file (dirs created) and commits exactly that set.
func commit(t *testing.T, dir, msg string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(msg+" "+f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, append([]string{"add", "--"}, files...)...)
	git(t, dir, "commit", "-q", "-m", msg)
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	return dir
}

func edgeMap(t *testing.T, b *schema.Batch) map[string]schema.Edge {
	t.Helper()
	m := map[string]schema.Edge{}
	for _, e := range b.Edges {
		if e.Source >= e.Target {
			t.Fatalf("pair not lexically ordered: %q >= %q", e.Source, e.Target)
		}
		m[e.Source+"|"+e.Target] = e
	}
	if len(m) != len(b.Edges) {
		t.Fatalf("duplicate pairs in batch")
	}
	return m
}

func TestExtractCoChangePairs(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)

	// a.go+a_test.go co-change 3x (qualifies); b.go rides along once (support 1).
	commit(t, repo, "c1", "a.go", "a_test.go", "b.go")
	commit(t, repo, "c2", "a.go", "a_test.go")
	commit(t, repo, "c3", "a.go", "a_test.go")
	// a.go alone once more: ratio for a.go becomes 3/4.
	commit(t, repo, "c4", "a.go")

	// Bulk commit >20 files must be ignored: x00..x20 + the qualifying pair.
	bulk := []string{"a.go", "a_test.go"}
	for i := 0; i < 21; i++ {
		bulk = append(bulk, fmt.Sprintf("bulk/x%02d.go", i))
	}
	commit(t, repo, "bulk", bulk...)

	// Merge commit must be ignored (--no-merges).
	git(t, repo, "checkout", "-q", "-b", "side")
	commit(t, repo, "side", "a.go", "a_test.go")
	git(t, repo, "checkout", "-q", "main")
	commit(t, repo, "mainline", "c.md")
	git(t, repo, "merge", "-q", "--no-ff", "-m", "merge side", "side")

	b, err := Extract(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Nodes) != 0 {
		t.Fatalf("producer must emit edges only, got %d nodes", len(b.Nodes))
	}
	if b.Producer != Producer {
		t.Fatalf("producer = %q", b.Producer)
	}
	m := edgeMap(t, b)
	e, ok := m["a.go|a_test.go"]
	if !ok {
		t.Fatalf("missing a.go|a_test.go pair; edges: %v", b.Edges)
	}
	// c1+c2+c3 + the side-branch commit = 4; bulk and merge excluded.
	if got := e.Metadata["support"]; got != "4" {
		t.Fatalf("support = %s, want 4 (bulk/merge must not count)", got)
	}
	if e.Confidence != schema.Inferred {
		t.Fatalf("confidence = %s", e.Confidence)
	}
	if got := e.Metadata["synthesized_by"]; got != "git-cochange" {
		t.Fatalf("synthesized_by = %q", got)
	}
	// a.go appears in c1..c4 + side = 5 counted commits → 4/5.
	if got := e.Metadata["confidence_ratio"]; got != "0.80" {
		t.Fatalf("confidence_ratio = %s, want 0.80", got)
	}
	if _, ok := m["a.go|b.go"]; ok {
		t.Fatalf("support-1 pair a.go|b.go must not qualify (minSupport=%d)", minSupport)
	}
	for k := range m {
		if len(k) > 5 && k[:5] == "bulk/" {
			t.Fatalf("bulk commit leaked pair %s", k)
		}
	}
}

func TestDeletedFilePairsVanish(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	commit(t, repo, "c1", "c.go", "d.go")
	commit(t, repo, "c2", "c.go", "d.go")
	commit(t, repo, "c3", "c.go", "d.go")

	b, err := Extract(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := edgeMap(t, b)["c.go|d.go"]; !ok {
		t.Fatalf("expected c.go|d.go before deletion; edges: %v", b.Edges)
	}

	git(t, repo, "rm", "-q", "c.go")
	git(t, repo, "commit", "-q", "-m", "drop c.go")
	b, err = Extract(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range b.Edges {
		if e.Source == "c.go" || e.Target == "c.go" {
			t.Fatalf("deleted file still paired: %v", e)
		}
	}
}

func TestSecretNamesSkipped(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	commit(t, repo, "c1", ".env.local", "config.go")
	commit(t, repo, "c2", ".env.local", "config.go")
	commit(t, repo, "c3", ".env.local", "config.go")
	b, err := Extract(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range b.Edges {
		if e.Source == ".env.local" || e.Target == ".env.local" {
			t.Fatalf("secret-named file paired: %v", e)
		}
	}
}

func TestNonRepoIsEmptyNotError(t *testing.T) {
	requireGit(t)
	b, err := Extract(t.TempDir())
	if err != nil {
		t.Fatalf("non-repo must not error: %v", err)
	}
	if len(b.Edges) != 0 || len(b.Nodes) != 0 {
		t.Fatalf("non-repo must be empty, got %d/%d", len(b.Nodes), len(b.Edges))
	}
}

// Module subdir: ids must be RELATIVE TO THE MODULE DIR (matching that
// store's node ids), and root-only pairs must not leak in.
func TestSubdirScopingRelativizesIDs(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	commit(t, repo, "c1", "sub/x.go", "sub/y.go", "root.go")
	commit(t, repo, "c2", "sub/x.go", "sub/y.go", "top.md")
	commit(t, repo, "c3", "sub/x.go", "sub/y.go")
	commit(t, repo, "r1", "root.go", "top.md")
	commit(t, repo, "r2", "root.go", "top.md")
	commit(t, repo, "r3", "root.go", "top.md")

	b, err := Extract(filepath.Join(repo, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	m := edgeMap(t, b)
	if _, ok := m["x.go|y.go"]; !ok {
		t.Fatalf("want module-relative pair x.go|y.go; edges: %v", b.Edges)
	}
	for k := range m {
		if k != "x.go|y.go" {
			t.Fatalf("root files leaked into module scope: %s", k)
		}
	}

	// Root scope with sub/ excluded (fan-out residual task): only root pair.
	b, err = ExtractExcluding(repo, []string{filepath.Join(repo, "sub")})
	if err != nil {
		t.Fatal(err)
	}
	m = edgeMap(t, b)
	if _, ok := m["root.go|top.md"]; !ok {
		t.Fatalf("want root.go|top.md; edges: %v", b.Edges)
	}
	for k := range m {
		if k != "root.go|top.md" {
			t.Fatalf("excluded subtree leaked: %s", k)
		}
	}
}

func TestPairCapKeepsHighestSupport(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	// Two disjoint 20-file groups, 3 commits each: 2 × C(20,2) = 380 pairs at
	// support 3 (well over the cap, well inside the commit window) — plus one
	// pair at support 4 that must survive the cut.
	for g := 0; g < 2; g++ {
		var files []string
		for i := 0; i < 20; i++ {
			files = append(files, "g"+strconv.Itoa(g)+"/f"+strconv.Itoa(i)+".go")
		}
		for c := 0; c < 3; c++ {
			commit(t, repo, fmt.Sprintf("g%d-c%d", g, c), files...)
		}
	}
	commit(t, repo, "h1", "hot.go", "hot_test.go")
	commit(t, repo, "h2", "hot.go", "hot_test.go")
	commit(t, repo, "h3", "hot.go", "hot_test.go")
	commit(t, repo, "h4", "hot.go", "hot_test.go")

	b, err := Extract(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Edges) != maxPairs {
		t.Fatalf("cap: got %d edges, want %d", len(b.Edges), maxPairs)
	}
	if _, ok := edgeMap(t, b)["hot.go|hot_test.go"]; !ok {
		t.Fatalf("highest-support pair fell out of the cap")
	}
}
