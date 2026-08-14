package golden

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Determinism is a golden fact, and its absence went unnoticed for months.
//
// CLAUDE.md's doctrine is "plain files only; store artifacts must stay
// git-diffable (sorted output, atomic rename)" — which is a promise that
// re-gathering unchanged source produces an unchanged store. Nothing asserted
// it. ADR 2026-08-14-stable-node-identity documents the consequence found in
// the regression audit: two identical gathers of linux/block differ in the
// LOCATION of 354 nodes (229 in Newtonsoft), because same-name declarations
// collapse to one node id and `sort.Slice` is unstable when comparing ids
// alone, so which copy survives varies per run.
//
// That bug is real, pre-existing, and NOT fixed here. This file pins the
// property at the strongest level each tier can honestly hold today:
//
//   - hermetic fixtures  -> byte-identical (verified passing; they contain no
//     colliding declarations, so there is nothing to be unstable about)
//   - corpus tier        -> identity-SET equality (same node ids, same edge
//     tuples), which is what survives ADR 5
//
// When ADR 5 lands, the corpus tier should be promoted to byte-identity and
// this comment deleted.

// storeDigest renders a store root as a digest per module, hashing the raw
// graph files — the strictest possible statement of "nothing moved".
//
// Stores are labelled by their INDEX in sorted key order, never by key name:
// a single-module fixture keys its store off the repo directory name, and the
// repo lives in a t.TempDir() whose name changes every run ("001" vs "003").
// Including the key would report that as non-determinism, which it is not.
// Store KEYS are already pinned by the snapshot goldens; what this function
// exists to pin is the graph CONTENT.
func storeDigest(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	for i, key := range storeKeys(t, root) {
		for _, f := range []string{"nodes.ndjson", "edges.ndjson"} {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(key), "graph", f))
			if err != nil {
				t.Fatalf("read %s/%s: %v", key, f, err)
			}
			fmt.Fprintf(&b, "store[%d]/%s %x\n", i, f, sha256.Sum256(data))
		}
	}
	return b.String()
}

// TestGoldenGatherIsDeterministic gathers every hermetic fixture twice into
// separate stores and requires the graph bytes to match exactly. A failure
// here means a map iteration, an unstable sort, or worker-order leakage
// reached the output — which makes every citation the store emits unreliable
// and every re-gather a spurious diff.
func TestGoldenGatherIsDeterministic(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("testdata", "repos", "*"))
	if err != nil || len(fixtures) == 0 {
		t.Fatal("no fixtures under testdata/repos/")
	}
	for _, fx := range fixtures {
		info, err := os.Stat(fx)
		if err != nil || !info.IsDir() {
			continue
		}
		name := filepath.Base(fx)
		t.Run(name, func(t *testing.T) {
			gather := func() string {
				repo := t.TempDir()
				copyTree(t, fx, repo)
				storeRoot := t.TempDir()
				runCLI(t, "init", "--path", repo, "--store", storeRoot)
				runCLI(t, "add", repo, "--path", repo, "--store", storeRoot)
				return storeDigest(t, storeRoot)
			}
			first, second := gather(), gather()
			if first != second {
				t.Errorf("two identical gathers produced different graph bytes — "+
					"a citation from this store can move between runs.\nfirst:\n%s\nsecond:\n%s",
					first, second)
			}
		})
	}
}

// TestGoldenShortCircuitStaysCheap pins the 0-change re-gather across every
// fixture: it must short-circuit AND stay fast. This is the invariant that
// protects incremental work from a new producer being wired in BEFORE the
// early return at internal/app/multimodule.go — the shape of mistake that
// would turn "nothing changed" back into a full re-extract.
//
// Measured on the reference machine (n=4): 17-19ms (dotnetsln) to 77-87ms
// (multimod). The 1.5s ceiling is ~17x the worst of those — deliberately
// generous in absolute terms, because the property under test is "did the
// short-circuit survive", which is a 10x+ signal, not a 1.3x one. The tight
// perf gating lives in perf.go against a same-machine baseline.
func TestGoldenShortCircuitStaysCheap(t *testing.T) {
	fixtures, _ := filepath.Glob(filepath.Join("testdata", "repos", "*"))
	sort.Strings(fixtures)
	for _, fx := range fixtures {
		info, err := os.Stat(fx)
		if err != nil || !info.IsDir() {
			continue
		}
		t.Run(filepath.Base(fx), func(t *testing.T) {
			repo := t.TempDir()
			copyTree(t, fx, repo)
			storeRoot := t.TempDir()
			runCLI(t, "init", "--path", repo, "--store", storeRoot)
			runCLI(t, "add", repo, "--path", repo, "--store", storeRoot)
			resyncWithin(t, 1500*time.Millisecond, repo, storeRoot)
		})
	}
}
