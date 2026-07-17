package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/project"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/sources"
)

// cliStub is the app-level fake connector: okhost succeeds, badhost fails
// echoing the full resolved URL (password included — the scrub gate's prey),
// panichost panics with it.
type cliStub struct{}

func (cliStub) Scheme() string { return "stub" }
func (cliStub) Params() []sources.Param {
	return []sources.Param{{Name: "user:pass userinfo", Desc: "credentials", Cred: true}}
}
func (cliStub) Example() string { return "stub://$USER:$PASS@host:1234/db" }
func (cliStub) Capture(_ context.Context, u string) (*schema.Batch, error) {
	switch {
	case strings.Contains(u, "badhost"):
		return nil, fmt.Errorf("dial %s: authentication failed", u)
	case strings.Contains(u, "panichost"):
		panic("connector exploded on " + u)
	}
	id := "stub://okhost/billing/invoices"
	return &schema.Batch{Producer: "overridden", Nodes: []schema.Node{
		{ID: id, Label: "invoices", Kind: "table", FileType: "schema", Source: id},
	}}, nil
}

// TestSourcesCLI drives the whole Stage-1 surface through Run() — add-NAME
// vs add-dir disambiguation, capture, adapters list/help, up's TTL lane,
// reconcile/prune — and ends with THE SECRET-GREP GATE: a fake password
// (hunter2xyzq) planted in success, failure, AND panic paths must appear in
// neither the store tree, the repo tree, nor any captured output.
func TestSourcesCLI(t *testing.T) {
	sources.Register(cliStub{})
	t.Cleanup(func() { sources.Unregister("stub") })

	repo := t.TempDir()
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	if err := os.WriteFile(filepath.Join(repo, "notes.md"), []byte("# Billing\n\n## Invoices\n\ntext\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	const secret = "hunter2xyzq"
	t.Setenv("APP_STUB_URL", "stub://alice:"+secret+"@okhost:1/billing")
	t.Setenv("BAD_STUB_URL", "stub://alice:"+secret+"@badhost:1/billing")
	t.Setenv("PANIC_STUB_URL", "stub://alice:"+secret+"@panichost:1/billing")

	var transcript bytes.Buffer // EVERY byte of output lands here for the gate
	run := func(wantCode int, args ...string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := Run(args, &out, &errb)
		transcript.WriteString(out.String())
		transcript.WriteString(errb.String())
		if code != wantCode {
			t.Fatalf("%v: exit %d (want %d)\nstdout: %s\nstderr: %s", args, code, wantCode, out.String(), errb.String())
		}
		return out.String() + errb.String()
	}

	run(0, "init", "--path", repo)
	// Scaffold wrote the by-construction ignore.
	gi, err := os.ReadFile(filepath.Join(repo, ".ctxoptimize", ".gitignore"))
	if err != nil || !strings.Contains(string(gi), ".env*") || !strings.Contains(string(gi), "!.env.example") {
		t.Fatalf(".ctxoptimize/.gitignore: %q, %v", gi, err)
	}

	// ---- add: dir positional (unchanged semantics) vs NAME positional ----
	out := run(0, "add", repo, "--path", repo)
	if !strings.Contains(out, "added") {
		t.Fatalf("dir add broke: %s", out)
	}
	out = run(0, "add", "APP_STUB_URL", "--path", repo)
	for _, want := range []string{"APP_STUB_URL ← env", "captured (1 nodes", "recorded APP_STUB_URL"} {
		if !strings.Contains(out, want) {
			t.Fatalf("add NAME output missing %q:\n%s", want, out)
		}
	}
	cfg, err := project.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sources) != 1 || cfg.Sources[0] != "APP_STUB_URL" {
		t.Fatalf("config sources = %v", cfg.Sources)
	}
	// Idempotent: a second add records nothing twice.
	run(0, "add", "APP_STUB_URL", "--path", repo)
	if cfg, _ := project.Load(repo); len(cfg.Sources) != 1 {
		t.Fatalf("re-add duplicated the entry: %v", cfg.Sources)
	}
	// A var-shaped arg whose var is unset is STILL the source lane (shape
	// wins) — it must not fall through to a dir gather.
	out = run(1, "add", "SOME_UNSET_NAME", "--path", repo)
	if !strings.Contains(out, "SOME_UNSET_NAME not set") {
		t.Fatalf("unset-name add: %s", out)
	}
	// Failed capture: loud, non-zero, nothing recorded.
	out = run(1, "add", "BAD_STUB_URL", "--path", repo)
	if !strings.Contains(out, "not captured") {
		t.Fatalf("failed add: %s", out)
	}
	if cfg, _ := project.Load(repo); len(cfg.Sources) != 1 {
		t.Fatalf("failed add recorded: %v", cfg.Sources)
	}

	// ---- capture: Batch JSON to stdout, no store write ----
	out = run(0, "capture", "APP_STUB_URL", "--path", repo)
	var b schema.Batch
	if err := json.Unmarshal([]byte(out), &b); err != nil {
		t.Fatalf("capture stdout not a batch: %v\n%s", err, out)
	}
	if b.Producer != "source:APP_STUB_URL" || len(b.Nodes) != 1 {
		t.Fatalf("capture batch: %+v", b)
	}
	run(1, "capture", "stub://raw:url@host/db", "--path", repo) // names only on argv
	run(1, "capture", "PANIC_STUB_URL", "--path", repo)         // panic → failed, not a crash

	// ---- adapters list + help ----
	out = run(0, "adapters", "--path", repo)
	for _, want := range []string{"APP_STUB_URL", "schemes:", "postgres", "stub"} {
		if !strings.Contains(out, want) {
			t.Fatalf("adapters list missing %q:\n%s", want, out)
		}
	}
	out = run(0, "adapters", "help", "stub", "--path", repo)
	for _, want := range []string{"stub://$USER:$PASS@host:1234/db", "percent-encoded", "ctx-optimize add MY_STUB_URL", "credential"} {
		if !strings.Contains(out, want) {
			t.Fatalf("adapters help missing %q:\n%s", want, out)
		}
	}
	run(1, "adapters", "help", "ftp", "--path", repo)

	// ---- status shows source staleness ----
	out = run(0, "status", "--path", repo)
	if !strings.Contains(out, "APP_STUB_URL captured") {
		t.Fatalf("status missing source staleness:\n%s", out)
	}

	// ---- up: TTL lane, strict, failure visibility ----
	out = run(0, "up", "--path", repo)
	if !strings.Contains(out, "skipped (captured") { // fresh within 24h TTL
		t.Fatalf("up did not TTL-skip the fresh source:\n%s", out)
	}
	out = run(0, "up", "--path", repo, "--sources=always")
	if !strings.Contains(out, "captured (1 nodes") {
		t.Fatalf("up --sources=always did not dial:\n%s", out)
	}
	out = run(0, "up", "--path", repo, "--sources=never")
	if !strings.Contains(out, "--sources=never") {
		t.Fatalf("up --sources=never: %s", out)
	}
	run(1, "up", "--path", repo, "--sources=bogus")

	// Declare failing + panicking + unset sources directly in config (the
	// teammate-without-credentials / broken-DB world).
	cfg, _ = project.Load(repo)
	cfg.Sources = append(cfg.Sources, "BAD_STUB_URL", "PANIC_STUB_URL", "MISSING_TEAM_URL")
	if err := project.Save(repo, cfg); err != nil {
		t.Fatal(err)
	}
	out = run(0, "up", "--path", repo, "--sources=always") // loud but exit 0
	for _, want := range []string{"FAILED", "prior nodes kept", "panic", "MISSING_TEAM_URL not set", "1 captured, 1 skipped, 2 failed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("up outcome report missing %q:\n%s", want, out)
		}
	}
	out = run(1, "up", "--path", repo, "--sources=always", "--strict")
	if !strings.Contains(out, "--strict") {
		t.Fatalf("strict error: %s", out)
	}

	// ---- reconcile: undeclared source producers reported, pruned on ask ----
	cfg, _ = project.Load(repo)
	cfg.Sources = nil
	if err := project.Save(repo, cfg); err != nil {
		t.Fatal(err)
	}
	out = run(0, "up", "--path", repo)
	if !strings.Contains(out, "no longer declared") || !strings.Contains(out, "APP_STUB_URL") {
		t.Fatalf("reconcile report missing:\n%s", out)
	}
	out = run(0, "up", "--path", repo, "--prune-sources")
	if !strings.Contains(out, "pruned") {
		t.Fatalf("prune report missing:\n%s", out)
	}
	out = run(0, "query", "invoices", "--path", repo, "--json")
	if strings.Contains(out, "stub://okhost/billing/invoices") {
		t.Fatalf("pruned source nodes still answer queries:\n%s", out)
	}
	out = run(0, "status", "--path", repo)
	if strings.Contains(out, "APP_STUB_URL captured") {
		t.Fatalf("status still reports the pruned source:\n%s", out)
	}

	// ---- THE SECRET-GREP GATE (C1 merge gate) ----
	needles := []string{secret, url.QueryEscape(secret)}
	if strings.Contains(transcript.String(), needles[0]) || strings.Contains(transcript.String(), needles[1]) {
		t.Fatalf("secret leaked into CLI output")
	}
	for _, root := range []string{storeRoot, repo} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, n := range needles {
				if bytes.Contains(data, []byte(n)) {
					t.Errorf("secret found on disk: %s", path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
