package app

import (
	"bytes"
	"testing"
)

// A mistyped flag must be an ERROR, not silence. `boundaries --sensitve` used
// to print the full list including non-secrets and exit 0, so the reader
// believed they were seeing only credentials — the failure always errs toward
// showing MORE than was asked for.
func TestUnknownFlagIsRejected(t *testing.T) {
	for _, tc := range []struct{ verb, flag string }{
		{"boundaries", "--sensitve"}, // the reported case: one letter short
		{"nodes", "--kindd"},
		{"drift", "--nosuchflag"},
		{"query", "--budgett"},
		{"card", "--jsonn"},
	} {
		err := checkFlags(tc.verb, []string{tc.flag, "x"})
		if err == nil {
			t.Errorf("%s %s: accepted silently — a typo must not widen output", tc.verb, tc.flag)
		}
	}
	// The suggestion is the point: a bare rejection makes the user re-read docs.
	err := checkFlags("boundaries", []string{"--sensitve"})
	if err == nil || !contains(err.Error(), "--sensitive") {
		t.Errorf("no suggestion for a one-edit typo: %v", err)
	}
}

// The allowlist must not reject anything that legitimately worked. These are
// real invocations from the test suite and the shipped docs — the first cut of
// this table broke `install --claude`, because that flag is read as
// f.bools[loopVar] and a scan for f.bools["literal"] cannot see it.
func TestKnownFlagsStillAccepted(t *testing.T) {
	for _, tc := range []struct {
		verb string
		args []string
	}{
		{"install", []string{"--claude"}},
		{"install", []string{"--skills", "--hooks"}},
		{"update", []string{"--claude"}},
		{"update", []string{"--check"}},
		{"up", []string{"--sources=always", "--strict"}},
		{"add", []string{".", "--force", "--jobs", "4"}},
		{"nodes", []string{"--kind", "port", "--where", "scope=external"}},
		{"edges", []string{"--relation", "calls", "--json"}},
		{"deps", []string{"--scope", "dev"}},
		{"boundaries", []string{"--sensitive", "--json"}},
		{"boundaries", []string{"verify", "--strict"}},
		{"query", []string{"terms", "--budget", "2000"}},
		{"card", []string{"Sym", "--include-content"}},
		{"search", []string{"pat", "--ext", ".go", "--count"}},
		{"store", []string{"delete", "--yes"}},
		{"serve", []string{"--host", "127.0.0.1", "--port", "4747"}},
		{"export", []string{"--format", "dot", "--out", "g.dot"}},
		{"nodes", []string{"--kind", "port", "--", "--json"}}, // bare -- separator
	} {
		if err := checkFlags(tc.verb, tc.args); err != nil {
			t.Errorf("%s %v: newly rejected — %v", tc.verb, tc.args, err)
		}
	}
}

// A repeated string flag silently kept only the LAST value, so
// `--where a=1 --where b=2` answered as if only b had been asked — a plausible
// answer, which is worse than an error. `where` now ANDs; everything else
// repeating is rejected rather than silently resolved.
func TestRepeatedFlags(t *testing.T) {
	if err := checkFlags("nodes", []string{"--where", "a=1", "--where", "b=2"}); err != nil {
		t.Errorf("repeated --where must be allowed (it ANDs): %v", err)
	}
	if err := checkFlags("nodes", []string{"--kind", "port", "--kind", "file"}); err == nil {
		t.Error("repeated --kind accepted; only the last would have taken effect")
	}
}

// The join, not just the acceptance: two --where conditions must both survive
// into the parsed value, comma-separated, which is the AND graphfilter parses.
func TestRepeatedWhereJoinsRatherThanOverwrites(t *testing.T) {
	f := parseFlags([]string{"--where", "transport=network.http", "--where", "sensitive=true"})
	got := f.strs["where"]
	want := "transport=network.http,sensitive=true"
	if got != want {
		t.Errorf("--where join = %q, want %q — the first condition was dropped", got, want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// The unit tests above prove checkFlags is CORRECT; this proves it is WIRED.
// Without it, deleting the call in Run leaves every test above green — which is
// the same shape as the perf gate that recorded its own measurement and could
// never fail. Assert through the real entry point, on exit code.
func TestUnknownFlagIsRejectedThroughRun(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	var out, errb bytes.Buffer
	if code := Run([]string{"boundaries", "--sensitve"}, &out, &errb); code != 2 {
		t.Errorf("exit %d, want 2 — a mistyped flag reached the command instead of being rejected", code)
	}
	if !contains(errb.String(), "--sensitive") {
		t.Errorf("no suggestion on stderr: %q", errb.String())
	}
	// And the correct spelling must still run.
	out.Reset()
	errb.Reset()
	if code := Run([]string{"boundaries", "--sensitive"}, &out, &errb); code == 2 {
		t.Errorf("correct spelling rejected: %q", errb.String())
	}
}
