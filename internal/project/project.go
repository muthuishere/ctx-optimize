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

	// Per-project overrides of the machine-global settings (set via
	// `config <key> <value> --project`); empty means "inherit global".
	// Committable, so a team can pin a repo's behavior.
	Instructions string `json:"instructions,omitempty"` // CLAUDE|AGENTS|ALL|NONE
	Skills       string `json:"skills,omitempty"`       // CLAUDE|AGENTS|ALL
	Hooks        string `json:"hooks,omitempty"`        // CLAUDE|AGENTS|ALL|NONE
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

// gate is the first thing inside every pointer block: the agent must confirm
// the tool is on PATH before trusting any of the instructions. Not installed
// ⇒ the whole block is inert (read the code the normal way). This makes the
// committed CLAUDE.md/AGENTS.md safe on a teammate's machine that never ran
// `ctx-optimize install` — no failed commands, no confusion.
const pointerGate = "  <precondition>Run `command -v ctx-optimize` first. If it is NOT installed, IGNORE this entire\n" +
	"  block and answer by reading the code normally — the store is an optimization, not a requirement\n" +
	"  (install later with `npm install -g @muthuishere/ctx-optimize`, or download the binary). Everything\n" +
	"  below applies ONLY when the command exists.</precondition>\n"

func pointerBlock(name string, modules int) string {
	if modules > 0 {
		return pointerBegin + "\n" +
			"<ctx-optimize>\n" +
			pointerGate +
			"  <store>MULTI-MODULE repo, pre-built knowledge store at `~/ctxoptimize/" + name + "/` — " +
			fmt.Sprintf("one graph per module + a navigator, %d modules declared in `.ctxoptimize/config.json`.</store>\n", modules) +
			"  <use>For questions about this codebase — where is X, how does Y work, who calls Z, what breaks if I\n" +
			"  change W — use it INSTEAD of grep-and-read chains, not in addition:\n" +
			"  PICK BY INTENT — find: `ctx-optimize query \"<terms>\"` · inspect a symbol: `ctx-optimize card <symbol>` ·\n" +
			"  about to EDIT: `ctx-optimize change-plan <symbol>` (callers+impact+tests, one call) · blast radius: `ctx-optimize affected <symbol>` · connection: `ctx-optimize path <a> <b>`.\n" +
			"  Scope follows your cwd: inside a module dir answers come from that module (zero hits escalate repo-wide);\n" +
			"  at the root the navigator federates across the best-matching modules (`--modules all|a,b` to widen).\n" +
			"  Module map + hubs: `~/ctxoptimize/" + name + "/navigator.md`; unified wiki at `~/ctxoptimize/" + name + "/wiki/index.md`.\n" +
			"  Output is parsed fact with exact file:line — cite it directly, do NOT re-verify in source.</use>\n" +
			"  <no-local-store>Fresh clone with nothing at `~/ctxoptimize/" + name + "/`? If `.ctxoptimize/config.json` has a\n" +
			"  `remote`, run `ctx-optimize remote pull`; otherwise `ctx-optimize init && ctx-optimize add .` rebuilds every module store in seconds.</no-local-store>\n" +
			"</ctx-optimize>\n" +
			pointerEnd + "\n"
	}
	return pointerBegin + "\n" +
		"<ctx-optimize>\n" +
		pointerGate +
		"  <store>Pre-built knowledge store at `~/ctxoptimize/" + name + "/` (config in `.ctxoptimize/` here).</store>\n" +
		"  <use>For questions about this codebase — where is X, how does Y work, who calls Z, what breaks if I change W —\n" +
		"  use it INSTEAD of grep-and-read chains, not in addition:\n" +
		"  PICK BY INTENT — find: `ctx-optimize query \"<terms>\"` · inspect a symbol: `ctx-optimize card <symbol>` ·\n" +
		"  about to EDIT: `ctx-optimize change-plan <symbol>` (callers+impact+tests, one call) · blast radius:\n" +
		"  `ctx-optimize affected <symbol>` · `ctx-optimize path <a> <b>` · wiki at `~/ctxoptimize/" + name + "/wiki/`.\n" +
		"  Output is parsed fact with exact file:line — cite it directly, do NOT re-verify in source; open a file only\n" +
		"  when the answer needs a body the store didn't show. Exhaustive text sweeps (every literal occurrence of a\n" +
		"  string) are still grep's job.</use>\n" +
		"  <no-local-store>Fresh clone with nothing at `~/ctxoptimize/" + name + "/`? If `.ctxoptimize/config.json` has a\n" +
		"  `remote`, run `ctx-optimize remote pull`; otherwise `ctx-optimize init && ctx-optimize add .` rebuilds in seconds.</no-local-store>\n" +
		"</ctx-optimize>\n" +
		pointerEnd + "\n"
}

// PointerTargets maps the global `instructions` setting to the files init
// may touch: CLAUDE, AGENTS, ALL (default for ""; BOTH accepted as alias),
// or NONE (never touch the repo's instruction files). Anything else is
// refused — a typo must not silently fall back to writing files.
func PointerTargets(instructions string) ([]string, error) {
	switch strings.ToUpper(strings.TrimSpace(instructions)) {
	case "", "ALL", "BOTH":
		return []string{"CLAUDE.md", "AGENTS.md"}, nil
	case "CLAUDE":
		return []string{"CLAUDE.md"}, nil
	case "AGENTS":
		return []string{"AGENTS.md"}, nil
	case "NONE":
		return nil, nil
	}
	return nil, fmt.Errorf("instructions %q: want CLAUDE, AGENTS, ALL, or NONE", instructions)
}

// EnsureAgentPointer writes or refreshes the pointer block in the repo's
// agent-instruction files (targets from PointerTargets — global agents.type
// picks CLAUDE.md, AGENTS.md, or both). Existing content outside the markers
// is never touched; missing files are created with just the block. modules >
// 0 switches to the multi-module wording (navigator, scope-follows-cwd).
func EnsureAgentPointer(repo, name string, modules int, targets []string) ([]string, error) {
	block := pointerBlock(name, modules)
	var written []string
	for _, fn := range targets {
		changed, err := upsertMarkedBlock(filepath.Join(repo, fn), pointerBegin, pointerEnd, block)
		if err != nil {
			return written, err
		}
		if changed {
			written = append(written, fn)
		}
	}
	return written, nil
}

// upsertMarkedBlock writes block between begin/end markers in the file at p:
// creates the file (block only) when absent, replaces an existing marked
// region in place, or appends the block otherwise. Content outside the markers
// is never touched. Returns whether the file changed. This is the one insert
// primitive behind both the per-repo pointer and the global always-on block.
func upsertMarkedBlock(p, begin, end, block string) (bool, error) {
	data, err := os.ReadFile(p)
	switch {
	case os.IsNotExist(err):
		return true, os.WriteFile(p, []byte(block), 0o644)
	case err != nil:
		return false, err
	}
	s := string(data)
	if i := strings.Index(s, begin); i >= 0 {
		j := strings.Index(s, end)
		if j < i {
			return false, fmt.Errorf("%s: malformed ctx-optimize markers", filepath.Base(p))
		}
		s = s[:i] + strings.TrimSuffix(block, "\n") + s[j+len(end):]
	} else {
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		s += "\n" + block
	}
	if s == string(data) {
		return false, nil
	}
	return true, os.WriteFile(p, []byte(s), 0o644)
}

// removeMarkedBlock strips a marked region (and a trailing blank line) from the
// file at p if present. Missing file or missing markers is a no-op. Returns
// whether the file changed.
func removeMarkedBlock(p, begin, end string) (bool, error) {
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s := string(data)
	i := strings.Index(s, begin)
	if i < 0 {
		return false, nil
	}
	j := strings.Index(s, end)
	if j < i {
		return false, fmt.Errorf("%s: malformed ctx-optimize markers", filepath.Base(p))
	}
	out := strings.TrimRight(s[:i], "\n")
	rest := s[j+len(end):]
	if out != "" && rest != "" {
		out += "\n"
	}
	out += strings.TrimLeft(rest, "\n")
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	if out == s {
		return false, nil
	}
	return true, os.WriteFile(p, []byte(out), 0o644)
}

const globalBegin = "<!-- ctx-optimize:global:begin -->"
const globalEnd = "<!-- ctx-optimize:global:end -->"

// globalBlock is the always-on instruction written into the user's GLOBAL
// agent-instruction files (~/.claude/CLAUDE.md, ~/.codex/AGENTS.md) by
// `install`. Unlike the per-repo pointer it names no repo — it is a standing
// rule that applies across every project: use the store before grep when one
// exists, and OFFER to create the config when it doesn't. Self-gating on
// `command -v ctx-optimize`, so it is inert if the tool was later removed.
func globalBlock() string {
	return globalBegin + "\n" +
		"## ctx-optimize — knowledge graph before grep\n\n" +
		"If `command -v ctx-optimize` succeeds, then in ANY repo you work in:\n\n" +
		"- **Store present** — a `.ctxoptimize/` directory at the repo root or any parent of your cwd\n" +
		"  means a pre-built knowledge graph of that codebase already exists. Use it BEFORE any\n" +
		"  Grep/rg/Glob/find or exploratory Read — PICK BY INTENT: find → `ctx-optimize query \"<terms>\"` ·\n" +
		"  inspect a symbol → `card <symbol>` · about to EDIT → `change-plan <symbol>` (callers+impact+tests,\n" +
		"  one call) · blast radius → `affected <symbol>` · connection → `path <a> <b>`.\n" +
		"  Output is parsed fact with exact file:line — cite it directly, do NOT re-verify in source;\n" +
		"  open a file only for a body the store didn't show. Exhaustive literal-string sweeps stay grep's job.\n" +
		"- **No `.ctxoptimize/` yet** — before you start a grep-and-read chain on a real codebase, OFFER to\n" +
		"  build the graph: `ctx-optimize init && ctx-optimize add .` writes `.ctxoptimize/config.json`\n" +
		"  (commit it) and gathers the store in seconds; `ctx-optimize serve` opens the dashboard. For a\n" +
		"  monorepo, `ctx-optimize scan` first, confirm the module list, then `ctx-optimize init --scan --yes && add .`.\n" +
		"- **Not installed** (the command is missing) — ignore this block and read the code the normal way.\n" +
		globalEnd + "\n"
}

// EnsureGlobalPointer writes/refreshes the always-on block in the given global
// instruction files (absolute paths). Missing files are created. Returns the
// paths actually changed.
func EnsureGlobalPointer(targets []string) ([]string, error) {
	block := globalBlock()
	var written []string
	for _, p := range targets {
		if dir := filepath.Dir(p); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return written, err
			}
		}
		changed, err := upsertMarkedBlock(p, globalBegin, globalEnd, block)
		if err != nil {
			return written, err
		}
		if changed {
			written = append(written, p)
		}
	}
	return written, nil
}

// RemoveGlobalPointer strips the always-on block from the given global files.
func RemoveGlobalPointer(targets []string) ([]string, error) {
	var removed []string
	for _, p := range targets {
		changed, err := removeMarkedBlock(p, globalBegin, globalEnd)
		if err != nil {
			return removed, err
		}
		if changed {
			removed = append(removed, p)
		}
	}
	return removed, nil
}
