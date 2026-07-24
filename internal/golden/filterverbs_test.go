package golden

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFixtureFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A hermetic fixture with go + npm code AND manifests, so the native filter
// verbs (ADR 2026-07-24) can be pinned end-to-end: deterministic output,
// the deps two-hop importer join, scope filtering, and query pre-rank
// narrowing. Deterministic because the verbs sort their output.
func writeFilterFixture(t *testing.T) (repo, storeRoot string) {
	t.Helper()
	repo = t.TempDir()
	storeRoot = t.TempDir()
	files := map[string]string{
		"go.mod": "module github.com/acme/svc\n\ngo 1.22\n\nrequire github.com/redis/go-redis/v9 v9.5.0\n",
		"main.go": "package main\n\nimport (\n\t\"fmt\"\n\t\"github.com/redis/go-redis/v9\"\n)\n\n" +
			"func main() { fmt.Println(redis.Nil) }\n",
		"package.json": `{"dependencies":{"react":"^18.2.0"},"devDependencies":{"vitest":"^1.0.0"}}`,
		"app.tsx":      "import React from 'react';\nimport { test } from 'vitest';\nexport const App = () => React.createElement('div');\n",
	}
	for p, c := range files {
		writeFixtureFile(t, repo, p, c)
	}
	runCLI(t, "init", "--path", repo, "--store", storeRoot)
	runCLI(t, "add", repo, "--path", repo, "--store", storeRoot)
	return repo, storeRoot
}

func TestGoldenFilterVerbs(t *testing.T) {
	repo, storeRoot := writeFilterFixture(t)
	base := []string{"--path", repo, "--store", storeRoot}

	// deps --scope dev → only vitest (react is runtime); deterministic order.
	out := runCLI(t, append([]string{"deps", "--scope", "dev", "--json"}, base...)...)
	var deps []struct {
		ID     string `json:"id"`
		Scopes string `json:"scopes"`
	}
	if err := json.Unmarshal([]byte(out), &deps); err != nil {
		t.Fatalf("deps --json: %v\n%s", err, out)
	}
	if len(deps) != 1 || deps[0].ID != "dep:npm/vitest" || deps[0].Scopes != "dev" {
		t.Fatalf("deps --scope dev = %+v (want only dep:npm/vitest/dev)", deps)
	}

	// deps --importers: the runtime deps resolve to their importing files via
	// the two-hop join (module --resolves_to--> dep, file --imports--> module).
	out = runCLI(t, append([]string{"deps", "--importers", "--ndjson"}, base...)...)
	imp := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var r struct {
			ID        string   `json:"id"`
			Importers []string `json:"importers"`
		}
		if json.Unmarshal([]byte(line), &r) == nil {
			imp[r.ID] = r.Importers
		}
	}
	if got := imp["dep:go/github.com/redis/go-redis/v9"]; len(got) != 1 || got[0] != "main.go" {
		t.Fatalf("redis importers = %v (want [main.go])", got)
	}
	if got := imp["dep:npm/react"]; len(got) != 1 || got[0] != "app.tsx" {
		t.Fatalf("react importers = %v (want [app.tsx])", got)
	}

	// edges --relation resolves_to → exactly the deplink links, INFERRED.
	out = runCLI(t, append([]string{"edges", "--relation", "resolves_to", "--ndjson"}, base...)...)
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var e struct {
			Relation, Confidence string
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("edges ndjson decode: %v", err)
		}
		if e.Relation != "resolves_to" || e.Confidence != "INFERRED" {
			t.Fatalf("edges --relation leaked %+v", e)
		}
		n++
	}
	if n < 2 { // at least redis + react
		t.Fatalf("resolves_to edges = %d (want ≥2)", n)
	}

	// nodes --kind service on a repo with no k8s → graceful empty, never error.
	out = runCLI(t, append([]string{"nodes", "--kind", "service"}, base...)...)
	if !strings.Contains(out, "(0 nodes)") {
		t.Fatalf("empty kind should say (0 nodes): %q", out)
	}

	// query pre-rank narrowing: "app" --kind file returns app.tsx (the file),
	// and --kind decl surfaces the App decl instead — proving the narrow ranks
	// within the kind rather than post-filtering a fixed top-N.
	out = runCLI(t, append([]string{"query", "app", "--kind", "file", "--json"}, base...)...)
	if !strings.Contains(out, "app.tsx") || strings.Contains(out, "::App") {
		t.Fatalf("query app --kind file should surface the FILE app.tsx, not the decl: %s", out)
	}
}

// Perf: the native filter verbs must stay fast on the fixture — they reuse the
// already-safe loader, so this is a cheap regression guard, not the streaming
// micro-benchmark (that lives in the stress harness).
func TestGoldenFilterVerbsPerf(t *testing.T) {
	repo, storeRoot := writeFilterFixture(t)
	start := time.Now()
	for i := 0; i < 10; i++ {
		runCLI(t, "deps", "--importers", "--json", "--path", repo, "--store", storeRoot)
	}
	if wall := time.Since(start); wall > 3*time.Second {
		t.Errorf("10× deps --importers took %s, ceiling 3s — filter perf regression", wall.Round(time.Millisecond))
	}
}
