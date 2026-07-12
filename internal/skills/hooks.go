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
	return installPromptHook(filepath.Join(home, ".claude", "settings.json"), hookCommand)
}

// InstallCodexHook writes the same Claude-format UserPromptSubmit hook into
// ~/.codex/hooks.json — Codex adopted the identical schema and output
// contract. One difference: Codex requires the user to trust the hook once
// (`/hooks` inside codex, hash-recorded).
func InstallCodexHook() (string, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	return installPromptHook(filepath.Join(home, ".codex", "hooks.json"), hookCommand)
}

// InstallCopilotHook writes a self-contained hook file into
// ~/.copilot/hooks/ — Copilot's userPromptSubmitted cannot inject context,
// so the pointer rides sessionStart (fires once per session) with Copilot's
// own {additionalContext} contract.
func InstallCopilotHook() (string, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	p := filepath.Join(home, ".copilot", "hooks", "ctx-optimize.json")
	content := map[string]any{
		"version": 1,
		"hooks": map[string]any{
			"sessionStart": []any{map[string]any{
				"type": "command",
				"bash": hookCommand + " --format copilot",
			}},
		},
	}
	out, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return p, false, err
	}
	out = append(out, '\n')
	if old, err := os.ReadFile(p); err == nil && string(old) == string(out) {
		return p, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return p, false, err
	}
	return p, true, os.WriteFile(p, out, 0o644)
}

func installPromptHook(p, command string) (string, bool, error) {
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
		if b, _ := json.Marshal(e); strings.Contains(string(b), command) {
			return p, false, nil // already installed
		}
	}
	entries = append(entries, map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": command}},
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

// RemoveHooks reverses every hook install: filters ctx-optimize entries out
// of the Claude and Codex hook files (deleting hooks.json if it becomes
// empty) and removes the Copilot hook file. Mirrors graphify's reversible
// uninstall discipline.
func RemoveHooks() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, p := range []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".codex", "hooks.json"),
	} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		root := map[string]any{}
		if json.Unmarshal(data, &root) != nil {
			continue
		}
		hooks, _ := root["hooks"].(map[string]any)
		entries, _ := hooks["UserPromptSubmit"].([]any)
		kept := []any{}
		for _, e := range entries {
			if b, _ := json.Marshal(e); !strings.Contains(string(b), hookCommand) {
				kept = append(kept, e)
			}
		}
		if len(kept) == len(entries) {
			continue
		}
		if len(kept) == 0 {
			delete(hooks, "UserPromptSubmit")
		} else {
			hooks["UserPromptSubmit"] = kept
		}
		if len(hooks) == 0 && strings.HasSuffix(p, "hooks.json") {
			if err := os.Remove(p); err == nil {
				removed = append(removed, p)
			}
			continue
		}
		root["hooks"] = hooks
		out, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return removed, err
		}
		if err := os.WriteFile(p, append(out, '\n'), 0o644); err != nil {
			return removed, err
		}
		removed = append(removed, p)
	}
	cp := filepath.Join(home, ".copilot", "hooks", "ctx-optimize.json")
	if _, err := os.Stat(cp); err == nil {
		if err := os.Remove(cp); err == nil {
			removed = append(removed, cp)
		}
	}
	return removed, nil
}
