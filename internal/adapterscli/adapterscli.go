// Package adapterscli is the entry for ctx-optimize-adapters — the capture
// companion (ADR 2026-07-17, "FINAL architecture: companion binary after
// all"). Driver imports live ONLY in internal/sources/connectors, blank-
// imported by the companion's cmd shim; the main binary stays driver-free
// and execs this one to dial. It is a capture engine, not a second product
// surface: capture <ENV_NAME> · help <scheme> · schemes · --version.
//
// Deliberately NOT internal/app: that package embeds the dashboard UI and
// the 32MB tree-sitter bundle — the companion carries drivers + sources
// core only.
package adapterscli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/muthuishere/ctx-optimize/internal/project"
	"github.com/muthuishere/ctx-optimize/internal/sources"
	"github.com/muthuishere/ctx-optimize/internal/version"
)

// Run dispatches args (without argv[0]) and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	var err error
	switch cmd, rest := args[0], args[1:]; cmd {
	case "capture": // one connector → Batch JSON on stdout (names-only argv)
		err = cmdCapture(rest, stdout)
	case "help":
		if len(rest) != 1 {
			err = fmt.Errorf("usage: %s help <scheme>", sources.CompanionName)
		} else {
			var card string
			if card, err = sources.HelpCard(rest[0]); err == nil {
				fmt.Fprint(stdout, card)
			}
		}
	case "schemes": // sorted registered schemes, one per line (bridge cache feed)
		for _, s := range sources.RegisteredSchemes() {
			fmt.Fprintln(stdout, s)
		}
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "%s %s (%s, %s)\n", sources.CompanionName, version.Version, version.Commit, version.Date)
	default:
		fmt.Fprintf(stderr, "%s: unknown command %q\n\n", sources.CompanionName, cmd)
		usage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", sources.CompanionName, err)
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `%s — ctx-optimize's capture companion (database/queue/API drivers live here)

usage:
  %[1]s capture <ENV_NAME>   dial the source named by the env var, Batch JSON on stdout
  %[1]s help <scheme>        setup card for one connector
  %[1]s schemes              registered connector schemes, one per line
  %[1]s --version            print version

Invoked by ctx-optimize's exec bridge; argv carries NAMES only — the value
(a URL) lives in the environment or .ctxoptimize/.env at the repo root.
`, sources.CompanionName)
}

// cmdCapture mirrors the main binary's capture contract: ONE env-var name on
// argv, resolved through the same env → .ctxoptimize/.env → .env ladder from
// the repo root (found by walking up from cwd, exactly how the main binary's
// scope resolution does), one dial, Batch JSON on stdout, no store write.
func cmdCapture(args []string, stdout io.Writer) error {
	if len(args) != 1 || !sources.IsEnvName(args[0]) {
		return fmt.Errorf("usage: %s capture <ENV_NAME> — names only on argv (^[A-Z_][A-Z0-9_]*$); the value (a URL) lives in the environment or .ctxoptimize/.env", sources.CompanionName)
	}
	repo, err := repoRoot()
	if err != nil {
		return err
	}
	b, err := sources.CaptureOnly(args[0], repo)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(b)
}

// repoRoot walks up from cwd to the nearest .ctxoptimize/config.json — the
// same "how git finds .git" rule as the main binary; no config → cwd itself.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return "", err
	}
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, filepath.FromSlash(project.FileName))); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir, nil
		}
		d = parent
	}
}
