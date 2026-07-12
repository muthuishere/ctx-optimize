// Package skills embeds the bundled agent skill and installs it — the binary
// ships via npm/brew with no repo alongside, so embed-and-write-out is the
// only reliable delivery. Targets are always ~/.claude/skills and
// ~/.agents/skills (the cross-CLI SKILL.md standard); per-repo triggering is
// the AGENTS.md/CLAUDE.md pointer block that `init` writes.
package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed bundled
var bundled embed.FS

const skillName = "ctx-optimize"

// Targets returns the skill install directories: always the two standard
// locations, nothing CLI-specific. ~/.claude/skills is read by Claude Code
// (and Copilot); ~/.agents/skills is the cross-CLI SKILL.md standard read by
// Copilot and Devin. Codex/OpenCode get their pointer via the AGENTS.md
// block `init` writes — the mechanism measured to actually trigger usage.
func Targets(includeAgents bool) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	_ = includeAgents // both targets are unconditional now
	return []string{
		filepath.Join(home, ".claude", "skills", skillName),
		filepath.Join(home, ".agents", "skills", skillName),
	}, nil
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
