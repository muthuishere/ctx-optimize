package app

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// captureProgress redirects the live progress stream (a package var, defaulting
// to os.Stderr) the way reconcile_test.go does.
func captureProgress(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := progressOut
	progressOut = &buf
	t.Cleanup(func() { progressOut = old })
	var _ io.Writer = &buf
	return &buf
}

// Issue #12: a single long task printed nothing while it ran, so a big gather
// read as hung. The heartbeat is the fix, and it has two properties that need
// pinning in opposite directions: it must FIRE for a task that outlives the
// interval, and it must stay SILENT for one that does not (otherwise every
// normal repo gains noise).
func TestHeartbeatSilentForFastTasks(t *testing.T) {
	if progressHeartbeat < time.Second {
		t.Fatalf("progressHeartbeat = %v — too short to keep normal gathers quiet", progressHeartbeat)
	}
	repo, storeRoot := t.TempDir(), t.TempDir()
	writeRepoAt(t, repo, map[string]string{
		"a.go":                     "package a\n\nfunc A() {}\n",
		"m1/b.go":                  "package m1\n\nfunc B() {}\n",
		".ctxoptimize/config.json": `{"name":"hbfast","modules":[{"path":"m1"}]}`,
	})
	prog := captureProgress(t)
	out, code := runCLIIn(t, storeRoot, "add", repo)
	if code != 0 {
		t.Fatalf("add failed: %s", out)
	}
	got := prog.String()
	if strings.Contains(got, "still running") {
		t.Errorf("a fast gather must not print a heartbeat:\n%s", got)
	}
	// The start lines are the other half of the fix and are always present:
	// you can see what is running instead of inferring it.
	if !strings.Contains(got, "→ .") {
		t.Errorf("tasks must announce when they START, not only when they finish:\n%s", got)
	}
	// And completion ticks still land, so the counter is unchanged.
	if !strings.Contains(got, "/2]") {
		t.Errorf("completion ticks must still report i/N:\n%s", got)
	}
}

// The heartbeat interval is the one number here, so it is asserted rather than
// left implicit: long enough that a normal repo is quiet, short enough that a
// chromium-scale residual is never silent for long.
func TestHeartbeatIntervalIsSane(t *testing.T) {
	if progressHeartbeat > time.Minute {
		t.Errorf("progressHeartbeat = %v — too long; the point is that silence never means 'no idea if this is alive'", progressHeartbeat)
	}
}
