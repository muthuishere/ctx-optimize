// Package project reads/writes ctx-optimize.json — the ONE file that lives in
// the repo itself. It is committable on purpose: the remote (URL + credential
// NAMES) and the adapter commands travel with the code, so a teammate clones,
// runs `ctx-optimize remote pull`, and the world is there. Nothing else ever
// touches the repo.
//
// Secrets never appear in the file: credentials are ${VAR} placeholders that
// resolve from the environment at call time. The resolved values exist only
// in memory for the duration of the sync — never written, never printed.
package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const FileName = "ctx-optimize.json"

// Adapter is a declared producer: any command whose stdout is a schema.Batch.
// Templates are just scripts ("node hooks/kafka.js", "python3 hooks/pg.py") —
// we run them, validate the JSON, and merge; we never interpret them. The
// command runs through the shell, so $VAR/${VAR} expand there naturally.
type Adapter struct {
	Name string `json:"name"`
	Run  string `json:"run"`
}

// Remote is the sync target. In JSON it is either a plain string
// ("s3://bucket/prefix") or an object with explicit credentials:
//
//	{"type": "s3", "url": "s3://bucket/${REPO}",
//	 "credentials": {"access_key_id": "${TEAM_KEY_ID}",
//	                 "secret_access_key": "${TEAM_SECRET}",
//	                 "region": "auto", "endpoint": "${R2_ENDPOINT}"}}
//
// Credential keys: access_key_id, secret_access_key, session_token, region,
// endpoint. Anything omitted falls back to the standard AWS_* env vars.
type Remote struct {
	Type        string            `json:"type,omitempty"`
	URL         string            `json:"url,omitempty"`
	Credentials map[string]string `json:"credentials,omitempty"`
}

func (r *Remote) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &r.URL)
	}
	type alias Remote // shed methods to avoid recursion
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = Remote(a)
	return nil
}

func (r Remote) MarshalJSON() ([]byte, error) {
	if r.Type == "" && len(r.Credentials) == 0 {
		return json.Marshal(r.URL) // keep the simple form simple
	}
	type alias Remote
	return json.Marshal(alias(r))
}

var placeholderRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Resolve returns a copy with every ${VAR} expanded from the environment.
// Unset/empty variables are an error naming the VARIABLES (never values) so a
// missing credential fails loudly instead of signing garbage. The resolved
// copy is for immediate use only — never save or print it.
func (r Remote) Resolve() (Remote, error) {
	missing := map[string]bool{}
	exp := func(s string) string {
		return placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
			name := m[2 : len(m)-1]
			v := os.Getenv(name)
			if v == "" {
				missing[name] = true
			}
			return v
		})
	}
	out := Remote{Type: r.Type, URL: exp(r.URL)}
	if len(r.Credentials) > 0 {
		out.Credentials = make(map[string]string, len(r.Credentials))
		for k, v := range r.Credentials {
			out.Credentials[k] = exp(v)
		}
	}
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for n := range missing {
			names = append(names, n)
		}
		sort.Strings(names)
		return Remote{}, fmt.Errorf("remote config references unset environment variables: %s", strings.Join(names, ", "))
	}
	return out, nil
}

type Config struct {
	// Name overrides the store module key (default: repo basename) — the
	// folder under ~/ctxoptimize/. Use it for custom module names or when two
	// repos share a basename.
	Name     string    `json:"name,omitempty"`
	Remote   *Remote   `json:"remote,omitempty"`
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
