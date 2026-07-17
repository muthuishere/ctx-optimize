package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readInstr(t *testing.T, repo string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(InstructionsFile)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEnsureInstructionsCreates(t *testing.T) {
	repo := t.TempDir()
	changed, err := ensureInstructions(repo, "0.5.0")
	if err != nil || !changed {
		t.Fatalf("create: changed=%v err=%v", changed, err)
	}
	s := readInstr(t, repo)
	if !strings.Contains(s, instrBeginPrefix+"0.5.0"+instrBeginSuffix) {
		t.Fatalf("version stamp missing:\n%s", s[:120])
	}
	// The card is self-contained: intent verbs, verify, sources, remote, up.
	for _, want := range []string{"change-plan", "verify", "adapters help", "ctx-optimize add BILLING_DB_URL",
		"~/.config/ctx-optimize/.env", "remote push", "ctx-optimize up", "grep"} {
		if !strings.Contains(s, want) {
			t.Fatalf("card missing %q", want)
		}
	}
	// Idempotent at the same version.
	if changed, err := ensureInstructions(repo, "0.5.0"); err != nil || changed {
		t.Fatalf("second run must be a no-op: changed=%v err=%v", changed, err)
	}
}

func TestEnsureInstructionsUpgradePreservesUserText(t *testing.T) {
	repo := t.TempDir()
	if _, err := ensureInstructions(repo, "0.5.0"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(repo, filepath.FromSlash(InstructionsFile))
	s := readInstr(t, repo)
	// User edits OUTSIDE the managed block.
	edited := "# Team notes\n\nour billing DB is the read replica\n\n" + s + "\n## Appendix\nlocal quirks\n"
	if err := os.WriteFile(p, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := ensureInstructions(repo, "0.6.0")
	if err != nil || !changed {
		t.Fatalf("upgrade: changed=%v err=%v", changed, err)
	}
	got := readInstr(t, repo)
	if !strings.Contains(got, "our billing DB is the read replica") || !strings.Contains(got, "local quirks") {
		t.Fatalf("user text outside the block lost:\n%s", got)
	}
	if !strings.Contains(got, instrBeginPrefix+"0.6.0"+instrBeginSuffix) {
		t.Fatal("block not upgraded to the new stamp")
	}
	if strings.Count(got, instrBeginPrefix) != 1 || strings.Count(got, instrEnd) != 1 {
		t.Fatal("managed block must stay single")
	}
}

func TestEnsureInstructionsUpgradeOnly(t *testing.T) {
	repo := t.TempDir()
	if _, err := ensureInstructions(repo, "9.9.9"); err != nil {
		t.Fatal(err)
	}
	// An older binary (incl. a 0.0.0-dev build) must never rewrite it.
	for _, older := range []string{"0.5.0", "0.0.0-dev"} {
		changed, err := ensureInstructions(repo, older)
		if err != nil || changed {
			t.Fatalf("older binary %s rewrote a newer file: changed=%v err=%v", older, changed, err)
		}
	}
	if !strings.Contains(readInstr(t, repo), instrBeginPrefix+"9.9.9"+instrBeginSuffix) {
		t.Fatal("newer stamp lost")
	}
}

func TestEnsureInstructionsRemovedBlockIsUserOwned(t *testing.T) {
	repo := t.TempDir()
	if _, err := ensureInstructions(repo, "0.5.0"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(repo, filepath.FromSlash(InstructionsFile))
	const user = "# our own doc, block deliberately deleted\n"
	if err := os.WriteFile(p, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := ensureInstructions(repo, "9.9.9")
	if err != nil || changed {
		t.Fatalf("deleted block must stay deleted: changed=%v err=%v", changed, err)
	}
	if got := readInstr(t, repo); got != user {
		t.Fatalf("user-owned file touched:\n%s", got)
	}
}

func TestScaffoldWritesInstructions(t *testing.T) {
	repo := t.TempDir()
	if err := Scaffold(repo, "my-repo"); err != nil {
		t.Fatal(err)
	}
	s := readInstr(t, repo)
	if !strings.Contains(s, instrBeginPrefix) || !strings.Contains(s, "Pick by intent") {
		t.Fatalf("scaffold did not write the usage card:\n%.120s", s)
	}
}

func TestNewerVersion(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"0.5.0", "0.4.9", true},
		{"0.4.9", "0.5.0", false},
		{"0.5.0", "0.5.0", false},
		{"0.0.0-dev", "0.4.2", false},
		{"0.4.2", "0.0.0-dev", true},
		{"1.0.0", "0.99.99", true},
		{"garbage", "0.1.0", false},
	} {
		if got := newerVersion(tc.a, tc.b); got != tc.want {
			t.Fatalf("newerVersion(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
