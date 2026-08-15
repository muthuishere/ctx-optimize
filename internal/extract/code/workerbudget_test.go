package code

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func withGOMAXPROCS(t *testing.T, n int) {
	t.Helper()
	prev := runtime.GOMAXPROCS(n)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })
}

func resetSlots(t *testing.T) {
	t.Helper()
	slotMu.Lock()
	slotsUsed = 0
	slotMu.Unlock()
	t.Cleanup(func() {
		slotMu.Lock()
		slotsUsed = 0
		slotMu.Unlock()
	})
}

// A single gather must still get the whole budget — full throttle on a laptop
// is the behaviour D0 deliberately preserved, and D1 must not quietly take it
// away.
func TestBudgetGivesOneGatherFullThrottle(t *testing.T) {
	withGOMAXPROCS(t, 8)
	resetSlots(t)
	if got := reserveWorkers(7); got != 7 {
		t.Fatalf("single gather got %d workers, want the full 7", got)
	}
}

// The bill must stop multiplying: N concurrent gathers get (GOMAXPROCS-1)+N
// workers between them, not N x (GOMAXPROCS-1).
func TestBudgetDoesNotMultiplyAcrossGathers(t *testing.T) {
	withGOMAXPROCS(t, 8)
	resetSlots(t)
	const gathers = 7
	total := 0
	for i := 0; i < gathers; i++ {
		total += reserveWorkers(7)
	}
	if unbounded := gathers * 7; total >= unbounded {
		t.Fatalf("budget did not bind: %d workers across %d gathers (unbounded would be %d)", total, gathers, unbounded)
	}
	if max := (8 - 1) + gathers; total > max {
		t.Fatalf("budget exceeded its own bound: %d workers, max %d", total, max)
	}
}

// Every gather gets at least one worker even with the budget fully spent —
// otherwise a late module makes no progress at all and the fan-out deadlocks
// in practice even though it cannot deadlock in theory.
func TestBudgetAlwaysLeavesAProgressFloor(t *testing.T) {
	withGOMAXPROCS(t, 2)
	resetSlots(t)
	for i := 0; i < 5; i++ {
		if got := reserveWorkers(4); got < 1 {
			t.Fatalf("gather %d got %d workers — a gather with no workers never finishes", i, got)
		}
	}
}

// Release returns slots, so a long-lived process does not leak its budget away
// gather after gather until everything runs single-threaded.
func TestBudgetIsReturnedOnRelease(t *testing.T) {
	withGOMAXPROCS(t, 8)
	resetSlots(t)
	first := reserveWorkers(7)
	releaseWorkers(first)
	if second := reserveWorkers(7); second != first {
		t.Fatalf("budget leaked: first gather got %d, an identical later gather got %d", first, second)
	}
}

// The property that outranks the memory saving: concurrent gathers must not
// see each other in their OUTPUT. Worker count now depends on what else is
// running, so if scheduling were observable the graph would change under
// concurrency — an ADR 5-class bug wearing a different hat.
func TestConcurrentGathersProduceIdenticalOutput(t *testing.T) {
	withGOMAXPROCS(t, 8)
	resetSlots(t)

	root := t.TempDir()
	for i, src := range []string{
		"package a\nfunc Alpha() { Beta() }\n",
		"package a\nfunc Beta() {}\n",
		"package a\nfunc Gamma() { Alpha(); Beta() }\n",
		"package a\nfunc Delta() { Gamma() }\n",
	} {
		if err := os.WriteFile(filepath.Join(root, string(rune('a'+i))+".go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	render := func() string {
		b, err := Extract(root)
		if err != nil {
			t.Fatal(err)
		}
		var s string
		for _, n := range b.Nodes {
			s += n.ID + "|" + n.Kind + "|" + n.Location + "\n"
		}
		for _, e := range b.Edges {
			s += e.Source + "->" + e.Target + "|" + e.Relation + "|" + e.Confidence + "\n"
		}
		return s
	}

	solo := render()
	if solo == "" {
		t.Fatal("extracted nothing; fixture or walk is broken")
	}

	// Eight at once: most will be squeezed to the one-worker floor, which is
	// exactly the code path a monorepo hits and the one most likely to differ.
	const n = 8
	out := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out[i] = render()
		}(i)
	}
	wg.Wait()

	for i, got := range out {
		if got != solo {
			t.Fatalf("concurrent gather %d differs from a solo gather — worker count is observable in the output\n--- solo\n%s\n--- concurrent\n%s", i, solo, got)
		}
	}
}
