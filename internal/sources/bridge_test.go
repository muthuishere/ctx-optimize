package sources

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeCompanion drops an executable shell script named ctx-optimize-adapters
// into its own dir and points PATH at it (os.Executable's sibling dir has no
// companion in a test run, so PATH is the lane exercised).
func fakeCompanion(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script companion stub is not runnable on windows")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, CompanionName)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestBridgeCaptureRejectsTemplateEntries(t *testing.T) {
	_, err := bridgeCapture("postgres://$PGUSER@$PGHOST/app", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "env-var names only") {
		t.Fatalf("template entry must be refused before any exec: %v", err)
	}
}

func TestCompanionPathMissingIsLoudWithInstallHint(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := companionPath()
	if err == nil || !strings.Contains(err.Error(), "npm install -g") {
		t.Fatalf("missing companion must error with the install hint: %v", err)
	}
}

func TestBridgeCaptureParsesCompanionBatch(t *testing.T) {
	fakeCompanion(t, `if [ "$1" != capture ] || [ "$2" != MY_DB_URL ]; then echo "bad argv" >&2; exit 2; fi
echo '{"producer":"source:MY_DB_URL","nodes":[{"id":"db","label":"db","kind":"database","file_type":"source","source":"source:MY_DB_URL"}]}'`)
	b, err := bridgeCapture("MY_DB_URL", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b.Producer != "source:MY_DB_URL" || len(b.Nodes) != 1 {
		t.Fatalf("unexpected batch: %+v", b)
	}
}

func TestBridgeCaptureChildFailureKeepsCauseDropsDoubleWrap(t *testing.T) {
	fakeCompanion(t, `echo "source MY_DB_URL: dial tcp: connection refused — prior nodes kept" >&2; exit 1`)
	_, err := bridgeCapture("MY_DB_URL", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("child stderr cause must surface: %v", err)
	}
	if strings.Contains(err.Error(), "prior nodes kept") || strings.Contains(err.Error(), "source MY_DB_URL:") {
		t.Fatalf("child wrapping must be stripped to avoid doubling: %v", err)
	}
}

func TestBridgeCaptureInvalidBatchRefused(t *testing.T) {
	fakeCompanion(t, `echo '{"nodes":[]}'`)
	_, err := bridgeCapture("MY_DB_URL", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "invalid Batch") {
		t.Fatalf("batch without producer must be refused: %v", err)
	}
}
