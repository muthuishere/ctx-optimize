package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/muthuishere/clicodemode/codemode"
)

// The shipped modules (internal/app/js, imported as "ctx:<name>") are checked
// against the registered ops, documented, and each one is run against a real
// gathered store.

func TestCodeModulesCheckClean(t *testing.T) {
	e := codemode.NewEngine(codeRegistry())
	if d := e.CheckModules(); len(d) != 0 {
		t.Fatalf("modules must check clean, got %+v", d)
	}
}

func TestCodeModulesAreListedAndDocumented(t *testing.T) {
	rc, out, errb := runCode(t, "", "modules")
	if rc != 0 {
		t.Fatalf("rc=%d err=%s", rc, errb)
	}
	for _, m := range []string{"ctx:find", "ctx:edit", "ctx:impact", "ctx:orient", "ctx:sweep", "ctx:investigate", "ctx:cite"} {
		if !strings.Contains(out, m) {
			t.Errorf("`code modules` must list %s:\n%s", m, out)
		}
	}
	// Every export carries a /** doc */ (it is what describe and skill show).
	for _, m := range []string{"ctx:find", "ctx:edit", "ctx:impact", "ctx:orient", "ctx:safe", "ctx:sweep", "ctx:investigate", "ctx:cite"} {
		_, d, _ := runCode(t, "", "describe", m)
		for _, line := range strings.Split(d, "\n") {
			if strings.HasPrefix(line, "  export ") && !strings.Contains(line, ": ") {
				t.Errorf("%s: export without a doc: %q", m, line)
			}
		}
	}
}

// runModuleProg runs src against the fixture store and returns the parsed value.
func runModuleProg(t *testing.T, src string) map[string]json.RawMessage {
	t.Helper()
	rc, out, errb := runCode(t, "", "run", writeProg(t, src), "--json")
	if rc != 0 {
		t.Fatalf("rc=%d out=%s err=%s", rc, out, errb)
	}
	r := parseResult(t, out)
	if !r.OK {
		t.Fatalf("program failed: %s", out)
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal(r.Value, &v); err != nil {
		t.Fatalf("value is not an object: %s", r.Value)
	}
	return v
}

func TestCodeModuleFind(t *testing.T) {
	t.Chdir(codeFixture(t))
	v := runModuleProg(t, `import { findCards } from "ctx:find"
const cards = await findCards("Greet", 3);
({cards})`)
	var cards []struct{ Label, Signature string }
	if err := json.Unmarshal(v["cards"], &cards); err != nil || len(cards) == 0 {
		t.Fatalf("findCards returned nothing: %s", v["cards"])
	}
	found := false
	for _, c := range cards {
		if c.Label == "Greet" && strings.Contains(c.Signature, "func Greet") {
			found = true
		}
	}
	if !found {
		t.Errorf("Greet card missing: %s", v["cards"])
	}
}

func TestCodeModuleEdit(t *testing.T) {
	t.Chdir(codeFixture(t))
	v := runModuleProg(t, `import { planEdit, planEdits } from "ctx:edit"
const one = await planEdit("Decorate");
const many = await planEdits(["Decorate", "NoSuchSymbol"]);
({one, many})`)
	var one struct {
		Symbol, File string
		Callers      []string
	}
	if err := json.Unmarshal(v["one"], &one); err != nil || one.Symbol != "Decorate" || one.File != "main.go" || len(one.Callers) == 0 {
		t.Fatalf("planEdit(Decorate) = %s (%v)", v["one"], err)
	}
	var many struct {
		Plans   []json.RawMessage
		Missing []struct{ Name string }
	}
	if err := json.Unmarshal(v["many"], &many); err != nil {
		t.Fatal(err)
	}
	if len(many.Plans) != 1 || len(many.Missing) != 1 || many.Missing[0].Name != "NoSuchSymbol" {
		t.Errorf("a miss must land in `missing`, not throw: %s", v["many"])
	}
}

func TestCodeModuleImpact(t *testing.T) {
	t.Chdir(codeFixture(t))
	v := runModuleProg(t, `import { blastRadius } from "ctx:impact"
const r = await blastRadius(["Decorate", "NoSuchSymbol"], 2);
({r})`)
	var r struct {
		Files   []struct{ File string }
		Missing []struct{ Name string }
	}
	if err := json.Unmarshal(v["r"], &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Files) == 0 || r.Files[0].File != "main.go" || len(r.Missing) != 1 {
		t.Errorf("blastRadius = %s", v["r"])
	}
}

func TestCodeModuleOrient(t *testing.T) {
	t.Chdir(codeFixture(t))
	v := runModuleProg(t, `import { orient, connect } from "ctx:orient"
const hubs = await orient(3);
const steps = await connect("Greet", "Decorate");
({hubs, steps})`)
	var hubs []struct{ Label string }
	if err := json.Unmarshal(v["hubs"], &hubs); err != nil || len(hubs) == 0 {
		t.Errorf("orient returned nothing: %s", v["hubs"])
	}
	var steps []string
	if err := json.Unmarshal(v["steps"], &steps); err != nil || len(steps) == 0 || !strings.Contains(steps[0], "-") {
		t.Errorf("connect(Greet, Decorate) = %s", v["steps"])
	}
}

// A module can only reach the registered ops: it cannot widen privilege.
func TestCodeModulesCannotReachBeyondOps(t *testing.T) {
	rc, out, _ := runCode(t, "", "eval", `import { orient } from "ctx:orient"; typeof ops.ctx.add`, "--json")
	if rc == 0 && strings.Contains(out, `"function"`) {
		t.Fatalf("no write op may exist: %s", out)
	}
}

func TestCodeModuleSweep(t *testing.T) {
	t.Chdir(codeFixture(t))
	v := runModuleProg(t, `import { sweep, sweepMany } from "ctx:sweep"
const one = await sweep("Decorate", {ext: ".go"});
const many = await sweepMany(["Greet", "(bad"], {ext: ".go"});
({one, many})`)
	var one struct {
		Count, FileCount int
		Files            []struct{ File string }
	}
	if err := json.Unmarshal(v["one"], &one); err != nil || one.Count < 2 || one.FileCount != 1 || one.Files[0].File != "main.go" {
		t.Fatalf("sweep(Decorate) = %s (%v)", v["one"], err)
	}
	var many struct {
		Results map[string]struct {
			Count int
			Error string
		}
	}
	if err := json.Unmarshal(v["many"], &many); err != nil || many.Results["Greet"].Count == 0 || many.Results["(bad"].Error == "" {
		t.Errorf("a bad regex must be reported per pattern, not thrown: %s", v["many"])
	}
}

func TestCodeModuleInvestigate(t *testing.T) {
	t.Chdir(codeFixture(t))
	v := runModuleProg(t, `import { investigate } from "ctx:investigate"
const r = await investigate({terms: "Greet", pattern: "Decorate", ext: ".go", limit: 2});
({r})`)
	var r struct {
		Symbols []struct{ Label string }
		Plan    struct{ Symbol string }
		Sweep   struct{ Count int }
	}
	if err := json.Unmarshal(v["r"], &r); err != nil || len(r.Symbols) == 0 || r.Plan.Symbol == "" || r.Sweep.Count == 0 {
		t.Fatalf("investigate = %s (%v)", v["r"], err)
	}
}

func TestCodeModuleCite(t *testing.T) {
	t.Chdir(codeFixture(t))
	v := runModuleProg(t, `import { verifyClaims } from "ctx:cite"
const r = await verifyClaims(["main.go:L1-L3", "nope.go:L1-L2"]);
({r})`)
	var r struct {
		OK     bool
		Claims []struct{ Holds bool }
	}
	if err := json.Unmarshal(v["r"], &r); err != nil || r.OK || len(r.Claims) != 2 || !r.Claims[0].Holds || r.Claims[1].Holds {
		t.Fatalf("verifyClaims = %s (%v)", v["r"], err)
	}
}
