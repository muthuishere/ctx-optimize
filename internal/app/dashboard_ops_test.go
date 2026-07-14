package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/audit"
	"github.com/muthuishere/ctx-optimize/internal/dashboard"
)

// TestDashboardOnboardFlow drives the dashboard mutation endpoints wired to
// the REAL Ops (the same command funcs the CLI dispatches to) against a
// fixture repo: onboard scan → confirm (init + add, streamed) → stores lists
// it → re-gather → audit has every step → `log` prints it.
func TestDashboardOnboardFlow(t *testing.T) {
	repo := t.TempDir()
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	os.WriteFile(filepath.Join(repo, "design.md"),
		[]byte("# Payment Service\n\n## Refund Flow\n\nledger postings.\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "main.go"),
		[]byte("package main\n\nfunc main() { helper() }\n\nfunc helper() {}\n"), 0o644)

	srv := httptest.NewServer(dashboard.NewHandler(storeRoot, serveOps(storeRoot)))
	defer srv.Close()

	var tok struct {
		Token string `json:"token"`
	}
	resp, err := http.Get(srv.URL + "/api/token")
	if err != nil {
		t.Fatal(err)
	}
	json.NewDecoder(resp.Body).Decode(&tok)
	resp.Body.Close()

	post := func(path, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		req.Header.Set("X-Ctx-Token", tok.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	repoJSON, _ := json.Marshal(repo)

	// 1. Scan preview — flat repo, no modules found.
	code, body := post("/api/onboard", `{"path":`+string(repoJSON)+`}`)
	if code != 200 || !strings.Contains(body, `"modules"`) {
		t.Fatalf("onboard scan: %d %s", code, body)
	}

	// 2. Confirm: init + add with streamed progress ending in DONE.
	code, body = post("/api/onboard/confirm", `{"path":`+string(repoJSON)+`,"name":"fixture","modules":[]}`)
	if code != 200 || !strings.Contains(body, "store ready") ||
		!strings.Contains(body, "added") || !strings.HasSuffix(strings.TrimSpace(body), "DONE") {
		t.Fatalf("onboard confirm: %d %s", code, body)
	}

	// 3. The store exists under the chosen name with real nodes.
	resp, err = http.Get(srv.URL + "/api/stores")
	if err != nil {
		t.Fatal(err)
	}
	var stores []dashboard.StoreInfo
	json.NewDecoder(resp.Body).Decode(&stores)
	resp.Body.Close()
	found := false
	for _, s := range stores {
		if s.Key == "fixture" && s.Nodes > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("fixture store missing: %+v", stores)
	}

	// 4. Re-gather streams the same add output.
	code, body = post("/api/repo/add", `{"path":`+string(repoJSON)+`}`)
	if code != 200 || !strings.HasSuffix(strings.TrimSpace(body), "DONE") {
		t.Fatalf("repo add: %d %s", code, body)
	}

	// 5. Every mutation landed in the audit, in order.
	lines, err := audit.List(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, l := range lines {
		if l.Actor != "dashboard" {
			t.Fatalf("actor: %+v", l)
		}
		actions = append(actions, l.Action)
	}
	want := []string{"onboard.scan", "onboard.confirm", "repo.add"}
	if len(actions) != len(want) {
		t.Fatalf("audit actions: %v", actions)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("audit actions: %v (want %v)", actions, want)
		}
	}
	// The confirm line fingerprints the config file it wrote.
	if lines[1].AfterHash == "" {
		t.Fatalf("onboard.confirm missing after_hash: %+v", lines[1])
	}

	// 6. `log` prints the same history; --json round-trips.
	var out, errb bytes.Buffer
	if code := Run([]string{"log"}, &out, &errb); code != 0 {
		t.Fatalf("log: %s", errb.String())
	}
	if !strings.Contains(out.String(), "onboard.confirm") {
		t.Fatalf("log output: %s", out.String())
	}
	out.Reset()
	if code := Run([]string{"log", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("log --json: %s", errb.String())
	}
	var parsed []audit.Line
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil || len(parsed) != 3 {
		t.Fatalf("log --json: %v %s", err, out.String())
	}
}

// TestConfigSetWritesAudit: the CLI door logs through the same writer.
func TestConfigSetWritesAudit(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	var out, errb bytes.Buffer
	if code := Run([]string{"config", "instructions", "CLAUDE"}, &out, &errb); code != 0 {
		t.Fatalf("config set: %s", errb.String())
	}
	lines, err := audit.List(storeRoot)
	if err != nil || len(lines) != 1 {
		t.Fatalf("audit: %v %v", lines, err)
	}
	if lines[0].Actor != "cli" || lines[0].Action != "config.set instructions=CLAUDE" || lines[0].AfterHash == "" {
		t.Fatalf("audit line: %+v", lines[0])
	}
}
