package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// GlobalConfig is the machine-level config at <store root>/config.json —
// settings that belong to the user's setup, not to any one repo. Repos keep
// their own committed .ctxoptimize/config.json; this file is never committed
// anywhere. Keys are flat artifact nouns; values name who gets the artifact.
type GlobalConfig struct {
	// Instructions picks which agent-instruction files `init` writes the
	// pointer block into: CLAUDE, AGENTS, ALL (default), or NONE.
	Instructions string `json:"instructions,omitempty"`
	// Skills picks which skill dirs `install --skills` writes:
	// CLAUDE (~/.claude/skills), AGENTS (~/.agents/skills), or ALL (default).
	Skills string `json:"skills,omitempty"`

	// Legacy v0.2.6 shape ({"agents":{"type":...}}) — read-only alias,
	// never written back.
	LegacyAgents *struct {
		Type string `json:"type,omitempty"`
	} `json:"agents,omitempty"`
}

func globalConfigPath(root string) string { return filepath.Join(root, "config.json") }

// LoadGlobalConfig reads <root>/config.json; absent → zero config, not an
// error (defaults apply).
func LoadGlobalConfig(root string) (*GlobalConfig, error) {
	data, err := os.ReadFile(globalConfigPath(root))
	if os.IsNotExist(err) {
		return &GlobalConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c GlobalConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse global config: %w", err)
	}
	if c.Instructions == "" && c.LegacyAgents != nil {
		c.Instructions = c.LegacyAgents.Type
	}
	c.LegacyAgents = nil
	return &c, nil
}

// SaveGlobalConfig writes the config pretty-printed (git-diffable house
// style, even though this file lives outside any repo).
func SaveGlobalConfig(root string, c *GlobalConfig) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	c.LegacyAgents = nil
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(globalConfigPath(root), append(data, '\n'), 0o644)
}
