package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// boundaryRepo writes a repo whose boundaries are known exactly, INCLUDING a
// real secret VALUE sitting next to the variable that names it. The value is
// the trap: this verb renders identifiers, and an identifier is a NAME.
const secretValue = "sk-live-NEVER-PRINT-THIS-0xDEADBEEF"

func boundaryRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	files := map[string]string{
		"go.mod": "module bnd\n\ngo 1.22\n",
		"main.go": `package main

import ("net/http"; "os"; "os/exec")

func main() {
	_ = os.Getenv("PAYMENTS_API_KEY")
	_ = os.Getenv("SERVICE_TIER")
	_ = exec.Command("git", "log")
	http.Get("https://api.weather.example/v1/now")
	http.HandleFunc("/healthz", nil)
}
`,
		// The secret's VALUE lives here, in the same tree the gather walks.
		".env.example": "PAYMENTS_API_KEY=" + secretValue + "\n",
	}
	for p, c := range files {
		full := filepath.Join(repo, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func gatherBoundaryRepo(t *testing.T) string {
	t.Helper()
	repo := boundaryRepo(t)
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	runCLI(t, 0, "init", "--path", repo)
	runCLI(t, 0, "add", repo, "--path", repo)
	return repo
}

// THE doctrine test. A secret's VALUE must never enter output — not in text,
// not in JSON, not under --all. The variable NAME is the fact; the value is
// never read, stored or printed, and this verb is the likeliest place in the
// product to break that.
func TestBoundariesNeverPrintsASecretValue(t *testing.T) {
	repo := gatherBoundaryRepo(t)
	for _, args := range [][]string{
		{"boundaries", "--path", repo},
		{"boundaries", "--path", repo, "--all"},
		{"boundaries", "--path", repo, "--json"},
		{"boundaries", "--path", repo, "--sensitive"},
	} {
		out, _ := runCLI(t, 0, args...)
		if strings.Contains(out, secretValue) {
			t.Fatalf("%v LEAKED a secret value:\n%s", args, out)
		}
		if !strings.Contains(out, "PAYMENTS_API_KEY") {
			t.Errorf("%v should still name the variable; got:\n%s", args, out)
		}
	}
}

// The summary must be an ANSWER: direction split, counts, and a citation on
// every line. This pins the shape a reader depends on.
func TestBoundariesRendersDirectionSplitAndCitations(t *testing.T) {
	repo := gatherBoundaryRepo(t)
	out, _ := runCLI(t, 0, "boundaries", "--path", repo)
	for _, want := range []string{
		"CONSUMES", "PROVIDES",
		"config.env", "process.exec", "network.http",
		"PAYMENTS_API_KEY", "SECRET",
		"api.weather.example",
		"main.go:L", // every line traces to file:line
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
	// SERVICE_TIER is the negative case: a plain var must NOT be marked.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "SERVICE_TIER") && strings.Contains(line, "SECRET") {
			t.Errorf("plain env var wrongly flagged SECRET: %q", line)
		}
	}
}

// --json is the machine door and must stay parseable with the semconv keys
// intact — a static boundary joins a runtime trace on otel.server.address.
func TestBoundariesJSONCarriesSemconv(t *testing.T) {
	repo := gatherBoundaryRepo(t)
	out, _ := runCLI(t, 0, "boundaries", "--path", repo, "--json")
	var r struct {
		Ports    int `json:"ports"`
		Consumes []struct {
			Transport string `json:"transport"`
			Total     int    `json:"total"`
			Entries   []struct {
				Identifier string            `json:"identifier"`
				Tier       string            `json:"tier"`
				Sensitive  bool              `json:"sensitive"`
				Cite       string            `json:"cite"`
				Otel       map[string]string `json:"otel"`
			} `json:"entries"`
		} `json:"consumes"`
		Provides []struct {
			Transport string `json:"transport"`
		} `json:"provides"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("--json not parseable: %v\n%s", err, out)
	}
	if r.Ports == 0 {
		t.Fatal("no ports in json")
	}
	if len(r.Provides) == 0 {
		t.Error("provides side missing from json")
	}
	var sawSemconv, sawSecret bool
	for _, g := range r.Consumes {
		for _, e := range g.Entries {
			if e.Otel["otel.server.address"] == "api.weather.example" {
				sawSemconv = true
			}
			if e.Identifier == "PAYMENTS_API_KEY" && e.Sensitive {
				sawSecret = true
			}
			if e.Cite == "" && e.Tier == "" {
				t.Errorf("entry %q has neither cite nor tier", e.Identifier)
			}
		}
	}
	if !sawSemconv {
		t.Error("otel.server.address missing — semconv must pass through to json")
	}
	if !sawSecret {
		t.Error("PAYMENTS_API_KEY not flagged sensitive in json")
	}
}

// `boundaries verify` must keep working unchanged: the bare verb took the
// namespace, the subcommand did not move.
func TestBoundariesVerifySubcommandStillWorks(t *testing.T) {
	repo := gatherBoundaryRepo(t)
	out, _ := runCLI(t, 0, "boundaries", "verify", "--path", repo)
	if !strings.Contains(out, "boundaries verify:") {
		t.Errorf("verify subcommand broken by the new bare verb:\n%s", out)
	}
	if _, _ = runCLI(t, 1, "boundaries", "bogus", "--path", repo); true {
		// an unknown subcommand must fail loudly, not silently summarise
	}
}

// Deterministic output: same store, same bytes. The store is git-diffable and
// so is anything that reads it.
func TestBoundariesOutputIsDeterministic(t *testing.T) {
	repo := gatherBoundaryRepo(t)
	first, _ := runCLI(t, 0, "boundaries", "--path", repo, "--json")
	for i := 0; i < 3; i++ {
		again, _ := runCLI(t, 0, "boundaries", "--path", repo, "--json")
		if again != first {
			t.Fatalf("run %d differed from run 0", i+1)
		}
	}
}
