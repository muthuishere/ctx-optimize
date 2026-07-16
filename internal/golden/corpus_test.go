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
	// every machine. Set ~10x over measured local wall: slow CI runners pass,
	// an order-of-magnitude regression (accidental O(n²), a double tree walk)
	// fails. Fine-grained p50/p95 baseline diffs are the bench harness's job.
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

	storeRoot := t.TempDir()
	start := time.Now()
	runCLI(t, "init", "--path", gatherRoot, "--store", storeRoot)
	runCLI(t, "add", gatherRoot, "--path", gatherRoot, "--store", storeRoot)
	gatherWall := time.Since(start)

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
		name := strings.TrimSuffix(filepath.Base(specPath), ".json")
		if err := appendHistory(historyLine{Kind: "corpus", Corpus: name, Nodes: nodes, Edges: edges, GatherMS: gatherWall.Milliseconds()}); err != nil {
			t.Errorf("audit record failed: %v", err)
		}
	}

	// Performance is part of the golden contract — a gather that got an order
	// of magnitude slower is as broken as one that lost nodes.
	if spec.MaxGatherSeconds > 0 && gatherWall.Seconds() > spec.MaxGatherSeconds {
		t.Errorf("gather took %.1fs, performance ceiling %.0fs — performance regression", gatherWall.Seconds(), spec.MaxGatherSeconds)
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
