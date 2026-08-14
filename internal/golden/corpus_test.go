package golden

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/store"
)

// Corpus golden tests: gather PINNED real repositories and assert landmark
// facts — the never-break net at real-world scale. Env-gated per house rules
// (runtime skip, never a build tag):
//
//	CTX_OPTIMIZE_GOLDEN_CORPORA=<dir> go test ./internal/golden/ -run Corpus
//
// <dir> holds shallow clones the workflow creates with
// `git clone --depth 1 --branch <ref> <repo>` — one dir per spec in
// testdata/corpora/*.json. A clone at the wrong ref SKIPS (the pin moved, the
// numbers no longer apply); a missing clone SKIPS; a landmark miss FAILS.

type nodeSpec struct {
	Suffix string `json:"suffix"`
	Kind   string `json:"kind"`
}

type corpusSpec struct {
	Repo          string     `json:"repo"`
	Ref           string     `json:"ref"`
	GatherSubdir  string     `json:"gather_subdir"`
	Config        any        `json:"config"`
	MinNodes      int        `json:"min_nodes"`
	MinEdges      int        `json:"min_edges"`
	MustNodes     []nodeSpec `json:"must_nodes"`
	MustCallsInto []struct {
		TargetSuffix string `json:"target_suffix"`
		Min          int    `json:"min"`
	} `json:"must_calls_into"`
	CrossSplitCalls *struct {
		FromPrefix string `json:"from_prefix"`
		ToPrefix   string `json:"to_prefix"`
		Min        int    `json:"min"`
	} `json:"cross_split_calls"`
	// Performance gates — the coarse "never goes away" ceilings that run on
	// every machine, including CI boxes nobody calibrated. Because they must
	// assume the slowest plausible runner, they can only catch
	// order-of-magnitude breakage; the SAME-MACHINE baseline in perf.go is
	// what catches the ~1.5x class. Fine-grained p50/p95 diffs stay the bench
	// harness's job. See perf.go for the two-gate reasoning.
	MaxGatherSeconds float64 `json:"max_gather_seconds"`
	ProbeQuery       *struct {
		Text  string `json:"text"`
		MaxMS int64  `json:"max_ms"`
	} `json:"probe_query"`
}

func TestCorpusGolden(t *testing.T) {
	base := os.Getenv("CTX_OPTIMIZE_GOLDEN_CORPORA")
	if base == "" {
		t.Skip("CTX_OPTIMIZE_GOLDEN_CORPORA not set — corpus goldens run in the golden workflow (or locally against shallow clones)")
	}
	specs, err := filepath.Glob(filepath.Join("testdata", "corpora", "*.json"))
	if err != nil || len(specs) == 0 {
		t.Fatal("no corpus specs in testdata/corpora/")
	}
	for _, sp := range specs {
		sp := sp
		t.Run(strings.TrimSuffix(filepath.Base(sp), ".json"), func(t *testing.T) {
			runCorpus(t, base, sp)
		})
	}
}

func runCorpus(t *testing.T, base, specPath string) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var spec corpusSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("%s: %v", specPath, err)
	}

	repoDir := filepath.Join(base, spec.Repo)
	if _, err := os.Stat(repoDir); err != nil {
		t.Skipf("corpus %s not cloned at %s — clone with: git clone --depth 1 --branch %s <url> %s",
			spec.Repo, repoDir, spec.Ref, repoDir)
	}
	// Pin check: golden numbers only hold at the pinned ref.
	out, err := exec.Command("git", "-C", repoDir, "describe", "--tags", "--always").Output()
	if err == nil {
		got := strings.TrimSpace(string(out))
		if got != spec.Ref && !strings.HasPrefix(got, spec.Ref) {
			t.Skipf("corpus %s is at %q, spec pins %q — re-clone at the pin", spec.Repo, got, spec.Ref)
		}
	}

	// Gather root: whole repo or a pinned subtree (linux → block/).
	gatherRoot := repoDir
	if spec.GatherSubdir != "" {
		gatherRoot = filepath.Join(repoDir, spec.GatherSubdir)
	}
	// A spec-supplied config makes the clone a multi-module config repo —
	// written fresh each run so the clone stays pristine apart from it.
	if spec.Config != nil {
		cfgDir := filepath.Join(gatherRoot, ".ctxoptimize")
		if err := os.MkdirAll(cfgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		cfg, _ := json.MarshalIndent(spec.Config, "", "  ")
		if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), cfg, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	corpusName := strings.TrimSuffix(filepath.Base(specPath), ".json")

	// timedGather runs a full gather into a fresh store and returns the wall.
	// Separated so the perf gate can measure a SECOND time before failing.
	timedGather := func() (string, time.Duration) {
		storeRoot := t.TempDir()
		start := time.Now()
		runCLI(t, "init", "--path", gatherRoot, "--store", storeRoot)
		runCLI(t, "add", gatherRoot, "--path", gatherRoot, "--store", storeRoot)
		return storeRoot, time.Since(start)
	}
	storeRoot, gatherWall := timedGather()

	// Fold every module store into one fact set for landmark checks.
	var nodes int
	var edges int
	nodeIndex := map[string]string{} // id -> kind
	type edgeRow struct{ src, rel, dst string }
	var allEdges []edgeRow
	for _, key := range storeKeys(t, storeRoot) {
		st, err := store.Open(storeRoot, key)
		if err != nil {
			t.Fatal(err)
		}
		ns, err := st.Nodes()
		if err != nil {
			t.Fatal(err)
		}
		es, err := st.Edges()
		if err != nil {
			t.Fatal(err)
		}
		nodes += len(ns)
		edges += len(es)
		for _, n := range ns {
			nodeIndex[n.ID] = n.Kind
		}
		for _, e := range es {
			allEdges = append(allEdges, edgeRow{e.Source, e.Relation, e.Target})
		}
	}
	t.Logf("%s@%s: %d nodes, %d edges, gather %.1fs", spec.Repo, spec.Ref, nodes, edges, gatherWall.Seconds())
	if recordingEnabled() {
		if err := appendHistory(historyLine{Kind: "corpus", Corpus: corpusName, Nodes: nodes, Edges: edges, GatherMS: gatherWall.Milliseconds()}); err != nil {
			t.Errorf("audit record failed: %v", err)
		}
		if err := recordPerfBaseline(corpusName, gatherWall); err != nil {
			t.Errorf("perf baseline record failed: %v", err)
		} else {
			t.Logf("recorded perf baseline %s = %dms (machine %s)", corpusName, gatherWall.Milliseconds(), perfFingerprint())
		}
	}

	// Gate 1 — the portable absolute ceiling. Coarse by necessity (it runs on
	// uncalibrated hardware), so it only catches order-of-magnitude breakage.
	if spec.MaxGatherSeconds > 0 && gatherWall.Seconds() > spec.MaxGatherSeconds {
		t.Errorf("gather took %.2fs, performance ceiling %gs — performance regression", gatherWall.Seconds(), spec.MaxGatherSeconds)
	}
	// Gate 2 — the same-machine baseline, which is what catches the ~1.5x
	// class the ceiling must let through. Silent when this machine has no
	// recorded baseline: wall-clock does not travel between machines.
	if !recordingEnabled() {
		if want, ok := perfBaselineFor(corpusName); ok {
			limit := time.Duration(float64(want) * perfTolerance)
			if gatherWall > limit {
				// Measure again before accusing: a transient stall reproduces
				// rarely, a regression reproduces always. Costs nothing unless
				// we are already about to fail.
				_, retry := timedGather()
				best := gatherWall
				if retry < best {
					best = retry
				}
				if best > limit {
					t.Errorf("gather %s vs baseline %s on this machine (%s) — %.2fx, over the %.2fx tolerance; "+
						"confirmed by a second run at %s. Either a real regression, or re-record with RECORD_GOLDEN=1 and justify the rise.",
						best.Round(time.Millisecond), want.Round(time.Millisecond), perfFingerprint(),
						float64(best)/float64(want), perfTolerance, retry.Round(time.Millisecond))
				} else {
					t.Logf("gather %s exceeded the %.2fx tolerance but a second run came in at %s (baseline %s) — treated as noise",
						gatherWall.Round(time.Millisecond), perfTolerance, retry.Round(time.Millisecond), want.Round(time.Millisecond))
				}
			} else {
				t.Logf("gather %s vs baseline %s (%.2fx of %.2fx tolerance)",
					gatherWall.Round(time.Millisecond), want.Round(time.Millisecond),
					float64(gatherWall)/float64(want), perfTolerance)
			}
		} else {
			t.Logf("no gather baseline for machine %s — the tight perf gate is INACTIVE here; "+
				"establish one with RECORD_GOLDEN=1 (baselines on file: %v)", perfFingerprint(), perfBaselineCorpora())
		}
	}
	if pq := spec.ProbeQuery; pq != nil {
		qs := time.Now()
		runCLI(t, "query", pq.Text, "--path", gatherRoot, "--store", storeRoot)
		qWall := time.Since(qs)
		t.Logf("probe query %q: %dms (ceiling %dms)", pq.Text, qWall.Milliseconds(), pq.MaxMS)
		if qWall.Milliseconds() > pq.MaxMS {
			t.Errorf("probe query took %dms, ceiling %dms — query performance regression", qWall.Milliseconds(), pq.MaxMS)
		}
	}

	if nodes < spec.MinNodes {
		t.Errorf("nodes = %d, golden floor %d — extraction lost ground", nodes, spec.MinNodes)
	}
	if edges < spec.MinEdges {
		t.Errorf("edges = %d, golden floor %d — extraction lost ground", edges, spec.MinEdges)
	}
	for _, m := range spec.MustNodes {
		id, kind, found := findBySuffix(nodeIndex, m.Suffix)
		if !found {
			t.Errorf("landmark node missing: *%s", m.Suffix)
			continue
		}
		if m.Kind != "" && kind != m.Kind {
			t.Errorf("landmark %s: kind = %q, want %q", id, kind, m.Kind)
		}
	}
	for _, m := range spec.MustCallsInto {
		n := 0
		for _, e := range allEdges {
			if e.rel == "calls" && strings.HasSuffix(e.dst, m.TargetSuffix) {
				n++
			}
		}
		if n < m.Min {
			t.Errorf("calls into *%s = %d, golden floor %d", m.TargetSuffix, n, m.Min)
		}
	}
	// Determinism at corpus scale. ADR 2026-08-14-stable-node-identity blocks
	// byte-identity here — same-name declarations collapse to one id and the
	// surviving copy's LOCATION varies per run (354 nodes on linux/block, 229
	// on Newtonsoft). The identity SET is what survives that bug and is still
	// worth pinning: it catches a node or edge appearing/vanishing between
	// runs, which is a different and worse failure than a line number moving.
	// Promote this to storeDigest() byte-equality once ADR 5 lands.
	if !testing.Short() {
		reStore, _ := timedGather()
		reNodes := map[string]bool{}
		reEdges := map[string]bool{}
		for _, key := range storeKeys(t, reStore) {
			st, err := store.Open(reStore, key)
			if err != nil {
				t.Fatal(err)
			}
			ns, err := st.Nodes()
			if err != nil {
				t.Fatal(err)
			}
			es, err := st.Edges()
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range ns {
				reNodes[n.ID] = true
			}
			for _, e := range es {
				reEdges[e.Source+"\x00"+e.Relation+"\x00"+e.Target] = true
			}
		}
		var missing, added int
		for id := range nodeIndex {
			if !reNodes[id] {
				if missing++; missing <= 3 {
					t.Errorf("node id present in run 1, absent in run 2: %s", id)
				}
			}
		}
		for id := range reNodes {
			if _, ok := nodeIndex[id]; !ok {
				if added++; added <= 3 {
					t.Errorf("node id absent in run 1, present in run 2: %s", id)
				}
			}
		}
		if missing > 3 || added > 3 {
			t.Errorf("identity set unstable across two gathers: %d missing, %d added (first 3 of each shown)", missing, added)
		}
		if len(reEdges) != len(allEdges) {
			// Edge tuples are deduped into a set here; compare set sizes only
			// after the same dedup, or a legitimate duplicate would read as drift.
			seen := map[string]bool{}
			for _, e := range allEdges {
				seen[e.src+"\x00"+e.rel+"\x00"+e.dst] = true
			}
			if len(seen) != len(reEdges) {
				t.Errorf("edge identity set unstable across two gathers: %d vs %d distinct tuples", len(seen), len(reEdges))
			}
		}
	}

	if c := spec.CrossSplitCalls; c != nil {
		n := 0
		for _, e := range allEdges {
			if e.rel == "calls" && strings.HasPrefix(e.src, c.FromPrefix) && strings.HasPrefix(e.dst, c.ToPrefix) {
				n++
			}
		}
		if n < c.Min {
			t.Errorf("cross-split calls %s -> %s = %d, golden floor %d (the multi-path contract)",
				c.FromPrefix, c.ToPrefix, n, c.Min)
		}
	}
}

func findBySuffix(index map[string]string, suffix string) (string, string, bool) {
	for id, kind := range index {
		if strings.HasSuffix(id, suffix) {
			return id, kind, true
		}
	}
	return "", "", false
}

// Silence unused-import lint when specs carry no fmt usage path.
var _ = fmt.Sprintf
