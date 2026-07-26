package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// "Why does one break stop the whole?" — it did, at two of three levels, and
// a retired producer's nodes never went away at all. These tests pin all of it.
//
//	one bad FILE      → already contained (internal/extract/code)
//	one bad PRODUCER  → aborted every LATER lane, and a failing adapter threw
//	                    away an already-extracted code/docs/manifest gather
//	one bad MODULE    → skipped the navigator, denying federation over the
//	                    modules that succeeded
//	a RETIRED producer→ Replace is producer-scoped, so its nodes lived forever

func writeRepoAt(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Run returns an exit CODE, not an error — 0 is success.
func runCLIIn(t *testing.T, storeRoot string, args ...string) (string, int) {
	t.Helper()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb)
	return out.String() + errb.String(), code
}

// A broken adapter used to discard the whole gather. Now it costs only itself,
// the rest is committed, and the failure is both printed and returned.
func TestBrokenAdapterDoesNotDiscardTheGather(t *testing.T) {
	repo := t.TempDir()
	storeRoot := t.TempDir()
	writeRepoAt(t, repo, map[string]string{
		"a.go":                         "package a\n\nfunc Real() {}\n",
		".ctxoptimize/config.json":     `{"name":"contain"}`,
		".ctxoptimize/adapters/bad.sh": "echo 'not json at all'\nexit 3\n",
	})

	out, code := runCLIIn(t, storeRoot, "add", repo)
	if code == 0 {
		t.Error("a failed lane must still surface as a non-zero exit — silence would hide the gap")
	}
	if !strings.Contains(out, "LANE FAILED") {
		t.Errorf("the failure must be reported per lane:\n%s", out)
	}
	// The point: the code lane landed anyway.
	nodesOut, _ := runCLIIn(t, storeRoot, "nodes", "--path", repo, "--json")
	if !strings.Contains(nodesOut, "Real") {
		t.Errorf("one broken adapter discarded a successful code gather:\n%s", nodesOut)
	}
}

// Containment is only honest if the incompleteness is recorded — a store
// missing a lane must not answer as though it has one.
func TestPartialGatherIsRecordedAsPartial(t *testing.T) {
	repo := t.TempDir()
	storeRoot := t.TempDir()
	writeRepoAt(t, repo, map[string]string{
		"a.go":                         "package a\n\nfunc Real() {}\n",
		".ctxoptimize/config.json":     `{"name":"partialrec"}`,
		".ctxoptimize/adapters/bad.sh": "exit 3\n",
	})
	if _, code := runCLIIn(t, storeRoot, "add", repo); code == 0 {
		t.Fatal("want a non-zero exit from the failed lane")
	}
	raw, rerr := os.ReadFile(filepath.Join(storeRoot, "partialrec", "source.json"))
	if rerr != nil {
		t.Fatalf("no source.json written: %v", rerr)
	}
	// source.json holds one record per gathered source root.
	var recs []struct {
		Partial []string `json:"partial"`
		TreeSig string   `json:"tree_sig"`
	}
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 source record, got %d", len(recs))
	}
	rec := recs[0]
	if len(rec.Partial) == 0 {
		t.Error("a partial gather must record WHICH lanes failed")
	}
	// And it must not be short-circuited as "unchanged" next time, which would
	// freeze the gap in place.
	if rec.TreeSig != "" {
		t.Error("a partial gather must clear the tree signature so the next run really retries")
	}
}

// Replace is producer-scoped, so a producer that stops running is never
// replaced. Deleting an adapter left its nodes in the graph forever — measured,
// including under --force.
func TestRetiredProducerIsReportedThenPrunable(t *testing.T) {
	repo := t.TempDir()
	storeRoot := t.TempDir()
	adapter := filepath.Join(repo, ".ctxoptimize", "adapters", "mine.sh")
	writeRepoAt(t, repo, map[string]string{
		"a.go":                          "package a\n\nfunc Real() {}\n",
		".ctxoptimize/config.json":      `{"name":"retired"}`,
		".ctxoptimize/adapters/mine.sh": `echo '{"producer":"mine","nodes":[{"id":"custom://ghost","label":"Ghost","kind":"service","file_type":"config","source":"adapter"}],"edges":[]}'` + "\n",
	})
	if out, code := runCLIIn(t, storeRoot, "add", repo); code != 0 {
		t.Fatalf("add failed: %s", out)
	}
	if out, _ := runCLIIn(t, storeRoot, "nodes", "--path", repo, "--json"); !strings.Contains(out, "custom://ghost") {
		t.Fatal("precondition: the adapter's node should be in the store")
	}

	if err := os.Remove(adapter); err != nil {
		t.Fatal(err)
	}
	// Reported, not silently deleted: absence can mean "retired" OR "did not
	// run this time", and the two are indistinguishable from the store.
	out, _ := runCLIIn(t, storeRoot, "add", repo)
	if !strings.Contains(out, "retired producer") {
		t.Errorf("a retired producer must be reported, not left silently stale:\n%s", out)
	}

	// --rebuild is the guaranteed clean slate.
	if out, code := runCLIIn(t, storeRoot, "add", repo, "--rebuild"); code != 0 {
		t.Fatalf("rebuild failed: %s", out)
	}
	after, _ := runCLIIn(t, storeRoot, "nodes", "--path", repo, "--json")
	if strings.Contains(after, "custom://ghost") {
		t.Error("--rebuild must not carry a retired producer's nodes across")
	}
	if !strings.Contains(after, "Real") {
		t.Error("--rebuild must re-gather the real content")
	}
}
