// Package skills embeds the bundled agent skill and installs it — the binary
// ships via npm/brew with no repo alongside, so embed-and-write-out is the
// only reliable delivery. Fan-out: ~/.claude/skills always; ~/.agents/skills
// when codex is on PATH or --agents is passed (house pattern from crossmem).
package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed bundled
var bundled embed.FS

const skillName = "ctx-optimize"

// Targets returns the skill install directories for this machine. The
// SKILL.md format is a cross-CLI standard: Claude Code reads
// ~/.claude/skills; Copilot CLI and Devin CLI read ~/.agents/skills (Copilot
// also reads .claude/skills); Codex and Devin additionally have their own
// native dirs. Install everywhere the corresponding CLI is present.
func Targets(includeAgents bool) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	targets := []string{filepath.Join(home, ".claude", "skills", skillName)}
	if includeAgents || onPath("codex") || onPath("devin") || onPath("copilot") {
		targets = append(targets, filepath.Join(home, ".agents", "skills", skillName))
	}
	if onPath("codex") {
		targets = append(targets, filepath.Join(home, ".codex", "skills", skillName))
	}
	if onPath("devin") {
		targets = append(targets, filepath.Join(home, ".config", "devin", "skills", skillName))
	}
	return targets, nil
}

// Install writes the embedded skill into each target dir.
func Install(includeAgents bool) ([]string, error) {
	targets, err := Targets(includeAgents)
	if err != nil {
		return nil, err
	}
	src := "bundled/" + skillName
	for _, dst := range targets {
		err := fs.WalkDir(bundled, src, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, path)
			out := filepath.Join(dst, rel)
			if d.IsDir() {
				return os.MkdirAll(out, 0o755)
			}
			data, err := bundled.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(out, data, 0o644)
		})
		if err != nil {
			return nil, fmt.Errorf("install skill to %s: %w", dst, err)
		}
	}
	return targets, nil
}

// Uninstall removes the skill from every known target.
func Uninstall() ([]string, error) {
	targets, err := Targets(true)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, dst := range targets {
		if _, err := os.Stat(dst); err == nil {
			if err := os.RemoveAll(dst); err != nil {
				return nil, err
			}
			removed = append(removed, dst)
		}
	}
	return removed, nil
}

func onPath(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}
