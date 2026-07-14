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
// anywhere.
type GlobalConfig struct {
	Agents struct {
		// Type picks which agent-instruction files `init` writes the pointer
		// block into: AGENTS, CLAUDE, or BOTH (default when unset).
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
	return &c, nil
}

// SaveGlobalConfig writes the config pretty-printed (git-diffable house
// style, even though this file lives outside any repo).
func SaveGlobalConfig(root string, c *GlobalConfig) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(globalConfigPath(root), append(data, '\n'), 0o644)
}
