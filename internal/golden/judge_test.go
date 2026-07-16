package golden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/app"
)

// The judged Q&A benchmark: ~20 agent-shaped questions per corpus, each
// routed through the SAME verb the skill would route it to (query / card /
// affected / path) and marked deterministically:
//
//	1.0  the expected fact is where the routing table promises it
//	0.5  present but demoted (query: beyond top-k; affected: fewer than asked)
//	0.0  absent
//
// Questions carrying "gap" are DELIBERATE zero-scorers documenting a known
// weakness (ranking defects, missing lanes) — the scoreboard the next
// feature is aimed at. min_score is the ratchet: measured, recorded in the
// CHANGELOG, and enforced — a feature that raises the score should raise the
// floor in the same reviewed diff; nothing may lower it.
//
// Env-gated like the corpus goldens: CTX_OPTIMIZE_GOLDEN_CORPORA=<dir>.

type question struct {
	ID           string   `json:"id"`
	Text         string   `json:"text"`
	Verb         string   `json:"verb"`
	Args         []string `json:"args"`
	ExpectAny    []string `json:"expect_any"`
	K            int      `json:"k"`
	Contains     []string `json:"contains"`
	MinImpacts   int      `json:"min_impacts"`
	ImpactsMatch string   `json:"impacts_match"`
	Gap          string   `json:"gap"`
}

type questionSet struct {
	Corpus    string     `json:"corpus"`
	MinScore  float64    `json:"min_score"`
	Questions []question `json:"questions"`
}

func TestJudgeQuestions(t *testing.T) {
	base := os.Getenv("CTX_OPTIMIZE_GOLDEN_CORPORA")
	if base == "" {
		t.Skip("CTX_OPTIMIZE_GOLDEN_CORPORA not set — the judged benchmark runs in the golden workflow (or locally against pinned clones)")
	}
	sets, _ := filepath.Glob(filepath.Join("testdata", "questions", "*.json"))
	if len(sets) == 0 {
		t.Fatal("no question sets in testdata/questions/")
	}
	for _, qs := range sets {
		qs := qs
		t.Run(strings.TrimSuffix(filepath.Base(qs), ".json"), func(t *testing.T) {
			judgeSet(t, base, qs)
		})
	}
}

func judgeSet(t *testing.T, base, setPath string) {
	data, err := os.ReadFile(setPath)
	if err != nil {
		t.Fatal(err)
	}
	var set questionSet
	if err := json.Unmarshal(data, &set); err != nil {
		t.Fatalf("%s: %v", setPath, err)
	}
	// The corpus spec of the same name provides repo/ref/subdir/config.
	specData, err := os.ReadFile(filepath.Join("testdata", "corpora", set.Corpus+".json"))
	if err != nil {
		t.Fatalf("question set %s has no corpus spec: %v", set.Corpus, err)
	}
	var spec corpusSpec
	if err := json.Unmarshal(specData, &spec); err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(base, spec.Repo)
	if _, err := os.Stat(repoDir); err != nil {
		t.Skipf("corpus %s not cloned at %s", spec.Repo, repoDir)
	}
	gatherRoot := repoDir
	if spec.GatherSubdir != "" {
		gatherRoot = filepath.Join(repoDir, spec.GatherSubdir)
	}
	if spec.Config != nil {
		cfgDir := filepath.Join(gatherRoot, ".ctxoptimize")
		os.MkdirAll(cfgDir, 0o755)
		cfg, _ := json.MarshalIndent(spec.Config, "", "  ")
		os.WriteFile(filepath.Join(cfgDir, "config.json"), cfg, 0o644)
	}
	storeRoot := t.TempDir()
	runCLI(t, "init", "--path", gatherRoot, "--store", storeRoot)
	runCLI(t, "add", gatherRoot, "--path", gatherRoot, "--store", storeRoot)

	total := 0.0
	for _, q := range set.Questions {
		mark := judgeOne(t, q, gatherRoot, storeRoot)
		total += mark
		tag := "    "
		if q.Gap != "" {
			tag = "GAP "
		}
		t.Logf("%s %s %.1f/1.0  %s", tag, q.ID, mark, q.Text)
		if q.Gap != "" && mark >= 1.0 {
			t.Logf("      ^ gap question now PASSES (%s) — ratchet min_score and clear the gap note", q.Gap)
		}
	}
	t.Logf("SCORE %s: %.1f / %d  (floor %.1f)", set.Corpus, total, len(set.Questions), set.MinScore)
	if total < set.MinScore {
		t.Errorf("judged score %.1f fell below the floor %.1f — a previously-answerable question broke", total, set.MinScore)
	}
}

// judgeOne runs a single question through its verb and marks it. Verb output
// failures score 0 rather than aborting the set — an unanswerable question
// is a mark, not a test error.
func judgeOne(t *testing.T, q question, gatherRoot, storeRoot string) float64 {
	t.Helper()
	run := func(args ...string) (string, bool) {
		var out, errb bytes.Buffer
		code := app.Run(append(args, "--path", gatherRoot, "--store", storeRoot), &out, &errb)
		return out.String(), code == 0
	}
	switch q.Verb {
	case "query":
		out, ok := run(append([]string{"query"}, append(q.Args, "--json")...)...)
		if !ok {
			return 0
		}
		ids := parseHitIDs(out)
		k := q.K
		if k <= 0 {
			k = 5
		}
		for i, id := range ids {
			for _, want := range q.ExpectAny {
				if strings.Contains(id, want) {
					if i < k {
						return 1.0
					}
					return 0.5 // present but demoted below the promised window
				}
			}
		}
		return 0
	case "card":
		out, ok := run(append([]string{"card"}, q.Args...)...)
		if !ok {
			return 0
		}
		for _, c := range q.Contains {
			if !strings.Contains(out, c) {
				return 0
			}
		}
		return 1.0
	case "affected":
		out, ok := run(append([]string{"affected"}, append(q.Args, "--json")...)...)
		if !ok {
			return 0
		}
		srcs := parseImpactSources(out)
		if q.ImpactsMatch != "" {
			re := regexp.MustCompile(q.ImpactsMatch)
			n := 0
			for _, s := range srcs {
				if re.MatchString(s) {
					n++
				}
			}
			if n >= q.MinImpacts {
				return 1.0
			}
			return 0
		}
		if len(srcs) >= q.MinImpacts {
			return 1.0
		}
		if len(srcs) > 0 {
			return 0.5
		}
		return 0
	case "path":
		out, ok := run(append([]string{"path"}, q.Args...)...)
		if !ok {
			return 0
		}
		for _, c := range q.Contains {
			if !strings.Contains(out, c) {
				return 0
			}
		}
		return 1.0
	default:
		t.Fatalf("%s: unknown verb %q", q.ID, q.Verb)
		return 0
	}
}

func parseHitIDs(out string) []string {
	var envelope struct {
		Result *struct {
			Hits []struct {
				Node struct {
					ID string `json:"id"`
				} `json:"node"`
			} `json:"hits"`
		} `json:"result"`
		Hits []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
		} `json:"hits"`
	}
	if json.Unmarshal([]byte(out), &envelope) != nil {
		return nil
	}
	var ids []string
	if envelope.Result != nil {
		for _, h := range envelope.Result.Hits {
			ids = append(ids, h.Node.ID)
		}
		return ids
	}
	for _, h := range envelope.Hits {
		ids = append(ids, h.Node.ID)
	}
	return ids
}

func parseImpactSources(out string) []string {
	var envelope struct {
		Result *struct {
			Affected []struct {
				Node struct {
					ID     string `json:"id"`
					Source string `json:"source"`
				} `json:"node"`
			} `json:"affected"`
		} `json:"result"`
		Affected []struct {
			Node struct {
				ID     string `json:"id"`
				Source string `json:"source"`
			} `json:"node"`
		} `json:"affected"`
	}
	if json.Unmarshal([]byte(out), &envelope) != nil {
		return nil
	}
	var srcs []string
	if envelope.Result != nil {
		for _, a := range envelope.Result.Affected {
			srcs = append(srcs, a.Node.ID+" "+a.Node.Source)
		}
		return srcs
	}
	for _, a := range envelope.Affected {
		srcs = append(srcs, a.Node.ID+" "+a.Node.Source)
	}
	return srcs
}

var _ = fmt.Sprintf
