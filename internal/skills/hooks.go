package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstallClaudeHook wires `ctx-optimize hook-context` into Claude Code's
// UserPromptSubmit hooks (~/.claude/settings.json) — the one CLI of the
// supported set with a real hook API. The hook fires once per prompt, before
// any tool runs, and prints a store pointer only when the cwd repo has a
// populated store; everywhere else it is silent. The merge is surgical:
// existing settings and hooks are preserved byte-for-byte at the JSON level,
// and a second install is a no-op.
const hookCommand = "ctx-optimize hook-context"

func InstallClaudeHook() (string, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	p := filepath.Join(home, ".claude", "settings.json")
	settings := map[string]any{}
	if data, err := os.ReadFile(p); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return p, false, fmt.Errorf("parse %s: %w (not touching it)", p, err)
		}
	} else if !os.IsNotExist(err) {
		return p, false, err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	entries, _ := hooks["UserPromptSubmit"].([]any)
	for _, e := range entries {
		if b, _ := json.Marshal(e); strings.Contains(string(b), hookCommand) {
			return p, false, nil // already installed
		}
	}
	entries = append(entries, map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": hookCommand}},
	})
	hooks["UserPromptSubmit"] = entries
	settings["hooks"] = hooks
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return p, false, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return p, false, err
	}
	tmp := p + ".ctxoptimize.tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o644); err != nil {
		return p, false, err
	}
	return p, true, os.Rename(tmp, p)
}
