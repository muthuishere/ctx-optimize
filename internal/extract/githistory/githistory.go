// Package githistory is the tier-1 co-change producer: it derives
// "these files change together" edges purely from `git log` — no AST, no
// content read. History knows couplings content cannot express (impl↔test,
// code↔doc surface, schema↔migration); the spike that gated this build and
// every threshold below live in
// openspec/changes/2026-07-14-git-history-edges/proposal.md.
//
// It emits EDGES ONLY, between the file node ids the code/markdown producers
// already emit (dir-relative slash paths) — cross-batch edges are the
// schema's design intent. Best-effort like internal/extract/ignore: no git
// binary, not a repo, or empty history → empty batch, never an error that
// blocks add.
package githistory

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// Producer is the provenance tag; store.Replace scopes truth per producer,
// so every add restates (and prunes) exactly this producer's edges.
const Producer = "git-history"

// Thresholds — measured, not guessed: see the spec's STEP-0 evidence table.
const (
	windowCommits  = 500 // recency window: old couplings decay out; bounded runtime
	maxCommitFiles = 20  // bigger commits are bulk renames/scaffolds — they poison the signal
	minSupport     = 3   // below 3 shared commits, coincidence dominates
	maxPairs       = 200 // cap per store: bounded batch and query surface on huge repos
)

// Extract derives co-change edges for the git history of root.
func Extract(root string) (*schema.Batch, error) { return ExtractExcluding(root, nil) }

// ExtractExcluding is Extract with subtrees dropped — the multi-module
// fan-out passes each task's child-module dirs so a pair never spans into a
// subtree owned by another store.
func ExtractExcluding(root string, exclude []string) (*schema.Batch, error) {
	b := &schema.Batch{Producer: Producer}
	// --relative both filters commits to the current dir and re-relativizes
	// the printed paths, so ids match this store's node ids whether root is
	// the repo root or a module subdir. %x01 marks commit lines unambiguously
	// (a file could be named like a 40-hex sha).
	out, err := exec.Command("git", "-C", root, "log",
		"--relative", "--name-only", "--no-merges", "-n", strconv.Itoa(windowCommits),
		"--pretty=format:%x01%H", "--", ".").Output()
	if err != nil || len(out) == 0 {
		return b, nil // best-effort: not a repo / no git / no history
	}

	var exRel []string
	for _, ex := range exclude {
		if r, rerr := filepath.Rel(root, ex); rerr == nil && r != "." && !strings.HasPrefix(r, "..") {
			exRel = append(exRel, filepath.ToSlash(r)+"/")
		}
	}
	exists := map[string]bool{} // stat cache: dead files leave the graph, so their pairs must too
	eligible := func(rel string) bool {
		for _, p := range exRel {
			if strings.HasPrefix(rel, p) {
				return false
			}
		}
		if secretName(filepath.Base(rel)) {
			return false
		}
		ok, seen := exists[rel]
		if !seen {
			st, serr := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
			ok = serr == nil && !st.IsDir()
			exists[rel] = ok
		}
		return ok
	}

	type pair struct{ a, b string }
	pairCount := map[pair]int{}
	fileCount := map[string]int{}
	count := func(commit []string) {
		// Bulk gate first, on the files visible after pathspec/exclude
		// filtering (see spec: unfiltered size is a deferred refinement).
		if len(commit) == 0 || len(commit) > maxCommitFiles {
			return
		}
		var files []string
		for _, f := range commit {
			if eligible(f) {
				files = append(files, f)
			}
		}
		sort.Strings(files)
		for i, f := range files {
			fileCount[f]++
			for _, g := range files[i+1:] {
				pairCount[pair{f, g}]++
			}
		}
	}

	var commit []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "\x01"):
			count(commit)
			commit, seen = nil, map[string]bool{}
		case line == "":
		default:
			if !seen[line] {
				seen[line] = true
				commit = append(commit, line)
			}
		}
	}
	count(commit)

	type scored struct {
		pair
		n int
	}
	var top []scored
	for p, n := range pairCount {
		if n >= minSupport {
			top = append(top, scored{p, n})
		}
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].n != top[j].n {
			return top[i].n > top[j].n
		}
		if top[i].a != top[j].a {
			return top[i].a < top[j].a
		}
		return top[i].b < top[j].b
	})
	if len(top) > maxPairs {
		top = top[:maxPairs]
	}
	// Deterministic batch order (store artifacts stay git-diffable).
	sort.Slice(top, func(i, j int) bool {
		if top[i].a != top[j].a {
			return top[i].a < top[j].a
		}
		return top[i].b < top[j].b
	})
	for _, s := range top {
		b.Edges = append(b.Edges, schema.Edge{
			Source: s.a, Target: s.b, Relation: "co_changed_with", Confidence: schema.Inferred,
			Metadata: map[string]string{
				"synthesized_by":   "git-cochange",
				"support":          strconv.Itoa(s.n),
				"confidence_ratio": strconv.FormatFloat(float64(s.n)/float64(fileCount[s.a]), 'f', 2, 64),
			},
		})
	}
	return b, nil
}

// secretName mirrors the other producers' posture: even though edges only
// carry paths already refused by the content extractors, never let a
// secret-ish filename into the graph from this lane either.
func secretName(base string) bool {
	l := strings.ToLower(base)
	return strings.HasPrefix(l, ".env") ||
		strings.Contains(l, "secret") ||
		strings.Contains(l, "credential") ||
		strings.Contains(l, "password")
}
