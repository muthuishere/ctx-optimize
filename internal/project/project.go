// Package project reads/writes ctx-optimize.json — the ONE file that lives in
// the repo itself. It is committable on purpose: the remote URL and the
// adapter commands travel with the code, so a teammate clones, runs
// `ctx-optimize remote pull`, and the world is there. Nothing else ever
// touches the repo.
package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const FileName = "ctx-optimize.json"

// Adapter is a declared producer: any command whose stdout is a schema.Batch.
// Templates are just scripts ("node hooks/kafka.js", "python3 hooks/pg.py") —
// we run them, validate the JSON, and merge; we never interpret them.
type Adapter struct {
	Name string `json:"name"`
	Run  string `json:"run"`
}

type Config struct {
	Remote   string    `json:"remote,omitempty"`
	Adapters []Adapter `json:"adapters,omitempty"`
}

func path(repo string) string { return filepath.Join(repo, FileName) }

// Load reads the repo's ctx-optimize.json (absent → empty config, not an error).
func Load(repo string) (*Config, error) {
	data, err := os.ReadFile(path(repo))
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", FileName, err)
	}
	return &c, nil
}

// Save writes the config back, pretty-printed and newline-terminated so the
// committed file stays git-friendly.
func Save(repo string, c *Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(repo), append(data, '\n'), 0o644)
}
