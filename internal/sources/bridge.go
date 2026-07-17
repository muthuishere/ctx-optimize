// Exec bridge (ADR 2026-07-17, "FINAL architecture: companion binary after
// all"): the MAIN binary carries zero driver imports — real connectors live
// in internal/sources/connectors, compiled ONLY into the sibling
// `ctx-optimize-adapters` binary. When a dial misses the in-process registry
// (test stubs still register in-process and win), an ARMED bridge execs the
// companion with a names-only argv (`capture <NAME>`); the child inherits
// env + repo cwd, re-resolves the SAME env/.env ladder, and emits Batch JSON
// on stdout. No URL ever crosses argv. The companion itself never arms the
// bridge, so a registry miss there stays a plain error (no recursion).
package sources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// CompanionName is the capture companion's binary name (sans .exe).
const CompanionName = "ctx-optimize-adapters"

var (
	bridgeArmed   bool
	schemesOnce   sync.Once
	cachedSchemes []string
)

// ArmExecBridge turns the companion fallback on — called exactly once by the
// MAIN binary's entry (internal/app.Run). The companion and unit tests never
// arm it, so in-process registrations keep full authority.
func ArmExecBridge() { bridgeArmed = true }

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// companionPath locates the sibling binary beside our own executable first
// (npm platform packages and release archives ship them side by side), then
// falls back to PATH.
func companionPath() (string, error) {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), CompanionName+exeSuffix())
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	if p, err := exec.LookPath(CompanionName); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("source capture needs the %s companion (installed beside ctx-optimize by npm/releases) — reinstall (npm install -g @muthuishere/ctx-optimize) or download it from https://github.com/muthuishere/ctx-optimize/releases", CompanionName)
}

// bridgeCapture execs `ctx-optimize-adapters capture <NAME>` for one entry.
// argv is names-only: the entry must reduce to an env-var NAME (bare or
// $NAME); template entries need their URL folded into a single var. The
// child runs with cwd=repo so its Resolver ladder resolves identically.
// Non-zero exit = source failed (child stderr rides the error, scrubbed
// again by the caller).
func bridgeCapture(entry, repo string) (*schema.Batch, error) {
	name := SourceID(entry)
	if !IsEnvName(name) {
		return nil, fmt.Errorf("the %s companion takes env-var names only — put the full URL in a single env var (template entries capture only via an in-process connector)", CompanionName)
	}
	bin, err := companionPath()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, "capture", name)
	cmd.Dir = repo
	cmd.Env = os.Environ()
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(errb.String())
		if detail == "" {
			return nil, fmt.Errorf("%s: %s", CompanionName, err)
		}
		// The child's own wrapping ("<companion>: source NAME: … — prior
		// nodes kept") would double with dial's — strip it, keep the cause.
		detail = strings.TrimPrefix(detail, CompanionName+": ")
		detail = strings.TrimPrefix(detail, "source "+name+": ")
		detail = strings.TrimSuffix(detail, " — prior nodes kept")
		return nil, fmt.Errorf("%s", detail)
	}
	var b schema.Batch
	if err := json.Unmarshal(out.Bytes(), &b); err != nil {
		return nil, fmt.Errorf("%s emitted invalid Batch JSON: %v", CompanionName, err)
	}
	if err := b.Validate(); err != nil {
		return nil, fmt.Errorf("%s emitted an invalid Batch: %v", CompanionName, err)
	}
	return &b, nil
}

// bridgeHelp proxies `adapters help <scheme>` to the companion — param
// tables live only in connector code, so help never drifts from it.
func bridgeHelp(scheme string) (string, error) {
	bin, err := companionPath()
	if err != nil {
		return "", err
	}
	var out, errb bytes.Buffer
	cmd := exec.Command(bin, "help", scheme)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(errb.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("%s: %s", CompanionName, detail)
	}
	return out.String(), nil
}

// companionSchemes lists the companion's registered schemes (exec `schemes`),
// cached for the life of the process. Missing/broken companion → nil (the
// static connectorForScheme table still names the shipped set).
func companionSchemes() []string {
	if !bridgeArmed {
		return nil
	}
	schemesOnce.Do(func() {
		bin, err := companionPath()
		if err != nil {
			return
		}
		var out bytes.Buffer
		cmd := exec.Command(bin, "schemes")
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return
		}
		for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				cachedSchemes = append(cachedSchemes, line)
			}
		}
	})
	return cachedSchemes
}
