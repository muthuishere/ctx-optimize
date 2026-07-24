package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/store"
)

// Lever 3 — lazy autosync on a read verb (ADR 2026-07-24-lazy-autosync).
// Config-gated (default off), CODE-ONLY. A stale read either resyncs inline
// (block) or spawns a detached child `sync` and answers now (lazy). The staleness
// gate is lever 1's tree-signature — it catches UNCOMMITTED edits, which git-HEAD
// freshness would miss. Adapters/native sources are never touched here: a read
// must never run a script or dial (LOCKED scope).

const (
	autosyncEnv      = "CTX_OPTIMIZE_AUTOSYNC"
	autosyncLockFile = ".autosync.lock"
	// A dead-owner lock is reclaimed once it is older than this AND its pid is
	// gone — the grace closes the parent-exit/child-adopt race (the parent that
	// created the lock exits immediately; the child claims it within ms).
	autosyncLockGrace = 30 * time.Second
	// A lock older than this is reclaimed unconditionally — a wedged sync must
	// never block auto-sync forever.
	autosyncLockMax = 10 * time.Minute
)

// spawnAutosyncChild is the seam tests replace so they never fork a real
// process. Production wiring is spawnAutosyncChildReal.
var spawnAutosyncChild = spawnAutosyncChildReal

// autosyncReadVerbs are the read verbs that fire a lazy resync. status/fresh are
// excluded on purpose — they REPORT freshness, and auto-syncing would hide the
// signal they exist to show. No write verb, serve, wiki, config, or log.
var autosyncReadVerbs = map[string]bool{
	"query": true, "ask": true, "card": true, "change-plan": true, "plan": true,
	"affected": true, "path": true, "explain": true, "hubs": true, "verify": true,
	"nodes": true, "edges": true, "deps": true, "routes": true, "manifests": true,
	"export": true,
}

func isAutosyncTrigger(cmd string) bool { return autosyncReadVerbs[cmd] }

// resolveAutosyncMode folds the three sources: env > project config > global
// config > off.
func resolveAutosyncMode(projectMode, globalMode store.AutosyncMode) store.AutosyncMode {
	if v := os.Getenv(autosyncEnv); v != "" {
		return store.ParseAutosync(v)
	}
	if projectMode != "" {
		return projectMode
	}
	if globalMode != "" {
		return globalMode
	}
	return store.AutosyncOff
}

// maybeAutosync runs before a read verb dispatches. It never errors out the read:
// any problem (no config, unresolved scope, sync failure) degrades to "answer
// from what's here", which is exactly today's behavior.
func maybeAutosync(cmd string, args []string, stderr io.Writer) {
	if !isAutosyncTrigger(cmd) {
		return
	}
	f := parseFlags(args)
	sc, err := resolveScope(f)
	if err != nil || sc.cfg == nil {
		return // no committed .ctxoptimize config → never auto-sync
	}
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return
	}
	gcfg, _ := store.LoadGlobalConfig(storeRoot)
	var globalMode store.AutosyncMode
	if gcfg != nil {
		globalMode = gcfg.Autosync
	}
	mode := resolveAutosyncMode(sc.cfg.Autosync, globalMode)
	if mode == store.AutosyncOff {
		return
	}
	if !anyStale(autosyncTasks(sc), storeRoot) {
		return
	}
	switch mode {
	case store.AutosyncBlock:
		// Inline resync (code-only), progress to stderr so stdout/--json stays
		// byte-clean. Best-effort: a failed sync must not break the read.
		runAutosyncInline(f, stderr)
	case store.AutosyncLazy:
		rootStoreDir := filepath.Join(storeRoot, filepath.FromSlash(sc.rootKey))
		if _, statErr := os.Stat(rootStoreDir); statErr != nil {
			return // read never creates a store dir
		}
		lockPath := filepath.Join(rootStoreDir, autosyncLockFile)
		got, _ := acquireAutosyncLock(lockPath)
		if got {
			if err := spawnAutosyncChild(sc.rootDir, lockPath, f.strs["store"]); err != nil {
				os.Remove(lockPath) // spawn failed → don't leave a lock nobody holds
			}
		}
		fmt.Fprintln(stderr, "ctx-optimize: store is stale — code resync started in background; re-run for updated results")
	}
}

// autosyncTask mirrors the (base, excludes, storeKey) tuple a gather was called
// with, so treeSignature recomputes byte-identically to what was recorded.
type autosyncTask struct {
	base     string
	excludes []string
	storeKey string
}

// autosyncTasks reproduces the gather plan the code-only sync would run: the
// module fan-out for a root, or the single (base, storeKey) otherwise — matching
// cmdAdd's own branching so the recorded TreeSig lines up.
func autosyncTasks(sc *scope) []autosyncTask {
	if sc.kind == scopeRoot && len(sc.modules) > 0 {
		gts, err := planTasks(sc.rootDir, sc.rootKey, sc.modules, map[string]bool{})
		if err != nil {
			return nil
		}
		out := make([]autosyncTask, 0, len(gts))
		for _, t := range gts {
			out = append(out, autosyncTask{base: t.base, excludes: t.excludes, storeKey: t.storeKey})
		}
		return out
	}
	base := sc.rootDir
	if sc.kind == scopeModule && !(sc.mod != nil && sc.mod.Multi()) {
		base = filepath.Join(sc.rootDir, filepath.FromSlash(sc.modulePath))
	}
	return []autosyncTask{{base: base, storeKey: sc.storeKey}}
}

// anyStale reports whether any task's working tree diverges from the TreeSig its
// store recorded — the same stat-only fingerprint lever 1 uses. Missing
// provenance ⇒ stale (a first-ever/pre-lever-1 store; the sync will stamp it).
// A missing store DIR is skipped (the read handles emptiness; never create one).
func anyStale(tasks []autosyncTask, storeRoot string) bool {
	for _, t := range tasks {
		dir := filepath.Join(storeRoot, filepath.FromSlash(t.storeKey))
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		s, err := store.Open(storeRoot, t.storeKey)
		if err != nil {
			continue
		}
		absBase, _ := filepath.Abs(t.base)
		rec, ok := recordedSource(s, absBase)
		if !ok || rec.TreeSig == "" {
			return true
		}
		sig, err := treeSignature(t.base, t.excludes)
		if err != nil {
			continue
		}
		if sig != rec.TreeSig {
			return true
		}
	}
	return false
}

// runAutosyncInline runs the code-only resync (block mode) — `sync`'s default
// path — routed to stderr, forwarding only the scope-selecting flags.
func runAutosyncInline(f *flags, stderr io.Writer) {
	args := []string{"--no-adapters"}
	if p := f.strs["path"]; p != "" {
		args = append(args, "--path", p)
	} else {
		args = append(args, ".")
	}
	if st := f.strs["store"]; st != "" {
		args = append(args, "--store", st)
	}
	_ = cmdAdd(args, stderr, nil)
}

// --- lockfile: one sync in flight, PID-guarded ---

// acquireAutosyncLock atomically claims the lock (O_EXCL). Already held →
// reclaim it only when the owner is provably gone, else report "in flight"
// (false) so the caller no-ops the spawn.
func acquireAutosyncLock(path string) (bool, error) {
	if writeLock(path) == nil {
		return true, nil
	}
	if !lockReclaimable(path) {
		return false, nil
	}
	os.Remove(path)
	return writeLock(path) == nil, nil
}

func writeLock(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	fmt.Fprintf(f, "%d\n%d\n", os.Getpid(), time.Now().UnixNano())
	return f.Close()
}

// lockReclaimable: reclaim when the lock is older than the hard cap, or its pid
// is dead and it is past the startup grace. A malformed/unreadable lock is
// reclaimable (better a rare double-sync than a permanently wedged one).
func lockReclaimable(path string) bool {
	pid, startNanos, ok := readLock(path)
	if !ok {
		return true
	}
	age := time.Since(time.Unix(0, startNanos))
	if age > autosyncLockMax {
		return true
	}
	return !processAlive(pid) && age > autosyncLockGrace
}

func readLock(path string) (pid int, startNanos int64, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, false
	}
	lines := strings.Fields(string(data))
	if len(lines) < 2 {
		return 0, 0, false
	}
	pid, err1 := strconv.Atoi(lines[0])
	startNanos, err2 := strconv.ParseInt(lines[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return pid, startNanos, true
}

// --- detached child ---

// spawnAutosyncChildReal launches `ctx-optimize __autosync` fully detached
// (per-GOOS SysProcAttr), stdio to null, and returns without waiting. The child
// owns the lock's release.
func spawnAutosyncChildReal(repoDir, lockPath, storeFlag string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	a := []string{"__autosync", "--dir", repoDir, "--lock", lockPath}
	if storeFlag != "" {
		a = append(a, "--store", storeFlag)
	}
	cmd := exec.Command(exe, a...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = detachedSysProcAttr()
	return cmd.Start()
}

// cmdAutosyncChild is the hidden `__autosync` verb the detached child runs. It
// re-claims the lock with its OWN live pid (so stale-reclaim sees a live owner),
// runs the code-only resync, and always releases the lock on exit.
func cmdAutosyncChild(args []string) error {
	f := parseFlags(args)
	if lock := f.strs["lock"]; lock != "" {
		os.WriteFile(lock, []byte(fmt.Sprintf("%d\n%d\n", os.Getpid(), time.Now().UnixNano())), 0o644)
		defer os.Remove(lock)
	}
	dir := f.strs["dir"]
	if dir == "" {
		dir = "."
	}
	a := []string{"--no-adapters", "--path", dir}
	if st := f.strs["store"]; st != "" {
		a = append(a, "--store", st)
	}
	return cmdAdd(a, io.Discard, nil)
}
