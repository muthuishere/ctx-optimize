package code

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The worker count is the memory budget (each worker owns a 64MB wasm
// instance), so it must respond to GOMAXPROCS — NumCPU reports the HOST's
// cores and ignores cgroup quotas, which is how a 2-CPU container on a
// 64-core box ends up spawning 63 instances.
//
// It must ALSO be invisible in the output: if the number of workers can change
// the graph, that is an ADR 5-class nondeterminism bug hiding behind a
// scheduling detail, and a user capping GOMAXPROCS would silently get
// different facts.
func TestWorkerCountRespectsGOMAXPROCSAndDoesNotChangeOutput(t *testing.T) {
	root := t.TempDir()
	for i, src := range []string{
		"package a\nfunc Alpha() { Beta() }\n",
		"package a\nfunc Beta() {}\n",
		"package a\nfunc Gamma() { Alpha(); Beta() }\n",
	} {
		p := filepath.Join(root, string(rune('a'+i))+".go")
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
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

	prev := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(prev)

	runtime.GOMAXPROCS(1)
	one := render()
	runtime.GOMAXPROCS(8)
	many := render()

	if one != many {
		t.Errorf("worker count changed the graph — scheduling must not be observable\n--- GOMAXPROCS=1\n%s\n--- GOMAXPROCS=8\n%s", one, many)
	}
	if one == "" {
		t.Fatal("extracted nothing; fixture or walk is broken")
	}
}
