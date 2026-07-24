// instructions.md — the committed, self-contained usage card (ADR
// 2026-07-17-bundled-adapter-templates, "Scaffold additions"). Teammates'
// agents inherit full store usage with ZERO installation on any agent CLI:
// the CLAUDE.md/AGENTS.md pointer blocks stay a one-liner and reference this
// file as the deep doc. The card lives inside a MANAGED BLOCK whose begin
// marker carries the writing binary's version stamp; refresh rewrites only
// the block and only UPGRADES (M5) — an older binary never downgrades a
// newer committed file, and user text outside the markers is never touched.
// One person upgrades + commits, the whole team's agents upgrade.
package project

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/version"
)

const InstructionsFile = Dir + "/instructions.md"

const (
	instrBeginPrefix = "<!-- ctx-optimize:instructions:begin v"
	instrBeginSuffix = " -->"
	instrEnd         = "<!-- ctx-optimize:instructions:end -->"
)

// The card body is a real markdown file (go:embed), same doctrine as the
// other scaffold templates — editable as what it is, not a Go string.
//
//go:embed templates/instructions.md
var instructionsBody string

func instructionsBlock(ver string) string {
	// Dev builds carry a leading "v" (git describe) — strip it so the marker
	// never stamps "vv0.8.0-…".
	ver = strings.TrimPrefix(ver, "v")
	return instrBeginPrefix + ver + instrBeginSuffix + "\n" +
		strings.TrimSuffix(instructionsBody, "\n") + "\n" +
		instrEnd + "\n"
}

// EnsureInstructions writes or refreshes .ctxoptimize/instructions.md for
// the running binary's version. Returns whether the file changed.
func EnsureInstructions(repo string) (bool, error) {
	return ensureInstructions(repo, version.Version)
}

func ensureInstructions(repo, ver string) (bool, error) {
	p := filepath.Join(repo, filepath.FromSlash(InstructionsFile))
	block := instructionsBlock(ver)
	data, err := os.ReadFile(p)
	switch {
	case os.IsNotExist(err):
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return false, err
		}
		return true, os.WriteFile(p, []byte(block), 0o644)
	case err != nil:
		return false, err
	}
	s := string(data)
	i := strings.Index(s, instrBeginPrefix)
	if i < 0 {
		// The user removed the managed block entirely — the file is theirs
		// now; never re-insert over a deliberate deletion.
		return false, nil
	}
	rel := strings.Index(s[i:], instrBeginSuffix)
	j := strings.Index(s, instrEnd)
	if rel < 0 || j < i {
		return false, fmt.Errorf("%s: malformed instructions markers", InstructionsFile)
	}
	fileVer := s[i+len(instrBeginPrefix) : i+rel]
	if newerVersion(fileVer, ver) {
		return false, nil // upgrade-only: an older binary never rewrites a newer file's block
	}
	out := s[:i] + strings.TrimSuffix(block, "\n") + s[j+len(instrEnd):]
	if out == s {
		return false, nil
	}
	return true, os.WriteFile(p, []byte(out), 0o644)
}

// newerVersion reports a strictly newer than b, comparing dotted integer
// segments (pre-release suffixes after "-" ignored; unparseable segments
// compare as 0 — a "0.0.0-dev" build never outranks a release).
func newerVersion(a, b string) bool {
	av, bv := versionInts(a), versionInts(b)
	for k := 0; k < 3; k++ {
		if av[k] != bv[k] {
			return av[k] > bv[k]
		}
	}
	return false
}

func versionInts(v string) [3]int {
	v, _, _ = strings.Cut(strings.TrimPrefix(v, "v"), "-")
	var out [3]int
	for k, seg := range strings.SplitN(v, ".", 3) {
		if k > 2 {
			break
		}
		n, err := strconv.Atoi(seg)
		if err == nil {
			out[k] = n
		}
	}
	return out
}
