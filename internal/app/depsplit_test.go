// THE structural guarantee of the companion split (ADR 2026-07-17, "FINAL
// architecture: companion binary after all"): the MAIN binary links ZERO
// database/queue drivers — they live only in cmd/ctx-optimize-adapters via
// internal/sources/connectors. `go list -deps` is the proof, not a code
// review. Runtime-skipped when the go toolchain is absent (house rule: no
// build tags).
package app

import (
	"os/exec"
	"strings"
	"testing"
)

// forbiddenDriverDeps are module-path fragments that must never appear in
// the main binary's dependency closure.
var forbiddenDriverDeps = []string{
	"github.com/jackc/pgx",            // postgres
	"github.com/go-sql-driver",        // mysql
	"github.com/microsoft/go-mssqldb", // mssql
	"go.mongodb.org/mongo-driver",     // mongo
	"github.com/redis/go-redis",       // redis
	"github.com/twmb/franz-go",        // kafka
	"github.com/nats-io",              // nats
	"github.com/xdg-go",               // mongo SCRAM auth chain
}

func TestMainBinaryHasNoDriverDeps(t *testing.T) {
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	out, err := exec.Command(gobin, "list", "-deps", "../../cmd/ctx-optimize").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	deps := string(out)
	for _, bad := range forbiddenDriverDeps {
		if strings.Contains(deps, bad) {
			t.Errorf("main binary dependency closure contains driver %q — connectors must link only into ctx-optimize-adapters", bad)
		}
	}
	// Sanity: the split still leaves the sources core in the main binary.
	if !strings.Contains(deps, "github.com/muthuishere/ctx-optimize/internal/sources") {
		t.Error("main binary lost internal/sources — the core (entry/scrub/run/bridge) must stay in")
	}
	// And the companion DOES carry them (the drivers went somewhere real).
	out, err = exec.Command(gobin, "list", "-deps", "../../cmd/ctx-optimize-adapters").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps (companion): %v\n%s", err, out)
	}
	for _, want := range forbiddenDriverDeps[:7] { // xdg-go is transitive, mongo covers it
		if !strings.Contains(string(out), want) {
			t.Errorf("companion binary is missing driver %q — connectors did not link", want)
		}
	}
}
