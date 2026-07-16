package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/project"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// end-to-end through Run(): init → add (markdown) → add --json (the door) →
// query --json → remote init/push/pull → status. Hermetic via t.TempDir +
// CTX_OPTIMIZE_STORE.
func TestEndToEnd(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	md := "# Payment Service\n\n## Refund Flow\n\nSee [[Ledger]] for postings.\n"
	if err := os.WriteFile(filepath.Join(repo, "design.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(wantCode int, args ...string) (string, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := Run(args, &out, &errb)
		if code != wantCode {
			t.Fatalf("%v: exit %d (want %d): %s", args, code, wantCode, errb.String())
		}
		return out.String(), errb.String()
	}

	run(0, "init", "--path", repo)
	out, _ := run(0, "add", repo, "--path", repo)
	if !strings.Contains(out, "added") {
		t.Fatalf("add output: %s", out)
	}
	// Wiki-by-default: a successful add regenerates the wiki and says so.
	if !strings.Contains(out, "wiki: ") {
		t.Fatalf("add did not report the wiki: %s", out)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("CTX_OPTIMIZE_STORE"), filepath.Base(repo), "wiki", "index.md")); err != nil {
		t.Fatalf("add did not regenerate wiki/index.md: %v", err)
	}

	// The universal door: adapter JSON via file.
	batch := `{"producer":"pg-schema","nodes":[{"id":"pg://db/refunds","label":"refunds","kind":"table","file_type":"schema","source":"pg://db/refunds"}],"edges":[]}`
	bf := filepath.Join(t.TempDir(), "batch.json")
	os.WriteFile(bf, []byte(batch), 0o644)
	run(0, "add", "--json", bf, "--path", repo)

	// Door fails closed on garbage.
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte(`{"producer":"","nodes":[]}`), 0o644)
	_, errOut := run(1, "add", "--json", bad, "--path", repo)
	if !strings.Contains(errOut, "reject") {
		t.Fatalf("door did not fail closed: %s", errOut)
	}

	// Query finds both the markdown section and the adapter's table.
	out, _ = run(0, "query", "refund", "--path", repo, "--json")
	var res struct {
		Hits []struct {
			Node struct{ ID string }
		}
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("query --json not parseable: %v\n%s", err, out)
	}
	ids := map[string]bool{}
	for _, h := range res.Hits {
		ids[h.Node.ID] = true
	}
	if !ids["design.md::refund-flow"] || !ids["pg://db/refunds"] {
		t.Fatalf("query missed expected nodes, got %v", ids)
	}

	// `ask` is an alias for query.
	out, _ = run(0, "ask", "refund", "--path", repo)
	if !strings.Contains(out, "refund") && !strings.Contains(out, "Refund") {
		t.Fatalf("ask output: %s", out)
	}

	// push/pull take no arguments — the transport command lives in config.json.
	_, errURL := run(1, "remote", "push", "file:///nope", "--path", repo)
	if !strings.Contains(errURL, "takes no arguments") {
		t.Fatalf("expected argument rejection, got: %s", errURL)
	}

	// Nothing declared → clear error naming the config shape, exit 1.
	_, errNoRemote := run(1, "remote", "pull", "--path", repo)
	if !strings.Contains(errNoRemote, "no pull command") {
		t.Fatalf("expected missing-declaration error, got: %s", errNoRemote)
	}

	// Remote = a committed script (ADR scripted-remote-transports): declare a
	// cp-based transport, push, then pull on a fresh "machine".
	hostDir := t.TempDir()
	sync := "#!/bin/sh\nset -e\ncase \"$CTX_DIRECTION\" in\npush) rm -rf " + hostDir + "/\"$CTX_STORE_KEY\" && cp -R \"$CTX_STORE_DIR\" " + hostDir + "/\"$CTX_STORE_KEY\" ;;\npull) rm -rf \"$CTX_STORE_DIR\" && cp -R " + hostDir + "/\"$CTX_STORE_KEY\" \"$CTX_STORE_DIR\" ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(repo, ".ctxoptimize", "sync.sh"), []byte(sync), 0o755); err != nil {
		t.Fatal(err)
	}
	pc, err := project.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	pc.Remote = &project.Remote{Push: "sh .ctxoptimize/sync.sh", Pull: "sh .ctxoptimize/sync.sh"}
	if err := project.Save(repo, pc); err != nil {
		t.Fatal(err)
	}
	out, _ = run(0, "remote", "push", "--path", repo)
	if !strings.Contains(out, "push done") {
		t.Fatalf("push output: %s", out)
	}
	// Simulate a teammate: fresh store root, same repo — the cloned
	// .ctxoptimize/ carries the config AND the script, so bare pull works.
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	run(0, "remote", "pull", "--path", repo)
	out, _ = run(0, "status", "--path", repo, "--json")
	var st struct{ Nodes int }
	json.Unmarshal([]byte(out), &st)
	if st.Nodes == 0 {
		t.Fatalf("pulled store empty: %s", out)
	}
}

// The committable .ctxoptimize/ dir: adapter scripts dropped in adapters/
// are discovered and run on `add`, declared remote COMMANDS (env-var names
// expanded by the shell at run time) drive bare push, and "name" picks the
// store folder — everything travels with the repo, no secret ever committed.
func TestRepoConfigAdaptersAndRemote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("adapter command uses sh")
	}
	repo := t.TempDir()
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	remoteDir := t.TempDir()
	t.Setenv("CTX_TEST_REMOTE_DIR", remoteDir)

	adaptersDir := filepath.Join(repo, ".ctxoptimize", "adapters")
	os.MkdirAll(adaptersDir, 0o755)
	batch := `{"producer":"kafka-topics","nodes":[{"id":"kafka://orders","label":"orders","kind":"topic","file_type":"messaging","source":"kafka://orders"}],"edges":[]}`
	// Discovery: a .sh dropped into adapters/ IS the registration.
	os.WriteFile(filepath.Join(adaptersDir, "kafka.sh"), []byte("echo '"+batch+"'\n"), 0o644)
	// The remote is a declared COMMAND; $CTX_TEST_REMOTE_DIR expands in the
	// shell at run time — variable NAMES committed, values never.
	cfg := `{"name":"my-module",
	         "remote":{"push":"cp -R \"$CTX_STORE_DIR\" \"$CTX_TEST_REMOTE_DIR/out\"",
	                   "pull":"cp -R \"$CTX_TEST_REMOTE_DIR/out\" \"$CTX_STORE_DIR\""}}`
	os.WriteFile(filepath.Join(repo, ".ctxoptimize", "config.json"), []byte(cfg), 0o644)

	run := func(wantCode int, args ...string) (string, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := Run(args, &out, &errb)
		if code != wantCode {
			t.Fatalf("%v: exit %d (want %d): %s", args, code, wantCode, errb.String())
		}
		return out.String(), errb.String()
	}

	// `add` = refresh the world: built-ins + every declared adapter.
	out, _ := run(0, "add", "--path", repo)
	if !strings.Contains(out, "adapter kafka: 1 nodes") {
		t.Fatalf("adapter did not run: %s", out)
	}
	// "name" overrides the store folder: ~store~/my-module/.
	if _, err := os.Stat(filepath.Join(storeRoot, "my-module", "graph", "nodes.ndjson")); err != nil {
		t.Fatalf("custom module name not used: %v", err)
	}
	out, _ = run(0, "query", "orders topic", "--path", repo)
	if !strings.Contains(out, "kafka://orders") {
		t.Fatalf("adapter node not queryable: %s", out)
	}

	// Bare push runs the declared command; $CTX_TEST_REMOTE_DIR expands in
	// the shell. status reports the declaration, never expanded values.
	out, _ = run(0, "remote", "push", "--path", repo)
	if !strings.Contains(out, "push done") {
		t.Fatalf("push via repo config: %s", out)
	}
	out, _ = run(0, "status", "--path", repo, "--json")
	var st struct {
		Remote string `json:"remote"`
	}
	json.Unmarshal([]byte(out), &st)
	if st.Remote != "push + pull declared" {
		t.Fatalf("status remote line = %q", st.Remote)
	}
	if _, err := os.Stat(filepath.Join(remoteDir, "out", "manifest.json")); err != nil {
		t.Fatalf("push did not reach the host dir: %v", err)
	}

	// A broken adapter script fails the whole add, loudly.
	os.WriteFile(filepath.Join(adaptersDir, "boom.sh"), []byte("exit 3\n"), 0o644)
	_, errOut := run(1, "add", "--path", repo)
	if !strings.Contains(errOut, "adapter boom") {
		t.Fatalf("broken adapter not surfaced: %s", errOut)
	}
}

// merge combines module stores into a derived view (original producer
// metadata intact); export dumps it for other tools.
func TestMergeAndExport(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)

	run := func(wantCode int, args ...string) (string, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := Run(args, &out, &errb)
		if code != wantCode {
			t.Fatalf("%v: exit %d (want %d): %s", args, code, wantCode, errb.String())
		}
		return out.String(), errb.String()
	}

	// Two modules with one doc each.
	repoA, repoB := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(repoA, "a.md"), []byte("# Service A\n"), 0o644)
	os.WriteFile(filepath.Join(repoB, "b.md"), []byte("# Service B\n"), 0o644)
	run(0, "add", "--path", repoA)
	run(0, "add", "--path", repoB)

	keyA, _ := filepath.Abs(repoA)
	out, _ := run(0, "merge", filepath.Base(keyA), repoB, "--into", "everything")
	if !strings.Contains(out, "merged → ") {
		t.Fatalf("merge output: %s", out)
	}

	// The merged store is queryable and both modules are in it.
	var g struct {
		Nodes []struct {
			ID       string            `json:"id"`
			Metadata map[string]string `json:"metadata"`
		}
	}
	// export resolves the store by --path basename → the "everything" module.
	out, _ = run(0, "export", "--store", storeRoot, "--path", filepath.Join(storeRoot, "everything"))
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		t.Fatalf("export json: %v\n%s", err, out)
	}
	if len(g.Nodes) < 4 { // 2 docs + 2 sections
		t.Fatalf("merged export too small: %d nodes", len(g.Nodes))
	}
	for _, n := range g.Nodes {
		if n.Metadata["producer"] == "" || strings.HasPrefix(n.Metadata["producer"], "merge:") {
			t.Fatalf("merge clobbered provenance on %s: %q", n.ID, n.Metadata["producer"])
		}
	}

	// Unknown module fails clearly; dot export renders.
	_, errOut := run(1, "merge", "nope-not-a-module", "--into", "x")
	if !strings.Contains(errOut, "no module") {
		t.Fatalf("unknown module error: %s", errOut)
	}
	out, _ = run(0, "export", "--format", "dot", "--path", repoA)
	if !strings.Contains(out, "digraph ctxoptimize") {
		t.Fatalf("dot export: %s", out)
	}
}

// The learning loop end-to-end: save-result episodes, then reflect
// aggregates them into lessons + reflections/LESSONS.md in the store.
func TestSaveResultAndReflect(t *testing.T) {
	repo := t.TempDir()
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)

	run := func(wantCode int, args ...string) (string, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := Run(args, &out, &errb)
		if code != wantCode {
			t.Fatalf("%v: exit %d (want %d): %s", args, code, wantCode, errb.String())
		}
		return out.String(), errb.String()
	}

	// Validation surfaces as exit 1: question required, corrected needs text.
	_, errOut := run(1, "save-result", "--outcome", "useful", "--path", repo)
	if !strings.Contains(errOut, "question") {
		t.Fatalf("missing-question error: %s", errOut)
	}
	_, errOut = run(1, "save-result", "--question", "q", "--outcome", "corrected", "--path", repo)
	if !strings.Contains(errOut, "correction") {
		t.Fatalf("corrected-without-correction error: %s", errOut)
	}

	run(0, "save-result", "--question", "where is auth", "--answer", "internal/auth",
		"--type", "query", "--nodes", "auth.go::login, auth.go::verify", "--outcome", "useful", "--path", repo)
	run(0, "save-result", "--question", "how do refunds post", "--nodes", "auth.go::login",
		"--outcome", "useful", "--path", repo)
	run(0, "save-result", "--question", "is billing in auth", "--nodes", "billing.md::intro",
		"--outcome", "corrected", "--correction", "billing lives in internal/pay", "--path", repo)

	out, _ := run(0, "reflect", "--min-corroboration", "2", "--path", repo, "--json")
	var l struct {
		PreferredNodes []struct {
			Node   string `json:"node"`
			Useful int    `json:"useful"`
		} `json:"preferred_nodes"`
		DeadEnds    []struct{ Node string } `json:"dead_ends"`
		Corrections []struct {
			Correction string `json:"correction"`
		} `json:"corrections"`
	}
	if err := json.Unmarshal([]byte(out), &l); err != nil {
		t.Fatalf("reflect --json not parseable: %v\n%s", err, out)
	}
	if len(l.PreferredNodes) != 1 || l.PreferredNodes[0].Node != "auth.go::login" || l.PreferredNodes[0].Useful != 2 {
		t.Fatalf("preferred: %+v", l.PreferredNodes)
	}
	if len(l.DeadEnds) != 1 || l.DeadEnds[0].Node != "billing.md::intro" {
		t.Fatalf("dead ends: %+v", l.DeadEnds)
	}
	if len(l.Corrections) != 1 || l.Corrections[0].Correction != "billing lives in internal/pay" {
		t.Fatalf("corrections: %+v", l.Corrections)
	}
	lessons, err := os.ReadFile(filepath.Join(storeRoot, filepath.Base(mustAbs(t, repo)), "reflections", "LESSONS.md"))
	if err != nil {
		t.Fatalf("LESSONS.md not written: %v", err)
	}
	if !strings.Contains(string(lessons), "billing lives in internal/pay") {
		t.Fatalf("LESSONS.md missing correction:\n%s", lessons)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestUnknownCommandExits2(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Run([]string{"bogus"}, &out, &errb); code != 2 {
		t.Fatalf("want exit 2, got %d", code)
	}
	if !strings.Contains(errb.String(), "unknown command") {
		t.Fatalf("stderr: %s", errb.String())
	}
}

func TestVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Run([]string{"version"}, &out, &errb); code != 0 {
		t.Fatal("version failed")
	}
	if !strings.Contains(out.String(), "ctx-optimize") {
		t.Fatalf("version output: %s", out.String())
	}
}

// `add <path>` keys the store by the TARGET, never the cwd module — a
// positional gather must not Replace the current repo's graph.
func TestAddPositionalPathKeysStoreByTarget(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	other := filepath.Join(t.TempDir(), "other-repo")
	os.MkdirAll(other, 0o755)
	os.WriteFile(filepath.Join(other, "doc.md"), []byte("# Other\n"), 0o644)

	var out, errb bytes.Buffer
	if code := Run([]string{"add", other}, &out, &errb); code != 0 {
		t.Fatalf("add: %s", errb.String())
	}
	if _, err := os.Stat(filepath.Join(storeRoot, "other-repo", "graph", "nodes.ndjson")); err != nil {
		t.Fatalf("store not keyed by target: %v", err)
	}
}

// export --format all fans the graph out to every artifact under --out DIR.
func TestExportAllOut(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()
	os.WriteFile(filepath.Join(repo, "a.md"), []byte("# Service A\n\n## Setup\n"), 0o644)

	var out, errb bytes.Buffer
	if code := Run([]string{"add", "--path", repo}, &out, &errb); code != 0 {
		t.Fatalf("add: %s", errb.String())
	}

	// Without --out, all fails loudly.
	out.Reset()
	errb.Reset()
	if code := Run([]string{"export", "--format", "all", "--path", repo}, &out, &errb); code != 1 {
		t.Fatalf("all without --out: exit %d", code)
	}
	if !strings.Contains(errb.String(), "--out") {
		t.Fatalf("error should name --out: %s", errb.String())
	}

	dir := t.TempDir()
	out.Reset()
	errb.Reset()
	if code := Run([]string{"export", "--format", "all", "--path", repo, "--out", dir}, &out, &errb); code != 0 {
		t.Fatalf("export all: %s", errb.String())
	}
	for _, name := range []string{"graph.json", "graph.dot", "graph.graphml", "nodes.csv", "edges.csv", filepath.Join("obsidian", "_index.md")} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err != nil || st.Size() == 0 {
			t.Fatalf("artifact %s missing or empty: %v", name, err)
		}
		if !strings.Contains(out.String(), "wrote ") {
			t.Fatalf("no artifact lines printed: %s", out.String())
		}
	}
}

// `card X` answers with signature + doc + call graph — no file read needed.
func TestCardVerb(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()
	src := "package main\n\n// Greet says hi.\nfunc Greet(name string) string {\n\treturn name\n}\n\nfunc main() {\n\tGreet(\"x\")\n}\n"
	os.WriteFile(filepath.Join(repo, "main.go"), []byte(src), 0o644)

	var out, errb bytes.Buffer
	if code := Run([]string{"add", "--path", repo}, &out, &errb); code != 0 {
		t.Fatalf("add: %s", errb.String())
	}

	out.Reset()
	if code := Run([]string{"card", "Greet", "--path", repo}, &out, &errb); code != 0 {
		t.Fatalf("card: %s", errb.String())
	}
	text := out.String()
	for _, want := range []string{"sig: func Greet(name string) string", "doc: // Greet says hi.", "called by (1):", "main.go::main"} {
		if !strings.Contains(text, want) {
			t.Fatalf("card missing %q:\n%s", want, text)
		}
	}

	// --json returns the full CardData object.
	out.Reset()
	if code := Run([]string{"card", "Greet", "--path", repo, "--json"}, &out, &errb); code != 0 {
		t.Fatalf("card --json: %s", errb.String())
	}
	var c struct {
		Signature string   `json:"signature"`
		CalledBy  []string `json:"called_by"`
	}
	if err := json.Unmarshal(out.Bytes(), &c); err != nil {
		t.Fatalf("card --json not parseable: %v\n%s", err, out.String())
	}
	if c.Signature == "" || len(c.CalledBy) != 1 {
		t.Fatalf("card json: %+v", c)
	}

	// Query hits surface the signature inline (human render).
	out.Reset()
	if code := Run([]string{"query", "greet", "--path", repo}, &out, &errb); code != 0 {
		t.Fatalf("query: %s", errb.String())
	}
	if !strings.Contains(out.String(), "sig: func Greet") {
		t.Fatalf("query render missing signature:\n%s", out.String())
	}
}

func TestCardBodyHead(t *testing.T) {
	n := schema.Node{Source: "x.go", Location: "L2-L50"}
	dir := t.TempDir()
	var lines []string
	lines = append(lines, "package x")
	for i := 2; i <= 60; i++ {
		lines = append(lines, fmt.Sprintf("// line %d", i))
	}
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	body := bodyHead(dir, n)
	if !strings.HasPrefix(body, "// line 2") {
		t.Fatalf("body should start at L2: %q", body[:40])
	}
	if !strings.Contains(body, "more lines to L2-L50") {
		t.Fatalf("expected truncation note, got tail %q", body[len(body)-60:])
	}
	if bodyHead(dir, schema.Node{Source: "pg://db/t", Location: "L1"}) != "" {
		t.Fatal("adapter URIs must not be read as files")
	}
	if bodyHead(dir, schema.Node{Source: "missing.go", Location: "L1"}) != "" {
		t.Fatal("missing file must be silent")
	}
}
