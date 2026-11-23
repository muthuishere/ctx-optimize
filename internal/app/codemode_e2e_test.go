package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// End-to-end tests for `ctx-optimize code` (ADR 2026-11-22-code-mode): real
// gather into a hermetic store, then programs run from a FILE, from STDIN, and
// as a DIRECT `eval`, plus the validation (`check`) and error contract.

// codeFixture gathers a tiny Go repo into a temp store and returns its path.
func codeFixture(t *testing.T) string {
	t.Helper()
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	repo := t.TempDir()
	files := map[string]string{
		"main.go":      "package main\n\n// Greet says hi.\nfunc Greet(name string) string {\n\treturn Decorate(name)\n}\n\n// Decorate wraps a name.\nfunc Decorate(s string) string {\n\treturn \"<\" + s + \">\"\n}\n\nfunc main() {\n\tGreet(\"x\")\n}\n",
		"main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tif Greet(\"a\") == \"\" {\n\t\tt.Fatal()\n\t}\n}\n",
	}
	for n, src := range files {
		if err := os.WriteFile(filepath.Join(repo, n), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errb bytes.Buffer
	if rc := Run([]string{"add", "--path", repo}, &out, &errb); rc != 0 {
		t.Fatalf("add: %s", errb.String())
	}
	return repo
}

// codeResult is the one object `--json` prints.
type codeResult struct {
	OK          bool            `json:"ok"`
	Value       json.RawMessage `json:"value"`
	Error       string          `json:"error"`
	Diagnostics []struct {
		Severity string `json:"severity"`
		Message  string `json:"message"`
	} `json:"diagnostics"`
	MS int `json:"ms"`
}

// runCode runs `ctx-optimize code <args>` with stdin fed from stdinSrc ("" = none).
func runCode(t *testing.T, stdinSrc string, args ...string) (rc int, stdout, stderr string) {
	t.Helper()
	if stdinSrc != "" {
		f := filepath.Join(t.TempDir(), "stdin")
		if err := os.WriteFile(f, []byte(stdinSrc), 0o644); err != nil {
			t.Fatal(err)
		}
		in, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		old := os.Stdin
		os.Stdin = in
		defer func() { os.Stdin = old; in.Close() }()
	}
	var out, errb bytes.Buffer
	rc = Run(append([]string{"code"}, args...), &out, &errb)
	return rc, out.String(), errb.String()
}

func parseResult(t *testing.T, stdout string) codeResult {
	t.Helper()
	var r codeResult
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("--json must print exactly ONE object, got: %v\n%s", err, stdout)
	}
	return r
}

func writeProg(t *testing.T, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "prog.js")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ---- DIRECT: eval needs no store, no file --------------------------------

func TestCodeEvalDirect(t *testing.T) {
	rc, out, errb := runCode(t, "", "eval", "[1,2,3].map(x => x * 2)", "--json")
	if rc != 0 {
		t.Fatalf("rc=%d out=%s err=%s", rc, out, errb)
	}
	r := parseResult(t, out)
	if !r.OK || string(bytes.Join(bytes.Fields(r.Value), nil)) != "[2,4,6]" {
		t.Fatalf("eval value = %s", r.Value)
	}
	// A failing expression is exit 1 (ran and failed), not 2.
	rc, out, _ = runCode(t, "", "eval", "null.x", "--json")
	if rc != 1 || parseResult(t, out).OK {
		t.Fatalf("a runtime failure must be exit 1 with ok:false, rc=%d out=%s", rc, out)
	}
}

// ---- FILE + STDIN: all six ops in one program ----------------------------

const allSixProgram = `
const p = ["--path", input.repo];
const q = await ops.ctx.query({args: ["Greet", ...p]});
const card = await ops.ctx.card({args: ["Greet", ...p]});
const plan = await ops.ctx.change_plan({args: ["Greet", ...p]});
const aff = await ops.ctx.affected({args: ["Decorate", ...p]});
const path = await ops.ctx.path({args: ["main", "Decorate", ...p]});
const hubs = await ops.ctx.hubs({args: ["--top", "3", ...p]});
({
  queryHit: q.hits.length > 0,
  sig: card.signature,
  callers: card.called_by.length,
  planKeys: Object.keys(plan),
  affectedKeys: Object.keys(aff),
  pathKeys: Object.keys(path),
  hubCount: hubs.hubs.length,
})`

func TestCodeRunFileAllSixOps(t *testing.T) {
	repo := codeFixture(t)
	prog := writeProg(t, allSixProgram)
	in, _ := json.Marshal(map[string]string{"repo": repo})
	rc, out, errb := runCode(t, "", "run", prog, "--input", string(in), "--json")
	if rc != 0 {
		t.Fatalf("rc=%d out=%s err=%s", rc, out, errb)
	}
	r := parseResult(t, out)
	if !r.OK {
		t.Fatalf("program failed: %s", out)
	}
	var v struct {
		QueryHit bool     `json:"queryHit"`
		Sig      string   `json:"sig"`
		Callers  int      `json:"callers"`
		HubCount int      `json:"hubCount"`
		Plan     []string `json:"planKeys"`
	}
	if err := json.Unmarshal(r.Value, &v); err != nil {
		t.Fatal(err)
	}
	if !v.QueryHit || !strings.Contains(v.Sig, "func Greet(name string) string") || v.Callers != 2 || v.HubCount == 0 || len(v.Plan) == 0 {
		t.Fatalf("unexpected result: %s", r.Value)
	}
}

func TestCodeRunFromStdinMatchesFile(t *testing.T) {
	repo := codeFixture(t)
	in, _ := json.Marshal(map[string]string{"repo": repo})
	_, fromFile, _ := runCode(t, "", "run", writeProg(t, allSixProgram), "--input", string(in), "--json")
	rc, fromStdin, errb := runCode(t, allSixProgram, "run", "-", "--input", string(in), "--json")
	if rc != 0 {
		t.Fatalf("stdin run rc=%d err=%s out=%s", rc, errb, fromStdin)
	}
	a, b := parseResult(t, fromFile), parseResult(t, fromStdin)
	if string(a.Value) != string(b.Value) {
		t.Fatalf("file and stdin must give the same value:\nfile:  %s\nstdin: %s", a.Value, b.Value)
	}
}

func TestCodeRunIsDeterministic(t *testing.T) {
	repo := codeFixture(t)
	prog := writeProg(t, allSixProgram)
	in, _ := json.Marshal(map[string]string{"repo": repo})
	var first string
	for i := 0; i < 3; i++ {
		_, out, _ := runCode(t, "", "run", prog, "--input", string(in), "--json")
		v := string(parseResult(t, out).Value)
		if i == 0 {
			first = v
		} else if v != first {
			t.Fatalf("run %d differs:\n%s\nvs\n%s", i, v, first)
		}
	}
}

func TestCodeParallelFanOutEqualsSerial(t *testing.T) {
	repo := codeFixture(t)
	prog := writeProg(t, `
const names = ["Greet", "Decorate", "main"];
const par = await Promise.all(names.map(n => ops.ctx.card({args: [n, "--path", input.repo]})));
const ser = [];
for (const n of names) { ser.push(await ops.ctx.card({args: [n, "--path", input.repo]})); }
({same: JSON.stringify(par) === JSON.stringify(ser), n: par.length})`)
	in, _ := json.Marshal(map[string]string{"repo": repo})
	rc, out, errb := runCode(t, "", "run", prog, "--input", string(in), "--json")
	if rc != 0 {
		t.Fatalf("rc=%d err=%s out=%s", rc, errb, out)
	}
	if v := string(bytes.Join(bytes.Fields(parseResult(t, out).Value), nil)); v != `{"n":3,"same":true}` {
		t.Fatalf("parallel and serial cards must agree: %s", v)
	}
}

// ---- READ-ONLY: running programs must not touch the store ----------------

func storeDigest(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		// metrics/ is the usage telemetry EVERY read verb appends to (the
		// "served" counters); the graph and manifest are what a program must not touch.
		if strings.Contains(rel, "metrics"+string(filepath.Separator)) {
			return nil
		}
		b, _ := os.ReadFile(p)
		lines = append(lines, fmt.Sprintf("%s %x", rel, sha256.Sum256(b)))
		return nil
	})
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func TestCodeRunNeverWritesTheStore(t *testing.T) {
	repo := codeFixture(t)
	store := os.Getenv("CTX_OPTIMIZE_STORE")
	before := storeDigest(t, store)
	in, _ := json.Marshal(map[string]string{"repo": repo})
	if rc, out, _ := runCode(t, "", "run", writeProg(t, allSixProgram), "--input", string(in), "--json"); rc != 0 {
		t.Fatalf("run failed: %s", out)
	}
	if after := storeDigest(t, store); after != before {
		t.Fatalf("a read-only program changed the store:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestCodeMutatingVerbsAreNotOperations(t *testing.T) {
	for _, op := range []string{"ctx.add", "ctx.sync", "ctx.up", "ctx.install", "ctx.remote", "ctx.config", "ctx.store", "ctx.update"} {
		rc, out, errb := runCode(t, "await ops."+op+"({args: []})", "check", "-")
		if rc != 2 || !strings.Contains(out+errb, "unknown operation") {
			t.Fatalf("%s must be an unknown operation (exit 2), rc=%d out=%s err=%s", op, rc, out, errb)
		}
	}
	rc, out, _ := runCode(t, "", "operations", "--json")
	if rc != 0 {
		t.Fatalf("operations rc=%d", rc)
	}
	var names []struct {
		Name        string `json:"name"`
		SideEffects string `json:"side_effects"`
	}
	if json.Unmarshal([]byte(out), &names) != nil {
		var wrapped struct {
			Operations []struct {
				Name        string `json:"name"`
				SideEffects string `json:"side_effects"`
			} `json:"operations"`
		}
		if err := json.Unmarshal([]byte(out), &wrapped); err != nil {
			t.Fatalf("operations --json unparseable: %v\n%s", err, out)
		}
		names = wrapped.Operations
	}
	if len(names) != 6 {
		t.Fatalf("exactly six operations expected, got %d:\n%s", len(names), out)
	}
	for _, n := range names {
		if n.SideEffects != "" && n.SideEffects != "none" {
			t.Errorf("%s declares side effects %q; every op must be read-only", n.Name, n.SideEffects)
		}
	}
}

// ---- VALIDATE: `check` catches problems and runs nothing ------------------

func TestCodeCheckValidation(t *testing.T) {
	for _, tc := range []struct {
		name, prog string
		rc         int
		want       string
	}{
		{"valid", `await ops.ctx.hubs({args: ["--top", "3"]})`, 0, "ok:"},
		{"typo'd operation gets did-you-mean", `await ops.ctx.qeury({args: ["x"]})`, 2, `did you mean "ctx.query"`},
		{"hyphenated name", `await ops.ctx.change-plan({args: ["x"]})`, 2, "subtraction"},
		{"literal arg of the wrong type", `await ops.ctx.card({args: [42]})`, 2, "must be string"},
		{"unknown input property", `await ops.ctx.card({symbol: "x"})`, 2, "not an allowed property"},
		{"syntax error reports position", "const a = ;", 2, "syntax error"},
		{"unknown namespace", `await ops.zzz.query({args: ["x"]})`, 2, "unknown operation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc, out, errb := runCode(t, tc.prog, "check", "-")
			if rc != tc.rc || !strings.Contains(out+errb, tc.want) {
				t.Fatalf("rc=%d (want %d), want %q in:\n%s%s", rc, tc.rc, tc.want, out, errb)
			}
		})
	}
}

func TestCodeCheckRunsNothing(t *testing.T) {
	// No store exists here (CTX_OPTIMIZE_STORE is an empty temp dir); a run of
	// this program would fail, but `check` must only validate and say ok.
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	rc, out, _ := runCode(t, `await ops.ctx.hubs({args: ["--top", "3"]})`, "check", "-")
	if rc != 0 || !strings.Contains(out, "ok:") {
		t.Fatalf("check must validate without running, rc=%d out=%s", rc, out)
	}
}

func TestCodeCheckMissingFileIsUsageError(t *testing.T) {
	rc, _, _ := runCode(t, "", "check", filepath.Join(t.TempDir(), "nope.js"))
	if rc != 2 {
		t.Fatalf("a missing program file must exit 2 (nothing ran), got %d", rc)
	}
}

// ---- ERRORS at run time: exit codes and the single --json object ---------

func TestCodeRunErrorContract(t *testing.T) {
	repo := codeFixture(t)
	in, _ := json.Marshal(map[string]string{"repo": repo})

	// A name with no near match at all: the verb's own error, still exit 1.
	rc, out, _ := runCode(t, `await ops.ctx.card({args: ["NoSuchSymbolZZ", "--path", input.repo]})`, "run", "-", "--input", string(in), "--json")
	r := parseResult(t, out)
	if rc != 1 || r.OK || !strings.Contains(r.Error, "no node matching") {
		t.Fatalf("unknown symbol must be exit 1 / ok:false, rc=%d out=%s", rc, out)
	}
	// A near miss must NEVER come back ok:true with a near node.
	rc, out, _ = runCode(t, `await ops.ctx.card({args: ["Greeet", "--path", input.repo]})`, "run", "-", "--input", string(in), "--json")
	r = parseResult(t, out)
	if rc != 1 || r.OK || !strings.Contains(r.Error, "Greet") ||
		!(strings.Contains(r.Error, "was not found exactly") || strings.Contains(r.Error, "did you mean")) {
		t.Fatalf("a near miss must be refused naming the nearest match, rc=%d out=%s", rc, out)
	}
	// "Greet hi" resolves through the FUZZY path: the CLI would answer ok with a
	// near node and a stderr note a program never sees. The guard refuses it...
	rc, out, _ = runCode(t, `await ops.ctx.card({args: ["Greet hi", "--path", input.repo]})`, "run", "-", "--input", string(in), "--json")
	r = parseResult(t, out)
	// Which layer refuses depends on the store: a TIE is refused by the verb
	// itself ("refusing to guess"); a lone fuzzy winner is refused by the guard
	// ("was not found exactly", unit-tested in TestCodeGuardsRefuseFuzzyAndBadFlags).
	// The invariant is that it is NEVER ok:true, and always names a candidate.
	if rc != 1 || r.OK || !strings.Contains(r.Error, "Greet") ||
		!(strings.Contains(r.Error, "was not found exactly") || strings.Contains(r.Error, "refusing to guess")) {
		t.Fatalf("a fuzzy resolution must be refused naming a candidate, rc=%d out=%s", rc, out)
	}
	// ...unless --fuzzy is the explicit opt-in.
	rc, out, _ = runCode(t, `(await ops.ctx.card({args: ["Greet hi", "--fuzzy", "--path", input.repo]})).resolved_via`, "run", "-", "--input", string(in), "--json")
	if rc != 0 || !strings.Contains(string(parseResult(t, out).Value), "fuzzy") {
		t.Fatalf("--fuzzy must opt in to a near match, rc=%d out=%s", rc, out)
	}
	// A mistyped flag is rejected with the accepted list, and is catchable.
	rc, out, _ = runCode(t, `let m; try { await ops.ctx.hubs({args: ["--bogus"]}) } catch (e) { m = e.message } m`, "run", "-", "--json")
	if rc != 0 || !strings.Contains(string(parseResult(t, out).Value), "unknown flag --bogus") {
		t.Fatalf("mistyped flag must throw a catchable error, rc=%d out=%s", rc, out)
	}
	// Uncaught, the same mistake fails the run.
	rc, out, _ = runCode(t, `await ops.ctx.hubs({args: ["--bogus"]})`, "run", "-", "--json")
	if rc != 1 || parseResult(t, out).OK {
		t.Fatalf("uncaught op failure must be exit 1, rc=%d out=%s", rc, out)
	}
	// Bad --input is a usage error: nothing ran.
	rc, out, _ = runCode(t, `input`, "run", "-", "--input", "{bad", "--json")
	if rc != 2 || !strings.Contains(parseResult(t, out).Error, "--input is not valid JSON") {
		t.Fatalf("bad --input must be exit 2, rc=%d out=%s", rc, out)
	}
	// A program with check errors is not run at all.
	rc, out, _ = runCode(t, `await ops.ctx.qeury({args: ["x"]})`, "run", "-", "--json")
	if rc != 2 || !strings.Contains(parseResult(t, out).Error, "nothing was run") {
		t.Fatalf("a program with errors must exit 2 and say nothing ran, rc=%d out=%s", rc, out)
	}
}

func TestCodeFanOutToleratesMisses(t *testing.T) {
	repo := codeFixture(t)
	prog := writeProg(t, `
const out = await Promise.all(input.symbols.map(async n => {
  try { const c = await ops.ctx.card({args: [n, "--path", input.repo]}); return {name: n, sig: c.signature}; }
  catch (e) { return {name: n, error: String(e.message).split("\n")[0]}; }
}));
({found: out.filter(x => !x.error).length, missing: out.filter(x => x.error).map(x => x.name)})`)
	in, _ := json.Marshal(map[string]any{"repo": repo, "symbols": []string{"Greet", "Nope1", "Decorate", "Nope2"}})
	rc, out, errb := runCode(t, "", "run", prog, "--input", string(in), "--json")
	if rc != 0 {
		t.Fatalf("rc=%d err=%s out=%s", rc, errb, out)
	}
	if v := string(bytes.Join(bytes.Fields(parseResult(t, out).Value), nil)); v != `{"found":2,"missing":["Nope1","Nope2"]}` {
		t.Fatalf("per-item catch must keep the hits and name the misses: %s", v)
	}
}

// ---- describe: each op is documented and read-only ------------------------

func TestCodeDescribeEveryOp(t *testing.T) {
	for _, op := range []string{"ctx.query", "ctx.card", "ctx.change_plan", "ctx.affected", "ctx.path", "ctx.hubs"} {
		rc, out, errb := runCode(t, "", "describe", op)
		if rc != 0 || !strings.Contains(out, "side effects: none") || !strings.Contains(out, `"args"`) {
			t.Errorf("describe %s: rc=%d err=%s out=%s", op, rc, errb, out)
		}
	}
	if rc, _, _ := runCode(t, "", "describe", "ctx.nope"); rc != 2 {
		t.Errorf("describe of an unknown op must exit 2, got %d", rc)
	}
}

func TestCodeHelpAndUnknownSubcommand(t *testing.T) {
	rc, out, _ := runCode(t, "", "--help")
	if rc != 0 || !strings.Contains(out, "Usage:") || !strings.Contains(out, "ctx-optimize code run") {
		t.Fatalf("code --help must print Code Mode's own usage, rc=%d out=%s", rc, out)
	}
	if rc, _, _ := runCode(t, "", "frobnicate"); rc != 2 {
		t.Fatalf("unknown subcommand must exit 2, got %d", rc)
	}
}
