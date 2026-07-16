// Package bench is step 0 of the 2026-07-16 unified plan: the executable
// performance gate. It measures cold gather p50/p95, 5-file incremental
// refresh, query/card latency, peak RSS, and store size on fixed corpora,
// writes a machine-stamped record, and — when a committed baseline from the
// SAME machine exists — enforces the regression thresholds from the ADR
// (gather ≤5%, query p95 ≤10%, RSS ≤10%). Cross-machine records are
// informational, never gates.
//
// Env-gated runtime skip (house rule):
//
//	CTX_OPTIMIZE_BENCH=1 go test ./internal/bench/ -run TestBenchExtract -v
//
// `task bench-extract` wraps it. UPDATE_BASELINE=1 rewrites
// proof/bench/baseline-<slug>.json (reviewed like code). Corpora: this repo
// always; linux-block + Newtonsoft when CTX_OPTIMIZE_GOLDEN_CORPORA is set.
package bench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/app"
)

const coldRuns = 10
const refreshRounds = 5
const queryRuns = 50

type stats struct {
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
}

type record struct {
	Corpus     string  `json:"corpus"`
	Files      int     `json:"files"`
	Bytes      int64   `json:"bytes"`
	Machine    string  `json:"machine"`
	GoVersion  string  `json:"go_version"`
	Commit     string  `json:"commit"`
	When       string  `json:"when"`
	ColdGather stats   `json:"cold_gather"`
	Refresh5   stats   `json:"refresh_5_files"`
	Query      stats   `json:"query"`
	Card       stats   `json:"card"`
	PeakRSSMB  float64 `json:"peak_rss_mb"`
	StoreBytes int64   `json:"store_bytes"`
	// Agent-session cost: what a harness actually pays per call and per
	// 100-call session — output tokens it must ingest (bytes/4) and, when the
	// built binary exists, the real subprocess spawn+answer wall (agents pay
	// process startup on EVERY call; in-process numbers hide it).
	QueryOutTokens   int     `json:"query_out_tokens_per_call"`
	CardOutTokens    int     `json:"card_out_tokens_per_call"`
	SpawnQueryMS     float64 `json:"subprocess_query_ms_p50"`
	Session100MS     float64 `json:"session_100_queries_ms"`
	Session100Tokens int     `json:"session_100_queries_tokens"`
}

func TestBenchExtract(t *testing.T) {
	if os.Getenv("CTX_OPTIMIZE_BENCH") != "1" {
		t.Skip("CTX_OPTIMIZE_BENCH not set — run via `task bench-extract`")
	}
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	corpora := map[string]struct {
		src   string
		query string
		card  string
	}{
		"ctx-optimize": {repoRoot, "remote sync push pull", "runSync"},
	}
	if base := os.Getenv("CTX_OPTIMIZE_GOLDEN_CORPORA"); base != "" {
		if _, err := os.Stat(filepath.Join(base, "linux", "block")); err == nil {
			corpora["linux-block"] = struct{ src, query, card string }{
				filepath.Join(base, "linux", "block"), "split bio segments", "bio_split"}
		}
		if _, err := os.Stat(filepath.Join(base, "Newtonsoft.Json")); err == nil {
			corpora["newtonsoft"] = struct{ src, query, card string }{
				filepath.Join(base, "Newtonsoft.Json"), "deserialize json string object", "Newtonsoft.JsonConvert"}
		}
	}
	names := make([]string, 0, len(corpora))
	for n := range corpora {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		c := corpora[name]
		t.Run(name, func(t *testing.T) { benchCorpus(t, repoRoot, name, c.src, c.query, c.card) })
	}
}

func benchCorpus(t *testing.T, repoRoot, name, src, query, card string) {
	// Work on a temp copy: refresh mutates files, and gather writes state.
	work := filepath.Join(t.TempDir(), "repo")
	files, bytesTotal := copyTreeCounting(t, src, work)

	// Cold gathers: fresh store each run.
	var cold []float64
	var storeBytes int64
	lastStore := ""
	for i := 0; i < coldRuns; i++ {
		storeRoot := filepath.Join(t.TempDir(), fmt.Sprintf("s%d", i))
		start := time.Now()
		cli(t, "add", work, "--path", work, "--store", storeRoot)
		cold = append(cold, ms(time.Since(start)))
		lastStore = storeRoot
	}
	storeBytes = dirSize(lastStore)

	// 5-file incremental refresh against the last store.
	victims := pickSourceFiles(t, work, 5)
	var refresh []float64
	for i := 0; i < refreshRounds; i++ {
		for _, v := range victims {
			f, err := os.OpenFile(v, os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(f, "\n// bench touch %d\n", i)
			f.Close()
		}
		start := time.Now()
		cli(t, "add", work, "--path", work, "--store", lastStore)
		refresh = append(refresh, ms(time.Since(start)))
	}

	// Query/card latency + output cost on the warm store.
	var qs, cs []float64
	var qOut, cOut string
	for i := 0; i < queryRuns; i++ {
		start := time.Now()
		qOut = cli(t, "query", query, "--path", work, "--store", lastStore)
		qs = append(qs, ms(time.Since(start)))
		start = time.Now()
		cOut = cli(t, "card", card, "--path", work, "--store", lastStore)
		cs = append(cs, ms(time.Since(start)))
	}
	// Real subprocess cost (spawn + answer): what an agent pays per call.
	spawnP50 := subprocessQueryP50(t, repoRoot, query, work, lastStore)

	rec := record{
		Corpus: name, Files: files, Bytes: bytesTotal,
		Machine:   runtime.GOOS + "/" + runtime.GOARCH + "/" + hostname(),
		GoVersion: runtime.Version(), Commit: gitHead(repoRoot),
		When:       time.Now().UTC().Format(time.RFC3339),
		ColdGather: percentiles(cold), Refresh5: percentiles(refresh),
		Query: percentiles(qs), Card: percentiles(cs),
		PeakRSSMB: peakRSSMB(), StoreBytes: storeBytes,
		QueryOutTokens: len(qOut) / 4, CardOutTokens: len(cOut) / 4,
		SpawnQueryMS: spawnP50,
		Session100MS: 100 * (spawnP50), Session100Tokens: 100 * len(qOut) / 4,
	}
	out, _ := json.MarshalIndent(rec, "", "  ")
	t.Logf("bench %s:\n%s", name, out)

	baseline := filepath.Join(repoRoot, "proof", "bench", "baseline-"+name+".json")
	if os.Getenv("UPDATE_BASELINE") == "1" {
		os.MkdirAll(filepath.Dir(baseline), 0o755)
		if err := os.WriteFile(baseline, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("baseline updated: %s", baseline)
		return
	}
	prev, err := os.ReadFile(baseline)
	if err != nil {
		t.Logf("no baseline for %s — run UPDATE_BASELINE=1 to create it", name)
		return
	}
	var base record
	if err := json.Unmarshal(prev, &base); err != nil {
		t.Fatal(err)
	}
	if base.Machine != rec.Machine {
		t.Logf("baseline machine %q ≠ %q — informational only, no gates", base.Machine, rec.Machine)
		return
	}
	// Regression gates (ADR thresholds), same machine only.
	// Noise floor: single-digit-ms baselines make pure ratios flap (a 10%%
	// tolerance on 8ms is under scheduler jitter) — allow ratio OR +5ms/+64MB,
	// whichever is larger.
	gate := func(what string, baseV, gotV, maxRatio, slack float64) {
		if baseV <= 0 {
			return
		}
		allowed := baseV * maxRatio
		if baseV+slack > allowed {
			allowed = baseV + slack
		}
		if gotV > allowed {
			t.Errorf("%s regression: %.1f -> %.1f (allowed %.1f)", what, baseV, gotV, allowed)
		}
	}
	gate("cold gather p95 ms", base.ColdGather.P95MS, rec.ColdGather.P95MS, 1.05, 50)
	gate("refresh p95 ms", base.Refresh5.P95MS, rec.Refresh5.P95MS, 1.05, 50)
	gate("query p95 ms", base.Query.P95MS, rec.Query.P95MS, 1.10, 5)
	gate("peak RSS MB", base.PeakRSSMB, rec.PeakRSSMB, 1.10, 64)
	gate("query out tokens", float64(base.QueryOutTokens), float64(rec.QueryOutTokens), 1.20, 100)
	gate("subprocess query ms", base.SpawnQueryMS, rec.SpawnQueryMS, 1.15, 10)
}

// ---- helpers ----

func cli(t *testing.T, args ...string) string {
	t.Helper()
	var out, errb bytes.Buffer
	if code := app.Run(args, &out, &errb); code != 0 {
		t.Fatalf("%v: exit %d: %s%s", args, code, out.String(), errb.String())
	}
	return out.String()
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func percentiles(v []float64) stats {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	at := func(p float64) float64 {
		if len(s) == 0 {
			return 0
		}
		i := int(p * float64(len(s)-1))
		return s[i]
	}
	return stats{P50MS: at(0.5), P95MS: at(0.95)}
}

func copyTreeCounting(t *testing.T, src, dst string) (int, int64) {
	t.Helper()
	files := 0
	var total int64
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil // unreadable oddities don't break the bench
		}
		files++
		total += int64(len(data))
		return os.WriteFile(out, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return files, total
}

func pickSourceFiles(t *testing.T, root string, n int) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || len(out) >= n {
			return nil
		}
		switch filepath.Ext(p) {
		case ".go", ".c", ".cs", ".js", ".ts":
			out = append(out, p)
		}
		return nil
	})
	if len(out) == 0 {
		t.Fatal("no source files to touch")
	}
	return out
}

func dirSize(root string) int64 {
	var total int64
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

func peakRSSMB() float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	rss := float64(ru.Maxrss)
	if runtime.GOOS == "darwin" {
		return rss / (1024 * 1024) // bytes on darwin
	}
	return rss / 1024 // KB on linux
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func gitHead(repo string) string {
	data, err := os.ReadFile(filepath.Join(repo, ".git", "HEAD"))
	if err != nil {
		return "unknown"
	}
	s := string(bytes.TrimSpace(data))
	if len(s) > 5 && s[:5] == "ref: " {
		ref, err := os.ReadFile(filepath.Join(repo, ".git", filepath.FromSlash(s[5:])))
		if err != nil {
			return "unknown"
		}
		s = string(bytes.TrimSpace(ref))
	}
	if len(s) > 12 {
		s = s[:12]
	}
	return s
}

// subprocessQueryP50 measures the REAL per-call price an agent pays: exec the
// built binary (process spawn + store open + answer), p50 of 10 calls. Skips
// (returns 0) when bin/ctx-optimize hasn't been built.
func subprocessQueryP50(t *testing.T, repoRoot, query, work, storeRoot string) float64 {
	t.Helper()
	bin := filepath.Join(repoRoot, "bin", "ctx-optimize")
	if _, err := os.Stat(bin); err != nil {
		t.Logf("bin/ctx-optimize not built — subprocess cost skipped (run task build)")
		return 0
	}
	var samples []float64
	for i := 0; i < 10; i++ {
		start := time.Now()
		cmd := exec.Command(bin, "query", query, "--path", work, "--store", storeRoot)
		if err := cmd.Run(); err != nil {
			t.Logf("subprocess query failed: %v", err)
			return 0
		}
		samples = append(samples, ms(time.Since(start)))
	}
	return percentiles(samples).P50MS
}
