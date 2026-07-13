// Package project reads/writes the .ctxoptimize/ directory — the ONE thing
// that lives in the repo itself. It is committable on purpose: config.json
// carries the remote (URL + credential NAMES) and adapters/ carries the
// adapter scripts, so a teammate clones, runs `ctx-optimize remote pull`, and
// the world is there. Nothing else ever touches the repo.
//
//	.ctxoptimize/
//	  config.json      name, remote (string or {type,url,credentials})
//	  adapters/        drop scripts here — `add` runs every *.js/*.py/*.sh
//
// Secrets never appear in the files: credentials are ${VAR} placeholders that
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

	"github.com/muthuishere/ctx-optimize/internal/scan"
)

const (
	Dir         = ".ctxoptimize"
	FileName    = Dir + "/config.json"
	AdaptersDir = Dir + "/adapters"
)

// Adapter is a declared producer: any command whose stdout is a schema.Batch.
// Adapters are just scripts ("node .ctxoptimize/adapters/kafka.js") — we run
// them, validate the JSON, and merge; we never interpret them. The command
// runs through the shell, so $VAR/${VAR} expand there naturally.
type Adapter struct {
	Name string `json:"name"`
	Run  string `json:"run"`
}

// runnerByExt maps adapter script extensions to their interpreter. Files in
// adapters/ with any other extension (README.md, *.sample, data files) are
// inert — rename to .js/.py/.sh to arm one.
var runnerByExt = map[string]string{
	".js":  "node",
	".mjs": "node",
	".py":  "python3",
	".sh":  "sh",
}

// DiscoverAdapters lists the runnable scripts in .ctxoptimize/adapters/,
// sorted by name. "They can change and do files there": dropping a script in
// IS the registration — no config entry needed.
func DiscoverAdapters(repo string) ([]Adapter, error) {
	entries, err := os.ReadDir(filepath.Join(repo, AdaptersDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Adapter
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		runner, ok := runnerByExt[strings.ToLower(filepath.Ext(e.Name()))]
		if !ok {
			continue
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		out = append(out, Adapter{
			Name: name,
			Run:  runner + " " + AdaptersDir + "/" + e.Name(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
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

	// Modules is the generated, owned module list of a multi-module root
	// (written by `init --scan`, hand-editable; globs allowed). Present ⇒
	// this config is a ROOT: `add` fans out and read verbs resolve scope
	// against it.
	Modules []scan.Module `json:"modules,omitempty"`
	// ModuleOf marks an OPT-IN child config inside a module dir: the value
	// is the root store key. The upward walk stops here (self-describing
	// module); most modules have no config at all.
	ModuleOf string `json:"module_of,omitempty"`
	// Scan tunes the generator (`scan` / `init --scan`): depth, extra
	// markers, include/exclude globs.
	Scan *scan.Options `json:"scan,omitempty"`
}

func path(repo string) string { return filepath.Join(repo, filepath.FromSlash(FileName)) }

// Load reads .ctxoptimize/config.json (absent → empty config, not an error).
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
// committed file stays git-friendly. Creates .ctxoptimize/ if needed.
func Save(repo string, c *Config) error {
	if err := os.MkdirAll(filepath.Join(repo, Dir), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(repo), append(data, '\n'), 0o644)
}

const adapterTemplate = `#!/usr/bin/env node
// ctx-optimize adapter template. Rename to <name>.js to arm it — every
// .js/.py/.sh in this directory runs on ` + "`ctx-optimize add`" + `.
// Print ONE batch (nodes + edges) to stdout; anything invalid is rejected
// whole. Secrets: read process.env, never hardcode values.
const batch = {
  producer: "example",
  nodes: [
    // {id: "kafka://orders", label: "orders", kind: "topic",
    //  file_type: "messaging", source: "kafka://orders"}
  ],
  edges: [
    // {source: "kafka://orders", target: "svc://billing",
    //  relation: "consumed_by", confidence: "EXTRACTED"}
  ],
};
console.log(JSON.stringify(batch));
`

// Scaffold creates the .ctxoptimize/ layout in the repo: config.json (with
// the module name) and adapters/ seeded with an inert template. Existing
// files are never overwritten.
func Scaffold(repo, name string) error {
	if err := os.MkdirAll(filepath.Join(repo, filepath.FromSlash(AdaptersDir)), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path(repo)); os.IsNotExist(err) {
		if err := Save(repo, &Config{Name: name}); err != nil {
			return err
		}
	}
	tmpl := filepath.Join(repo, filepath.FromSlash(AdaptersDir), "example.js.sample")
	if _, err := os.Stat(tmpl); os.IsNotExist(err) {
		return os.WriteFile(tmpl, []byte(adapterTemplate), 0o644)
	}
	return nil
}

// Agent-pointer block: the one mechanism measured to make agents actually
// use the store unprompted (proof S16 addendum: skill alone fired 0/2 times
// headless; skill + this line in the repo's agent-instructions file fired
// immediately). CLAUDE.md is read by Claude Code; AGENTS.md by Codex,
// Copilot, OpenCode and Devin. The block is marker-fenced and idempotent.
const pointerBegin = "<!-- ctx-optimize:begin -->"
const pointerEnd = "<!-- ctx-optimize:end -->"

func pointerBlock(name string) string {
	return pointerBegin + "\n" +
		"This repo has a pre-built ctx-optimize knowledge store (`.ctxoptimize/` here, data at `~/ctxoptimize/" + name + "/`).\n" +
		"For questions about this codebase — where is X, how does Y work, who calls Z, what breaks if I change W —\n" +
		"use it INSTEAD of grep-and-read chains, not in addition to them:\n" +
		"`ctx-optimize query \"<terms>\"` · `ctx-optimize card <symbol>` (signature+doc+callers+callees) ·\n" +
		"`ctx-optimize affected <symbol>` · `ctx-optimize path <a> <b>` · wiki at `~/ctxoptimize/" + name + "/wiki/`.\n" +
		"Card/query output is parsed fact with exact file:line — cite it directly, do NOT re-verify in source;\n" +
		"open a file only when the answer needs a body the store didn't show. Exhaustive text sweeps\n" +
		"(every literal occurrence of a string) are still grep's job. Fresh clone? `ctx-optimize init && ctx-optimize add .` rebuilds in seconds.\n" +
		pointerEnd + "\n"
}

// EnsureAgentPointer writes or refreshes the pointer block in the repo's
// CLAUDE.md and AGENTS.md. Existing content outside the markers is never
// touched; missing files are created with just the block.
func EnsureAgentPointer(repo, name string) ([]string, error) {
	block := pointerBlock(name)
	var written []string
	for _, fn := range []string{"CLAUDE.md", "AGENTS.md"} {
		p := filepath.Join(repo, fn)
		data, err := os.ReadFile(p)
		switch {
		case os.IsNotExist(err):
			if err := os.WriteFile(p, []byte(block), 0o644); err != nil {
				return written, err
			}
		case err != nil:
			return written, err
		default:
			s := string(data)
			if i := strings.Index(s, pointerBegin); i >= 0 {
				j := strings.Index(s, pointerEnd)
				if j < i {
					return written, fmt.Errorf("%s: malformed ctx-optimize markers", fn)
				}
				s = s[:i] + strings.TrimSuffix(block, "\n") + s[j+len(pointerEnd):]
			} else {
				if !strings.HasSuffix(s, "\n") {
					s += "\n"
				}
				s += "\n" + block
			}
			if s == string(data) {
				continue
			}
			if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
				return written, err
			}
		}
		written = append(written, fn)
	}
	return written, nil
}
