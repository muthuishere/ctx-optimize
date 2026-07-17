// Exec-bridge integration (env-gated: CTX_OPTIMIZE_TEST_BRIDGE=1): builds
// BOTH real binaries, then proves the whole companion loop — main binary has
// no openapi connector in-process, so `capture <NAME>` must exec the sibling
// ctx-optimize-adapters with a names-only argv, the child re-resolves the
// env ladder (URL never on argv), dials an httptest OpenAPI server, and the
// Batch JSON round-trips through the bridge back out of the main binary.
package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func TestExecBridgeCapture(t *testing.T) {
	if os.Getenv("CTX_OPTIMIZE_TEST_BRIDGE") != "1" {
		t.Skip("set CTX_OPTIMIZE_TEST_BRIDGE=1 to run the exec-bridge integration test")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}

	bindir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	mainBin := filepath.Join(bindir, "ctx-optimize"+suffix)
	compBin := filepath.Join(bindir, "ctx-optimize-adapters"+suffix)
	for target, out := range map[string]string{
		"../../cmd/ctx-optimize":          mainBin,
		"../../cmd/ctx-optimize-adapters": compBin,
	} {
		cmd := exec.Command(gobin, "build", "-o", out, target)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", target, err, b)
		}
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openapi":"3.0.3","info":{"title":"Bridge","version":"1.0"},` +
			`"paths":{"/ping":{"get":{"summary":"Ping","responses":{"200":{"description":"ok"}}}}}}`))
	}))
	defer ts.Close()

	repo := t.TempDir() // no config: the repo root is the cwd itself

	// Names-only argv into the MAIN binary; the URL rides ONLY in env.
	cmd := exec.Command(mainBin, "capture", "BRIDGE_SPEC_URL")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "BRIDGE_SPEC_URL="+ts.URL)
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("main-binary capture via bridge failed: %v\nstderr: %s", err, errb.String())
	}
	var b schema.Batch
	if err := json.Unmarshal([]byte(out.String()), &b); err != nil {
		t.Fatalf("bridge output is not Batch JSON: %v\n%s", err, out.String())
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("bridge Batch failed validation: %v", err)
	}
	if len(b.Nodes) == 0 {
		t.Fatal("bridge Batch has no nodes — capture did not reach the openapi connector")
	}
	found := false
	for _, n := range b.Nodes {
		if strings.Contains(n.ID, "/ping") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a /ping path node in the bridged Batch; got %d nodes", len(b.Nodes))
	}

	// Companion gone → the loud install-hint error, not a silent pass.
	if err := os.Remove(compBin); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(mainBin, "capture", "BRIDGE_SPEC_URL")
	cmd.Dir = repo
	// Strip PATH so LookPath cannot find a globally installed companion.
	cmd.Env = append(envWithout("PATH"), "BRIDGE_SPEC_URL="+ts.URL)
	var errb2 strings.Builder
	cmd.Stderr = &errb2
	if err := cmd.Run(); err == nil {
		t.Fatal("capture succeeded with the companion missing")
	}
	if !strings.Contains(errb2.String(), "ctx-optimize-adapters companion") {
		t.Errorf("companion-missing error should name the companion + install hint; got: %s", errb2.String())
	}
}

// envWithout returns os.Environ() minus one variable.
func envWithout(name string) []string {
	var out []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, name+"=") {
			out = append(out, kv)
		}
	}
	return out
}
