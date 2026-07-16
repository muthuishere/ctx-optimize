package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo makes a git repo with one committed Go file so gather records
// HEAD provenance and drift is checkable.
func gitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	writeFiles(t, repo, map[string]string{
		"pay.go": "package pay\n\nfunc PayInvoice() {}\n\nfunc helper() { PayInvoice() }\n",
	})
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"},
		{"add", "-A"}, {"commit", "-qm", "one"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

// The verify matrix (ADR 2026-07-16-verify-verb): ok / missing-node with
// suggestions / out-of-range / missing-file / drifted after an edit /
// multi-claim exit code. Verify never fuzzes.
func TestVerifyMatrix(t *testing.T) {
	repo := gitRepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)

	// Exact label → ok, drift clean.
	out, _ := runCLI(t, 0, "verify", "PayInvoice", "--path", repo, "--json")
	var res struct {
		Claims []verifyResult `json:"claims"`
		OK     bool           `json:"ok"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("verify --json: %v\n%s", err, out)
	}
	if !res.OK || res.Claims[0].Verdict != "ok" || res.Claims[0].Drift != "clean" {
		t.Fatalf("committed claim must verify clean: %+v", res.Claims)
	}

	// file:range claim → ok; out-of-range → fails.
	runCLI(t, 0, "verify", "pay.go:L3-L3", "--path", repo)
	out, errOut := runCLI(t, 1, "verify", "pay.go:L999", "--path", repo)
	if !strings.Contains(out+errOut, "out-of-range") {
		t.Fatalf("expected out-of-range: %s%s", out, errOut)
	}

	// Fabricated node → missing-node, with the near-name in the detail. A
	// NEAR name must not verify (verify never fuzzes).
	out, errOut = runCLI(t, 1, "verify", "PayInvoic", "--path", repo)
	if !strings.Contains(out+errOut, "missing-node") {
		t.Fatalf("near-name must not verify: %s%s", out, errOut)
	}

	// Missing file.
	out, errOut = runCLI(t, 1, "verify", "gone.go:L1", "--path", repo)
	if !strings.Contains(out+errOut, "missing-file") {
		t.Fatalf("expected missing-file: %s%s", out, errOut)
	}

	// Multi-claim: one bad claim fails the whole call (before the drift edit
	// below, so the good claim is still clean).
	_, errOut = runCLI(t, 1, "verify", "pay.go:L1", "nope-not-real", "--path", repo)
	if !strings.Contains(errOut, "1 of 2 claims failed") {
		t.Fatalf("multi-claim exit: %s", errOut)
	}

	// Edit the file (uncommitted) → drifted.
	if err := os.WriteFile(filepath.Join(repo, "pay.go"),
		[]byte("package pay\n\n// moved\nfunc PayInvoice() {}\n\nfunc helper() { PayInvoice() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut = runCLI(t, 1, "verify", "PayInvoice", "--path", repo)
	if !strings.Contains(out+errOut, "drifted") {
		t.Fatalf("edited file must report drifted: %s%s", out, errOut)
	}
}

// Ambiguous fuzzy asks refuse with candidates by default; --fuzzy takes the
// deterministic top candidate; a clear fuzzy winner announces itself.
func TestCardAmbiguityRefusalAndFuzzyOptIn(t *testing.T) {
	repo := t.TempDir()
	writeFiles(t, repo, map[string]string{
		"a.go": "package a\n\nfunc PayInvoiceRetry() {}\n\nfunc PayInvoiceOnce() {}\n",
	})
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)

	out, errOut := runCLI(t, 1, "card", "PayInvoice", "--path", repo)
	if !strings.Contains(out+errOut, "refusing to guess") || !strings.Contains(out+errOut, "PayInvoiceOnce") {
		t.Fatalf("tie must refuse with candidates:\n%s%s", out, errOut)
	}
	out, _ = runCLI(t, 0, "card", "PayInvoice", "--fuzzy", "--path", repo)
	if !strings.Contains(out, "PayInvoiceOnce") {
		t.Fatalf("--fuzzy must take the top candidate:\n%s", out)
	}

	// Clear winner: single near name resolves, loudly labeled.
	repo2 := t.TempDir()
	writeFiles(t, repo2, map[string]string{"b.go": "package b\n\nfunc PayInvoiceRetry() {}\n"})
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--path", repo2)
	runCLI(t, 0, "add", "--path", repo2)
	out, _ = runCLI(t, 0, "card", "PayInvoice", "--path", repo2)
	if !strings.Contains(out, "[resolved via fuzzy") {
		t.Fatalf("fuzzy answer must announce itself:\n%s", out)
	}
}
