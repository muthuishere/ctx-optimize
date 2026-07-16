package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/project"
)

// declareScriptRemote wires a cp-based transport into the repo's committed
// config (ADR 2026-07-16-scripted-remote-transports): one sh script serves
// both directions via CTX_DIRECTION — push copies the store tree into
// hostDir, pull copies it back. Every invocation also appends its CTX_* env
// to envLog so tests can assert exactly what the binary handed the script.
func declareScriptRemote(t *testing.T, repo, hostDir, envLog string) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
set -e
echo "$CTX_DIRECTION|$CTX_STORE_KEY|$CTX_SCOPE_PREFIX|$CTX_STORE_DIR" >> %q
if [ "$CTX_DIRECTION" = "push" ]; then
  rm -rf %q/"$CTX_STORE_KEY"
  mkdir -p %q
  cp -R "$CTX_STORE_DIR" %q/"$CTX_STORE_KEY"
else
  rm -rf "$CTX_STORE_DIR"
  cp -R %q/"$CTX_STORE_KEY" "$CTX_STORE_DIR"
fi
`, envLog, hostDir, hostDir, hostDir, hostDir)
	if err := os.WriteFile(filepath.Join(repo, ".ctxoptimize", "sync.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := project.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Remote = &project.Remote{Push: "sh .ctxoptimize/sync.sh", Pull: "sh .ctxoptimize/sync.sh"}
	if err := project.Save(repo, cfg); err != nil {
		t.Fatal(err)
	}
}

// The full script-transport loop on a monorepo, with the env contract
// asserted at both scopes: root (whole tree, empty prefix) and module dir
// (CTX_SCOPE_PREFIX = the module's store-key segment).
func TestScriptRemoteRoundTripAndScopeEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("transport script uses sh")
	}
	repo := fakeMonorepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--scan", "--yes", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)
	host := t.TempDir()
	envLog := filepath.Join(t.TempDir(), "env.log")
	declareScriptRemote(t, repo, host, envLog)
	key := filepath.Base(repo)

	// Root push: the script gets the ROOT store dir, no prefix.
	out, _ := runCLI(t, 0, "remote", "push", "--path", repo)
	if !strings.Contains(out, "push done") {
		t.Fatalf("push output: %s", out)
	}
	if _, err := os.Stat(filepath.Join(host, key, "services", "api", "graph", "nodes.ndjson")); err != nil {
		t.Fatalf("push script did not land the tree on the host: %v", err)
	}
	log, _ := os.ReadFile(envLog)
	if !strings.Contains(string(log), "push|"+key+"||") {
		t.Fatalf("root push env wrong (want empty prefix): %s", log)
	}

	// Module-scoped push: same tree, but the script is TOLD the scope.
	runCLI(t, 0, "remote", "push", "--path", filepath.Join(repo, "services", "api"))
	log, _ = os.ReadFile(envLog)
	if !strings.Contains(string(log), "push|"+key+"|services/api|") {
		t.Fatalf("module push env wrong (want services/api prefix): %s", log)
	}

	// Teammate: fresh store root, bare pull via the committed config.
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "remote", "pull", "--path", repo)
	st, _ := runCLI(t, 0, "status", "--json", "--path", repo)
	var status struct {
		Nodes  int    `json:"nodes"`
		Remote string `json:"remote"`
	}
	if err := json.Unmarshal([]byte(st), &status); err != nil {
		t.Fatalf("status --json: %v\n%s", err, st)
	}
	if status.Nodes == 0 {
		t.Fatalf("pulled store empty:\n%s", st)
	}
	if status.Remote != "push + pull declared" {
		t.Fatalf("status remote line = %q", status.Remote)
	}
	log, _ = os.ReadFile(envLog)
	if !strings.Contains(string(log), "pull|"+key+"||") {
		t.Fatalf("pull env wrong: %s", log)
	}
}

// A failing transport command fails the verb loudly, naming the command.
func TestScriptRemoteFailsLoudly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("transport script uses sh")
	}
	repo := t.TempDir()
	writeFiles(t, repo, map[string]string{"main.go": "package main\n\nfunc Boot() {}\n"})
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--path", repo)
	cfg, err := project.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Remote = &project.Remote{Push: "exit 3"}
	if err := project.Save(repo, cfg); err != nil {
		t.Fatal(err)
	}
	out, errOut := runCLI(t, 1, "remote", "push", "--path", repo)
	if !strings.Contains(out+errOut, "exit 3") {
		t.Fatalf("failure must name the command: %s%s", out, errOut)
	}
}

// The retired surfaces error with migration guidance, never silently no-op:
// `remote init`, a legacy v0.3 URL config, a missing declaration, and stray
// arguments.
func TestRemoteMigrationErrors(t *testing.T) {
	repo := t.TempDir()
	writeFiles(t, repo, map[string]string{"main.go": "package main\n\nfunc Boot() {}\n"})
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--path", repo)

	_, errInit := runCLI(t, 1, "remote", "init", "file:///x", "--path", repo)
	if !strings.Contains(errInit, "is gone") || !strings.Contains(errInit, "push.js.sample") {
		t.Fatalf("remote init must carry migration guidance: %s", errInit)
	}
	_, errArgs := runCLI(t, 1, "remote", "push", "file:///nope", "--path", repo)
	if !strings.Contains(errArgs, "takes no arguments") {
		t.Fatalf("expected argument rejection: %s", errArgs)
	}
	_, errNone := runCLI(t, 1, "remote", "pull", "--path", repo)
	if !strings.Contains(errNone, "no pull command") {
		t.Fatalf("expected missing-declaration error: %s", errNone)
	}

	// Legacy v0.3 URL form: loads fine, pushes with a targeted message.
	cfgPath := filepath.Join(repo, ".ctxoptimize", "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"remote": "s3://bucket/prefix"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errLegacy := runCLI(t, 1, "remote", "push", "--path", repo)
	if !strings.Contains(errLegacy, "legacy remote config") {
		t.Fatalf("expected legacy-config error: %s", errLegacy)
	}
}

// TestInitOnCloneAutoPulls: a fresh clone already carries the committed
// .ctxoptimize/ (config with remote.pull + the transport script) but has no
// local store. `init` must run the declared pull itself — not
// scaffold-and-rebuild from source. The store is populated right after init.
func TestInitOnCloneAutoPulls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("transport script uses sh")
	}
	// Producer: build + publish a single-project store.
	repo := t.TempDir()
	writeFiles(t, repo, map[string]string{"main.go": "package main\n\nfunc Boot() {}\n"})
	host := t.TempDir()
	envLog := filepath.Join(t.TempDir(), "env.log")
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--path", repo)
	runCLI(t, 0, "add", "--path", repo)
	declareScriptRemote(t, repo, host, envLog)
	runCLI(t, 0, "remote", "push", "--path", repo)

	// Consumer clone: the committed .ctxoptimize/ travels (config + script).
	clone := t.TempDir()
	cfgData, err := os.ReadFile(filepath.Join(repo, ".ctxoptimize", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	syncData, err := os.ReadFile(filepath.Join(repo, ".ctxoptimize", "sync.sh"))
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, clone, map[string]string{
		"main.go":                  "package main\n\nfunc Boot() {}\n",
		".ctxoptimize/config.json": string(cfgData),
		".ctxoptimize/sync.sh":     string(syncData),
	})
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir()) // fresh, empty

	out, _ := runCLI(t, 0, "init", "--path", clone)
	if !strings.Contains(out, "already configured") || !strings.Contains(out, "fetching") {
		t.Fatalf("init on a clone must auto-fetch the prebuilt graph, got:\n%s", out)
	}
	if strings.Contains(out, "scaffolded") {
		t.Fatalf("init on a clone must NOT scaffold a fresh store:\n%s", out)
	}

	// init itself populated the store — no separate pull needed.
	st, _ := runCLI(t, 0, "status", "--json", "--path", clone)
	var status struct {
		Nodes int `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(st), &status); err != nil {
		t.Fatalf("status --json: %v\n%s", err, st)
	}
	if status.Nodes == 0 {
		t.Fatalf("init on a clone should have auto-pulled the store:\n%s", st)
	}
}
