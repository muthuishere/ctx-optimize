// Package golden is the never-break acceptance net: it gathers known repos —
// two small committed fixtures (a multi-module config repo and a plain
// csproj/sln repo) plus env-gated real corpora (linux, Newtonsoft.Json) — and
// asserts the extraction contract as exact golden snapshots and named
// landmark facts. Any feature that shifts nodes, edges, or query ranking on
// these repos fails here first, with a readable diff.
//
// Snapshots live in testdata/golden/*.txt; regenerate deliberately with
// UPDATE_GOLDEN=1 go test ./internal/golden/ and review the diff like code.
package golden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/app"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

// runCLI drives the real binary entrypoint, hermetically.
func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	var out, errb bytes.Buffer
	if code := app.Run(args, &out, &errb); code != 0 {
		t.Fatalf("%v: exit %d: %s%s", args, code, out.String(), errb.String())
	}
	return out.String()
}

// gatherWithin runs init+add and enforces a wall-clock ceiling — performance
// is part of the golden contract even on the tiny hermetic fixtures (a repo
// this small taking seconds means something pathological landed in gather).
func gatherWithin(t *testing.T, ceiling time.Duration, repo, storeRoot string) {
	t.Helper()
	start := time.Now()
	runCLI(t, "init", "--path", repo, "--store", storeRoot)
	runCLI(t, "add", repo, "--path", repo, "--store", storeRoot)
	if wall := time.Since(start); wall > ceiling {
		t.Errorf("fixture gather took %s, performance ceiling %s — performance regression", wall.Round(time.Millisecond), ceiling)
	}
}

// copyTree copies a committed fixture into a temp dir so gather never writes
// into testdata (init writes pointer files + store state).
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// storeKeys walks a store root and returns every module key (a dir holding
// graph/), sorted, so snapshots cover multi-module trees without knowing the
// layout in advance.
func storeKeys(t *testing.T, root string) []string {
	t.Helper()
	var keys []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if d.Name() == "graph" {
			rel, _ := filepath.Rel(root, filepath.Dir(p))
			keys = append(keys, filepath.ToSlash(rel))
			return filepath.SkipDir
		}
		return nil
	})
	sort.Strings(keys)
	if len(keys) == 0 {
		t.Fatalf("no stores under %s", root)
	}
	return keys
}

// snapshot renders the full extraction contract of a store root as sorted,
// diffable lines: every node (id, kind, source, location) and every edge
// (source, relation, target, confidence) per module store.
func snapshot(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	for _, key := range storeKeys(t, root) {
		st, err := store.Open(root, key)
		if err != nil {
			t.Fatalf("open %s: %v", key, err)
		}
		nodes, err := st.Nodes()
		if err != nil {
			t.Fatal(err)
		}
		edges, err := st.Edges()
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, n := range nodes {
			lines = append(lines, fmt.Sprintf("N %s | %s | %s | %s", n.ID, n.Kind, n.Source, n.Location))
		}
		for _, e := range edges {
			lines = append(lines, fmt.Sprintf("E %s -%s-> %s | %s", e.Source, e.Relation, e.Target, e.Confidence))
		}
		sort.Strings(lines)
		fmt.Fprintf(&b, "== store %s (%d nodes, %d edges)\n", key, len(nodes), len(edges))
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

// queryTop runs a query scoped to dir and returns the top-k hit ids — the
// retrieval half of the golden contract (ranking changes are breaking too).
func queryTop(t *testing.T, storeRoot, dir, q string, k int) []string {
	t.Helper()
	out := runCLI(t, "query", q, "--path", dir, "--store", storeRoot, "--json")
	type hits struct {
		Hits []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
		} `json:"hits"`
	}
	// The CLI wraps scoped queries as {"result": {...}, "scope": ...}; a bare
	// store answers unwrapped. Accept both so the golden survives either path.
	var envelope struct {
		Result *hits `json:"result"`
	}
	var flat hits
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("query --json: %v\n%s", err, out)
	}
	parsed := envelope.Result
	if parsed == nil {
		if err := json.Unmarshal([]byte(out), &flat); err != nil {
			t.Fatalf("query --json: %v\n%s", err, out)
		}
		parsed = &flat
	}
	var ids []string
	for i, h := range parsed.Hits {
		if i >= k {
			break
		}
		ids = append(ids, h.Node.ID)
	}
	if len(ids) == 0 {
		t.Fatalf("query %q returned no hits — retrieval regression:\n%s", q, out)
	}
	return ids
}

// checkGolden compares got against testdata/golden/<name>.txt, or rewrites it
// under UPDATE_GOLDEN=1. The diff shown is line-level and complete.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden updated: %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s — run UPDATE_GOLDEN=1 go test ./internal/golden/ and review the new file", path)
	}
	if string(want) == got {
		return
	}
	t.Errorf("golden mismatch: %s\n%s", path, lineDiff(string(want), got))
}

// lineDiff is a minimal set-style diff: lines lost vs gained, sorted — enough
// to read an extraction change without a diff dependency.
func lineDiff(want, got string) string {
	w := map[string]bool{}
	for _, l := range strings.Split(want, "\n") {
		w[l] = true
	}
	g := map[string]bool{}
	for _, l := range strings.Split(got, "\n") {
		g[l] = true
	}
	var lost, gained []string
	for l := range w {
		if !g[l] {
			lost = append(lost, "- "+l)
		}
	}
	for l := range g {
		if !w[l] {
			gained = append(gained, "+ "+l)
		}
	}
	sort.Strings(lost)
	sort.Strings(gained)
	return strings.Join(append(lost, gained...), "\n")
}

// mustContain asserts a named landmark line exists in the snapshot — used so
// failures name the fact that broke even when the full diff is large.
func mustContain(t *testing.T, snap, what, substr string) {
	t.Helper()
	if !strings.Contains(snap, substr) {
		t.Errorf("landmark missing — %s: %q not found in snapshot", what, substr)
	}
}
