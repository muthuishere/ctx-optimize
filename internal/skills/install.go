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
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed bundled
var bundled embed.FS

// OnPath reports whether a CLI binary is installed on this machine.
func OnPath(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

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

// InstallDir writes the embedded skill into one target dir — an EXACT
// replace, staged in a temp sibling and swapped in. A plain file-by-file
// overwrite would leave files a previous version shipped but this one
// dropped as stale orphans an agent could still read; the swap guarantees
// the installed skill is byte-for-byte this binary's bundle.
func InstallDir(dst string) error {
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, "."+skillName+"-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage) // no-op after the successful rename
	src := "bundled/" + skillName
	err = fs.WalkDir(bundled, src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		out := filepath.Join(stage, rel)
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
		return err
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.Rename(stage, dst)
}

// SkillTargets maps the global `skills` setting to install dirs: CLAUDE
// (~/.claude/skills), AGENTS (~/.agents/skills), or ALL (default for "";
// BOTH accepted as alias). Typos are refused, never silently widened.
func SkillTargets(choice string) ([]string, error) {
	claude, err := ClaudeSkillDir()
	if err != nil {
		return nil, err
	}
	agents, err := AgentsSkillDir()
	if err != nil {
		return nil, err
	}
	switch strings.ToUpper(strings.TrimSpace(choice)) {
	case "", "ALL", "BOTH":
		return []string{claude, agents}, nil
	case "CLAUDE":
		return []string{claude}, nil
	case "AGENTS":
		return []string{agents}, nil
	}
	return nil, fmt.Errorf("skills %q: want CLAUDE, AGENTS, or ALL", choice)
}

// HookPlatforms maps the global `hooks` setting to the platforms whose hook
// files install may write: CLAUDE (~/.claude/settings.json — also read
// natively by Devin), AGENTS (the AGENTS.md-family CLIs: codex + copilot),
// ALL (default for ""; BOTH accepted as alias), or NONE. Devin itself never
// gets a hook file — it reads the Claude hook and AGENTS.md natively.
func HookPlatforms(choice string) (map[string]bool, error) {
	switch strings.ToUpper(strings.TrimSpace(choice)) {
	case "", "ALL", "BOTH":
		return map[string]bool{"claude": true, "codex": true, "copilot": true}, nil
	case "CLAUDE":
		return map[string]bool{"claude": true}, nil
	case "AGENTS":
		return map[string]bool{"codex": true, "copilot": true}, nil
	case "NONE":
		return map[string]bool{}, nil
	}
	return nil, fmt.Errorf("hooks %q: want CLAUDE, AGENTS, ALL, or NONE", choice)
}

// ClaudeSkillDir and AgentsSkillDir are the two standard install targets.
func ClaudeSkillDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "skills", skillName), nil
}

func AgentsSkillDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agents", "skills", skillName), nil
}

// Install writes the embedded skill into each target dir (exact replace,
// see InstallDir).
func Install(includeAgents bool) ([]string, error) {
	targets, err := Targets(includeAgents)
	if err != nil {
		return nil, err
	}
	for _, dst := range targets {
		if err := InstallDir(dst); err != nil {
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
