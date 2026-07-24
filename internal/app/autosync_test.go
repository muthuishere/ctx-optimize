package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/store"
)

// Precedence: env > project > global > off (ADR 2026-07-24-lazy-autosync).
func TestResolveAutosyncMode(t *testing.T) {
	t.Setenv(autosyncEnv, "") // "" == unset for our reader
	if m := resolveAutosyncMode("", ""); m != store.AutosyncOff {
		t.Fatalf("all empty → %q, want off", m)
	}
	if m := resolveAutosyncMode("", store.AutosyncBlock); m != store.AutosyncBlock {
		t.Fatalf("global only → %q, want block", m)
	}
	if m := resolveAutosyncMode(store.AutosyncLazy, store.AutosyncBlock); m != store.AutosyncLazy {
		t.Fatalf("project overrides global → %q, want lazy", m)
	}
	t.Setenv(autosyncEnv, "block")
	if m := resolveAutosyncMode(store.AutosyncLazy, store.AutosyncOff); m != store.AutosyncBlock {
		t.Fatalf("env overrides all → %q, want block", m)
	}
}

// The staleness gate is the tree-signature: an UNCOMMITTED edit (which git-HEAD
// freshness would miss) must read as stale.
func TestAnyStaleTreeSig(t *testing.T) {
	repo, storeRoot, key := setupIncremental(t, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": baseMain,
	})
	tasks := []autosyncTask{{base: repo, storeKey: key}}
	if anyStale(tasks, storeRoot) {
		t.Fatal("freshly-gathered store must not be stale")
	}
	mustWrite(t, repo, "main.go", baseMain+"func Beta() {}\n")
	if !anyStale(tasks, storeRoot) {
		t.Fatal("uncommitted edit must read as stale")
	}
}

// A missing store dir must never be created by the staleness check (reads don't
// write a store).
func TestAnyStaleNeverCreatesStore(t *testing.T) {
	storeRoot := t.TempDir()
	tasks := []autosyncTask{{base: t.TempDir(), storeKey: "ghost"}}
	if anyStale(tasks, storeRoot) {
		t.Fatal("no store dir → not stale (skip), not a trigger")
	}
	if _, err := os.Stat(filepath.Join(storeRoot, "ghost")); !os.IsNotExist(err) {
		t.Fatal("staleness check created a store dir")
	}
}

// block: a stale read runs the resync inline BEFORE the query answers.
func TestAutosyncBlockResyncsBeforeRead(t *testing.T) {
	repo, storeRoot, key := setupIncremental(t, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": baseMain,
	})
	mustWrite(t, repo, "main.go", baseMain+"func Beta() {}\n")
	if strings.Contains(graphOf(t, storeRoot, key), "::Beta") {
		t.Fatal("precondition: Beta not yet in the store")
	}
	t.Setenv(autosyncEnv, "block")
	runCLI(t, 0, "query", "Beta", "--path", repo, "--store", storeRoot)
	if !strings.Contains(graphOf(t, storeRoot, key), "::Beta") {
		t.Fatal("block mode must resync the edit into the store before answering")
	}
}

// lazy: a stale read spawns ONE detached child (guarded by the lockfile) and
// answers now; a second stale read while the first is "in flight" does not spawn
// again.
func TestAutosyncLazySpawnsOnceAndLocks(t *testing.T) {
	repo, storeRoot, key := setupIncremental(t, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": baseMain,
	})
	mustWrite(t, repo, "main.go", baseMain+"func Beta() {}\n")

	var spawns int
	orig := spawnAutosyncChild
	// Stub the fork: record the call, leave the parent-written lock in place (a
	// real child would hold then release it — here it simulates "in flight").
	spawnAutosyncChild = func(repoDir, lockPath, storeFlag string) error {
		spawns++
		return nil
	}
	defer func() { spawnAutosyncChild = orig }()

	t.Setenv(autosyncEnv, "lazy")
	_, errOut := runCLI(t, 0, "query", "Beta", "--path", repo, "--store", storeRoot)
	if spawns != 1 {
		t.Fatalf("first stale lazy read must spawn once, got %d", spawns)
	}
	if !strings.Contains(errOut, "background") {
		t.Fatalf("lazy read must print a staleness note to stderr: %q", errOut)
	}
	lock := filepath.Join(storeRoot, key, autosyncLockFile)
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("lock must exist after a spawn: %v", err)
	}
	// Second read while the lock is held (our live pid) → no second spawn.
	runCLI(t, 0, "query", "Beta", "--path", repo, "--store", storeRoot)
	if spawns != 1 {
		t.Fatalf("a sync already in flight must not spawn again, got %d", spawns)
	}
}

// off (default): a stale read is byte-identical to today — no resync, no spawn.
func TestAutosyncOffIsNoop(t *testing.T) {
	repo, storeRoot, key := setupIncremental(t, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": baseMain,
	})
	mustWrite(t, repo, "main.go", baseMain+"func Beta() {}\n")
	before := graphOf(t, storeRoot, key)

	spawned := false
	orig := spawnAutosyncChild
	spawnAutosyncChild = func(_, _, _ string) error { spawned = true; return nil }
	defer func() { spawnAutosyncChild = orig }()

	t.Setenv(autosyncEnv, "") // default off
	runCLI(t, 0, "query", "Beta", "--path", repo, "--store", storeRoot)
	if spawned {
		t.Fatal("off mode must never spawn a resync")
	}
	if graphOf(t, storeRoot, key) != before {
		t.Fatal("off mode must not touch the store")
	}
}

func TestLockReclaim(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, autosyncLockFile)

	// Dead pid, old timestamp → reclaimable.
	os.WriteFile(path, []byte(fmt.Sprintf("%d\n%d\n", 2147483646, time.Now().Add(-time.Hour).UnixNano())), 0o644)
	if !lockReclaimable(path) {
		t.Fatal("dead+old lock must be reclaimable")
	}

	// Our own live pid, fresh → not reclaimable.
	os.Remove(path)
	if err := writeLock(path); err != nil {
		t.Fatal(err)
	}
	if lockReclaimable(path) {
		t.Fatal("a live, fresh lock must not be reclaimable")
	}

	// acquireAutosyncLock on a held (live) lock → false, no takeover.
	got, _ := acquireAutosyncLock(path)
	if got {
		t.Fatal("must not acquire a lock a live owner holds")
	}
}

// sync --adapters / --all must not error on a repo with no adapters/sources, and
// stays the code path otherwise (regression guard for the flag plumbing).
func TestSyncFlagsNoAdaptersNoSources(t *testing.T) {
	repo, storeRoot, _ := setupIncremental(t, map[string]string{
		"go.mod": "module ex\n\ngo 1.22\n", "main.go": baseMain,
	})
	// Must run from the repo dir (sync takes no path); use --path via a fresh add
	// is not possible, so chdir for this verb.
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	runCLI(t, 0, "sync")
	runCLI(t, 0, "sync", "--adapters")
	runCLI(t, 0, "sync", "--all")
}
