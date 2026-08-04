package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #13. Lane containment (2040834) RECORDED which lanes failed but nothing
// READ the record, so `fresh` exited 0 for a store whose code lane had failed —
// defeating the one job `fresh` exists for, which is gating an agent or hook
// before it trusts an answer.

func partialRepo(t *testing.T) (repo, storeRoot string) {
	t.Helper()
	repo, storeRoot = t.TempDir(), t.TempDir()
	writeRepoAt(t, repo, map[string]string{
		"a.go":                         "package a\n\nfunc Alpha() {}\n",
		".ctxoptimize/config.json":     `{"name":"partialst"}`,
		".ctxoptimize/adapters/bad.sh": "exit 3\n",
	})
	return repo, storeRoot
}

func TestFreshReportsPartialNotFresh(t *testing.T) {
	repo, storeRoot := partialRepo(t)
	if _, code := runCLIIn(t, storeRoot, "add", repo); code == 0 {
		t.Fatal("precondition: the failing adapter must make the gather non-zero")
	}

	out, code := runCLIIn(t, storeRoot, "fresh", "--path", repo)
	if code == 0 {
		t.Errorf("fresh exited 0 for an INCOMPLETE store — a hook gating on this would trust missing data:\n%s", out)
	}
	if code != 3 {
		t.Errorf("fresh exit = %d, want 3 (partial) — stale(1) would tell a hook the wrong fix", code)
	}
	if !strings.Contains(out, "PARTIAL") {
		t.Errorf("the verdict must say partial:\n%s", out)
	}
	// Naming the lane is the point: "incomplete" alone leaves the reader unable
	// to judge whether their question is affected.
	if !strings.Contains(out, "adapter") {
		t.Errorf("the verdict must name WHICH lane failed:\n%s", out)
	}
}

func TestStatusSurfacesPartial(t *testing.T) {
	repo, storeRoot := partialRepo(t)
	runCLIIn(t, storeRoot, "add", repo)

	out, _ := runCLIIn(t, storeRoot, "status", "--path", repo)
	if !strings.Contains(out, "PARTIAL") {
		t.Errorf("status must surface the incompleteness:\n%s", out)
	}
}

func TestFreshJSONCarriesPartial(t *testing.T) {
	repo, storeRoot := partialRepo(t)
	runCLIIn(t, storeRoot, "add", repo)

	out, _ := runCLIIn(t, storeRoot, "fresh", "--path", repo, "--json")
	var got struct {
		Fresh     string `json:"fresh"`
		Freshness []struct {
			State   string   `json:"state"`
			Partial []string `json:"partial"`
		} `json:"freshness"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	if got.Fresh != "partial" {
		t.Errorf("fresh = %q, want \"partial\"", got.Fresh)
	}
	if len(got.Freshness) == 0 || len(got.Freshness[0].Partial) == 0 {
		t.Error("--json must carry the failed lanes so a hook can branch on them")
	}
}

// A partial store whose HEAD still matches used to report Fresh — that is the
// precise lie this state exists to stop, so it gets its own test.
func TestPartialBeatsAHeadMatch(t *testing.T) {
	repo, storeRoot := partialRepo(t)
	runCLIIn(t, storeRoot, "add", repo)

	raw, err := os.ReadFile(filepath.Join(storeRoot, "partialst", "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"partial"`) {
		t.Fatal("precondition: the gather must have recorded the failed lane")
	}
	if _, code := runCLIIn(t, storeRoot, "fresh", "--path", repo); code == 0 {
		t.Error("a partial store must never report fresh, whatever its head says")
	}
}

// `up` on a partial store must retry in FULL. The fast path skips adapters, and
// if an adapter is what failed, skipping it would clear the marker while the
// data is still missing.
func TestUpRetriesPartialWithAdapters(t *testing.T) {
	repo, storeRoot := partialRepo(t)
	runCLIIn(t, storeRoot, "add", repo)

	out, _ := runCLIIn(t, storeRoot, "up", "--path", repo)
	if strings.Contains(out, "adapter scripts skipped") {
		t.Errorf("up must NOT take the adapter-skipping fast path for a partial store:\n%s", out)
	}
	if !strings.Contains(out, "adapters INCLUDED") {
		t.Errorf("up must say it is retrying in full:\n%s", out)
	}
	// Still broken, so still partial — never silently cleared.
	if _, code := runCLIIn(t, storeRoot, "fresh", "--path", repo); code != 3 {
		t.Errorf("fresh exit = %d after a full retry that still fails, want 3", code)
	}
}

// A gather that SKIPS a lane must not clear that lane's prior failure: it never
// retried it, so the data is still missing.
func TestSkippedAdaptersKeepsPriorAdapterFailure(t *testing.T) {
	repo, storeRoot := partialRepo(t)
	runCLIIn(t, storeRoot, "add", repo)

	if _, code := runCLIIn(t, storeRoot, "add", repo, "--no-adapters", "--force"); code != 0 {
		// no-adapters run itself succeeds — nothing failed in it
		t.Log("note: --no-adapters run returned non-zero")
	}
	out, code := runCLIIn(t, storeRoot, "fresh", "--path", repo)
	if code != 3 {
		t.Errorf("fresh exit = %d, want 3 — skipping a lane must not clear its failure:\n%s", code, out)
	}
	if !strings.Contains(out, "not retried") {
		t.Errorf("the carried-forward failure must say it was not retried:\n%s", out)
	}
}
