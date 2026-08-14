package golden

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

// Performance as a golden fact — the gate that a +60-120% regression walked
// straight through on 2026-08-14 (ADR 2026-08-14-boundaries-second-walk).
//
// The net pinned node counts, query latency and judged answer quality, and the
// corpus specs did carry `max_gather_seconds`. It failed anyway because the
// ceilings were decoration: linux-block allowed 12s for a gather measured at
// 0.42s (29x headroom), newtonsoft 25s for 1.14s (22x). Nothing short of a
// catastrophe could trip them.
//
// Two gates now, because ONE gate cannot do both jobs:
//
//  1. ABSOLUTE ceilings (in testdata/corpora/*.json). Portable — they run on
//     any machine, including a CI box nobody calibrated. An absolute ceiling
//     must therefore assume the slowest plausible runner, so it can only ever
//     catch order-of-magnitude breakage. Tightened from ~25x to ~6x headroom:
//     still coarse, no longer decoration.
//
//  2. A SAME-MACHINE baseline ratio (this file). A wall-clock number is only
//     comparable to another number from the same hardware, so the baseline is
//     keyed by machine fingerprint and simply does not apply when the
//     fingerprint is absent. Where it does apply it is tight enough to catch
//     the ~1.5x class of regression that gate 1 must let through.
//
// Neither gate replaces the bench harness's p50/p95 work; they exist so a
// regression cannot ship GREEN.

// perfTolerance is how far over its recorded baseline a gather may drift
// before it is called a regression.
//
// Calibrated against measured run-to-run spread on the reference machine
// (n=5, while other heavy work ran — deliberately a "busy machine" sample):
// linux-block max/min 1.20x, newtonsoft 1.05x. A 1.35x tolerance clears the
// worst observed spread by ~1.12x, and catches the 1.4x+ regressions that
// matter (the newtonsoft lane cost +50% on the day this was written).
//
// Transient noise is handled by RETRY, not by loosening this number: a gather
// that exceeds the tolerance is measured a second time and only fails if it
// exceeds twice. That keeps the happy path free and a flake cheap, while a
// real regression — which reproduces — still fails. Loosening the tolerance
// instead would trade a rare re-run for permanent blindness.
const perfTolerance = 1.35

// perfBaselineFile holds one entry per machine fingerprint. It is COMMITTED,
// like the judged floors, and reviewed the same way: a number may fall freely
// (that is an improvement), and only rises with a reason.
const perfBaselineFile = "testdata/perf-baseline.json"

type perfBaselines struct {
	Doc      string                      `json:"_doc"`
	Machines map[string]map[string]int64 `json:"machines"` // fingerprint -> corpus -> gather ms
}

// perfFingerprint identifies a machine CLASS, not a machine: OS, architecture
// and CPU count. Two boxes that share it are close enough for their gather
// timings to be compared; two that do not are not, and the gate stays silent
// rather than inventing a comparison. Hostname is deliberately excluded — it
// would fragment CI (fresh runner name per job) and leak an identifier into a
// committed file for nothing.
func perfFingerprint() string {
	return fmt.Sprintf("%s-%s-cpu%d", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
}

func loadPerfBaselines() *perfBaselines {
	b := &perfBaselines{Machines: map[string]map[string]int64{}}
	data, err := os.ReadFile(perfBaselineFile)
	if err != nil {
		return b
	}
	// A malformed baseline is a broken gate, not a passing one — but it must
	// not fail every corpus test either. Callers see "no baseline" and log it.
	if err := json.Unmarshal(data, b); err != nil {
		return &perfBaselines{Machines: map[string]map[string]int64{}}
	}
	if b.Machines == nil {
		b.Machines = map[string]map[string]int64{}
	}
	return b
}

// perfBaselineFor returns this machine's recorded gather time for a corpus.
func perfBaselineFor(corpus string) (time.Duration, bool) {
	byCorpus, ok := loadPerfBaselines().Machines[perfFingerprint()]
	if !ok {
		return 0, false
	}
	ms, ok := byCorpus[corpus]
	if !ok || ms <= 0 {
		return 0, false
	}
	return time.Duration(ms) * time.Millisecond, true
}

// recordPerfBaseline writes this machine's gather time for a corpus. Called
// only under RECORD_GOLDEN=1, the same switch the judged scoreboard uses.
//
// Recording is a DELIBERATE act: whatever the tree does at that moment becomes
// the number future runs are held to, so recording on a known-slow build pins
// the slowness as normal.
func recordPerfBaseline(corpus string, wall time.Duration) error {
	b := loadPerfBaselines()
	b.Doc = "Gather wall-time baselines per machine fingerprint (GOOS-GOARCH-cpuN). " +
		"Written by RECORD_GOLDEN=1, enforced by TestCorpusGolden within perfTolerance. " +
		"A fingerprint with no entry means the gate does not apply on that machine — " +
		"wall-clock is not comparable across hardware. Numbers may FALL freely; a rise " +
		"is a reviewed diff, exactly like a judged floor."
	fp := perfFingerprint()
	if b.Machines[fp] == nil {
		b.Machines[fp] = map[string]int64{}
	}
	b.Machines[fp][corpus] = wall.Milliseconds()

	// Deterministic, git-diffable output — sorted keys, trailing newline.
	if err := os.MkdirAll(filepath.Dir(perfBaselineFile), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(perfBaselineFile, append(data, '\n'), 0o644)
}

// perfBaselineCorpora lists the corpora carrying a baseline on this machine,
// sorted — used only for reporting.
func perfBaselineCorpora() []string {
	byCorpus := loadPerfBaselines().Machines[perfFingerprint()]
	out := make([]string, 0, len(byCorpus))
	for k := range byCorpus {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
