package code

import (
	"runtime"
	"sync"
)

// Worker slots are a PROCESS-WIDE budget, not a per-call one.
//
// Each worker owns a wazero instance whose linear memory starts at 64 MB, so
// the worker count IS the memory bill. D0 (GOMAXPROCS instead of NumCPU) fixed
// the per-call number, but `ExtractPaths` runs ONCE PER MODULE and a monorepo
// gathers modules concurrently — so the bill multiplied:
//
//	modules_in_flight x (GOMAXPROCS-1) x 64 MB
//
// Measured on reqsume (7 modules) at GOMAXPROCS=18 that is 9.40 GB, while the
// same machine gathering a single-module repo uses 2.41 GB. A small monorepo
// was the worst case in the whole product, which is backwards.
//
// The fix is the same reasoning symcache.go already applied to symbol tables:
// wasm state is a process resource, so budget it process-wide.
//
// Two properties this deliberately guarantees:
//
//   - EVERY gather gets at least one worker, even when the budget is spent.
//     A pure semaphore would be correct (holders always finish, so there is no
//     circular wait) but it would serialize modules behind whichever one
//     grabbed the pool first. The floor costs at most one instance per
//     in-flight module and keeps the fan-out actually fanned out.
//   - GOMAXPROCS is read FRESH on every reservation rather than captured once,
//     so a process that changes it — tests do — is not stuck with a stale
//     budget.
//
// Upper bound is therefore (GOMAXPROCS-1) + modules_in_flight workers, versus
// modules_in_flight x (GOMAXPROCS-1) before.
var (
	slotMu    sync.Mutex
	slotsUsed int
)

// reserveWorkers hands out up to `want` worker slots from the global budget,
// never fewer than one. The caller MUST releaseWorkers(got) when its workers
// have exited.
func reserveWorkers(want int) int {
	if want < 1 {
		want = 1
	}
	slotMu.Lock()
	defer slotMu.Unlock()

	total := runtime.GOMAXPROCS(0) - 1
	if total < 1 {
		total = 1
	}
	got := 1 // the progress floor: never zero, whatever the budget says
	if free := total - slotsUsed; free > 1 {
		extra := free - 1
		if extra > want-1 {
			extra = want - 1
		}
		got += extra
	}
	slotsUsed += got
	return got
}

func releaseWorkers(n int) {
	slotMu.Lock()
	slotsUsed -= n
	if slotsUsed < 0 { // defensive: a double release is a bug, not a reason to wedge
		slotsUsed = 0
	}
	slotMu.Unlock()
}
