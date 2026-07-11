// Package app is the CLI: a deliberately dumb front over the internal
// packages. Hand-rolled dispatch and flags (house style — no cobra), --json
// on every read command, errors to stderr as "ctx-optimize: msg". The binary
// never calls a model, a database, or the network except `remote push/pull`
// against the URL the user configured.
package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/markdown"
	"github.com/muthuishere/ctx-optimize/internal/project"
	"github.com/muthuishere/ctx-optimize/internal/query"
	"github.com/muthuishere/ctx-optimize/internal/remote"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/skills"
	"github.com/muthuishere/ctx-optimize/internal/store"
	"github.com/muthuishere/ctx-optimize/internal/version"
)

// Run dispatches args (without argv[0]) and returns the process exit code.
// stdout/stderr are injected so CLI tests capture output hermetically.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "ctx-optimize %s (%s, %s)\n", version.Version, version.Commit, version.Date)
	case "init":
		err = cmdInit(rest, stdout)
	case "add":
		err = cmdAdd(rest, stdout, os.Stdin)
	case "query", "ask": // `ask` — same verb graphify users reach for
		err = cmdQuery(rest, stdout)
	case "status":
		err = cmdStatus(rest, stdout)
	case "remote":
		err = cmdRemote(rest, stdout)
	case "install":
		err = cmdInstall(rest, stdout)
	case "uninstall":
		err = cmdUninstall(rest, stdout)
	case "help", "-h", "--help":
		usage(stdout)
	default:
		fmt.Fprintf(stderr, "ctx-optimize: unknown command %q\n\n", cmd)
		usage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "ctx-optimize: %v\n", err)
		return 1
	}
	return 0
}

// ---- flag parsing (tiny, dependency-free; house style) ----

type flags struct {
	strs  map[string]string
	bools map[string]bool
	args  []string
}

func parseFlags(args []string) *flags {
	f := &flags{strs: map[string]string{}, bools: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			f.args = append(f.args, a)
			continue
		}
		name := strings.TrimPrefix(a, "--")
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			f.strs[name[:eq]] = name[eq+1:]
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			f.strs[name] = args[i+1]
			i++
		} else {
			f.bools[name] = true
		}
	}
	return f
}

// resolvePath resolves --path (default cwd) — the module directory that both
// the store key and the repo-level ctx-optimize.json hang off.
func resolvePath(f *flags) (string, error) {
	if p := f.strs["path"]; p != "" {
		return p, nil
	}
	return os.Getwd()
}

// openStore resolves --path + --store into the module's store. The module
// key defaults to the repo basename (~/ctxoptimize/<repo-name>/); a "name" in
// ctx-optimize.json overrides it (custom modules, basename collisions).
func openStore(f *flags) (*store.Store, error) {
	path, err := resolvePath(f)
	if err != nil {
		return nil, err
	}
	root, err := store.Root(f.strs["store"])
	if err != nil {
		return nil, err
	}
	pc, err := project.Load(path)
	if err != nil {
		return nil, err
	}
	key := store.SanitizeKey(pc.Name)
	if key == "" {
		key, err = store.ModuleKey(path)
		if err != nil {
			return nil, err
		}
	}
	return store.Open(root, key)
}

// resolveRemote picks the sync target: repo ctx-optimize.json > store config
// (per-machine fallback, set via remote init --local). The returned remote is
// UNresolved — ${VAR} placeholders intact, safe to print.
func resolveRemote(repoPath string, s *store.Store) (*project.Remote, string, error) {
	pc, err := project.Load(repoPath)
	if err != nil {
		return nil, "", err
	}
	if pc.Remote != nil && (pc.Remote.URL != "" || pc.Remote.Type != "") {
		return pc.Remote, project.FileName, nil
	}
	sc, err := s.Config()
	if err != nil {
		return nil, "", err
	}
	if sc.Remote != "" {
		return &project.Remote{URL: sc.Remote}, "store config", nil
	}
	return nil, "", nil
}

// openBackend resolves ${VAR}s and opens the sync backend. Resolved values
// stay inside the remote package — nothing here prints them.
func openBackend(r *project.Remote) (remote.Backend, error) {
	rr, err := r.Resolve()
	if err != nil {
		return nil, err
	}
	c := rr.Credentials
	return remote.OpenWith(rr.URL, remote.Options{
		AccessKeyID:     c["access_key_id"],
		SecretAccessKey: c["secret_access_key"],
		SessionToken:    c["session_token"],
		Region:          c["region"],
		Endpoint:        c["endpoint"],
	})
}

// ---- commands ----

func cmdInit(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	s, err := openStore(f)
	if err != nil {
		return err
	}
	if _, err := s.UpdateManifest(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "store ready: %s\n", s.Dir)
	return nil
}

// cmdAdd is both the built-in producer runner (`add <path>`) and the
// universal door (`add --json -` / `add --json file`): every adapter in the
// world enters here, strictly validated.
func cmdAdd(args []string, stdout io.Writer, stdin io.Reader) error {
	f := parseFlags(args)
	s, err := openStore(f)
	if err != nil {
		return err
	}

	var batches []*schema.Batch
	if src, ok := f.strs["json"]; ok || f.bools["json"] {
		var data []byte
		if !ok || src == "-" {
			data, err = io.ReadAll(stdin)
		} else {
			data, err = os.ReadFile(src)
		}
		if err != nil {
			return fmt.Errorf("read batch: %w", err)
		}
		var b schema.Batch
		if err := json.Unmarshal(data, &b); err != nil {
			return fmt.Errorf("parse batch json: %w", err)
		}
		batches = append(batches, &b)
	} else {
		// Built-in tier-1 producers over the target path. Markdown today;
		// code languages (tree-sitter wasm) join here.
		target := f.strs["path"]
		if target == "" && len(f.args) > 0 {
			target = f.args[0]
		}
		if target == "" {
			target, _ = os.Getwd()
		}
		b, err := markdown.Extract(target)
		if err != nil {
			return err
		}
		batches = append(batches, b)

		// Declared adapters from the repo's ctx-optimize.json: run each
		// command, treat stdout as a batch through the same fail-closed door.
		// This is `add` = refresh the world — one command re-gathers all
		// declared sources.
		pc, err := project.Load(target)
		if err != nil {
			return err
		}
		for _, a := range pc.Adapters {
			b, err := runAdapter(target, a)
			if err != nil {
				return fmt.Errorf("adapter %s: %w", a.Name, err)
			}
			batches = append(batches, b)
			fmt.Fprintf(stdout, "adapter %s: %d nodes, %d edges\n", a.Name, len(b.Nodes), len(b.Edges))
		}
	}

	totalN, totalE := 0, 0
	for _, b := range batches {
		n, e, err := s.Merge(b) // Merge validates — the door fails closed
		if err != nil {
			return err
		}
		totalN += n
		totalE += e
	}
	if _, err := s.UpdateManifest(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "added %d nodes, %d edges → %s\n", totalN, totalE, s.Dir)
	return nil
}

// runAdapter executes a declared adapter command (cwd = the repo) and parses
// its stdout as a schema.Batch. The command is user-committed config — same
// trust model as npm scripts or a Taskfile.
func runAdapter(repo string, a project.Adapter) (*schema.Batch, error) {
	if a.Name == "" || a.Run == "" {
		return nil, fmt.Errorf("needs both name and run")
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", a.Run)
	} else {
		cmd = exec.Command("sh", "-c", a.Run)
	}
	cmd.Dir = repo
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w\n%s", err, strings.TrimSpace(errb.String()))
	}
	var b schema.Batch
	if err := json.Unmarshal(out.Bytes(), &b); err != nil {
		return nil, fmt.Errorf("stdout is not a batch: %w", err)
	}
	return &b, nil
}

func cmdQuery(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if len(f.args) == 0 {
		return fmt.Errorf("usage: ctx-optimize query \"<question>\" [--budget N] [--json]")
	}
	s, err := openStore(f)
	if err != nil {
		return err
	}
	nodes, err := s.Nodes()
	if err != nil {
		return err
	}
	edges, err := s.Edges()
	if err != nil {
		return err
	}
	budget := 2000
	if v, ok := f.strs["budget"]; ok {
		if b, err := strconv.Atoi(v); err == nil {
			budget = b
		}
	}
	res := query.Run(nodes, edges, strings.Join(f.args, " "), budget)
	if f.bools["json"] {
		return emit(stdout, res)
	}
	fmt.Fprint(stdout, query.Render(res))
	return nil
}

func cmdStatus(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	s, err := openStore(f)
	if err != nil {
		return err
	}
	nodes, err := s.Nodes()
	if err != nil {
		return err
	}
	edges, err := s.Edges()
	if err != nil {
		return err
	}
	repoPath, err := resolvePath(f)
	if err != nil {
		return err
	}
	r, remoteFrom, err := resolveRemote(repoPath, s)
	if err != nil {
		return err
	}
	remoteURL := ""
	if r != nil {
		remoteURL = r.URL // raw form: ${VAR} placeholders, never values
	}
	st := map[string]any{
		"store": s.Dir, "nodes": len(nodes), "edges": len(edges),
		"remote": remoteURL, "remote_from": remoteFrom,
	}
	if f.bools["json"] {
		return emit(stdout, st)
	}
	fmt.Fprintf(stdout, "store:  %s\nnodes:  %d\nedges:  %d\nremote: %s", s.Dir, len(nodes), len(edges), orNone(remoteURL))
	if remoteFrom != "" {
		fmt.Fprintf(stdout, "  (from %s)", remoteFrom)
	}
	fmt.Fprintln(stdout)
	return nil
}

func cmdRemote(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ctx-optimize remote <init URL|push|pull>")
	}
	sub, rest := args[0], args[1:]
	f := parseFlags(rest)
	s, err := openStore(f)
	if err != nil {
		return err
	}
	repoPath, err := resolvePath(f)
	if err != nil {
		return err
	}
	switch sub {
	case "init":
		if len(f.args) == 0 {
			return fmt.Errorf("usage: ctx-optimize remote init <s3://bucket/prefix | file:///dir>")
		}
		url := f.args[0]
		// Validate the resolved form (the raw one may hold ${VAR}s), persist
		// the raw one — placeholders belong in the file, values never do.
		if _, err := openBackend(&project.Remote{URL: url}); err != nil {
			return err
		}
		// The remote belongs to the repo, not the machine: write the
		// committable ctx-optimize.json so teammates clone → pull, done.
		// --local keeps it out of the repo (store config only).
		if f.bools["local"] {
			cfg, err := s.Config()
			if err != nil {
				return err
			}
			cfg.Remote = url
			if err := s.SaveConfig(cfg); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "remote set (this machine only): %s\n", url)
			return nil
		}
		pc, err := project.Load(repoPath)
		if err != nil {
			return err
		}
		pc.Remote = &project.Remote{URL: url}
		if err := project.Save(repoPath, pc); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "remote set: %s → %s (commit it — teammates just pull)\n", url, project.FileName)
		return nil
	case "push", "pull":
		if len(f.args) > 0 {
			return fmt.Errorf("remote %s takes no URL — the remote lives in %s (ctx-optimize remote init <url>)", sub, project.FileName)
		}
		r, _, err := resolveRemote(repoPath, s)
		if err != nil {
			return err
		}
		if r == nil {
			return fmt.Errorf("no remote — run `ctx-optimize remote init <url>` (writes %s)", project.FileName)
		}
		b, err := openBackend(r)
		if err != nil {
			return err
		}
		var res *remote.Result
		if sub == "push" {
			res, err = remote.Push(s, b)
		} else {
			res, err = remote.Pull(s, b)
		}
		if err != nil {
			return err
		}
		if f.bools["json"] {
			return emit(stdout, res)
		}
		fmt.Fprintf(stdout, "%s: %d transferred, %d unchanged\n", sub, len(res.Transferred), res.Skipped)
		return nil
	default:
		return fmt.Errorf("unknown remote subcommand %q", sub)
	}
}

func cmdInstall(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if !f.bools["skills"] {
		return fmt.Errorf("usage: ctx-optimize install --skills [--agents]")
	}
	targets, err := skills.Install(f.bools["agents"])
	if err != nil {
		return err
	}
	for _, t := range targets {
		fmt.Fprintf(stdout, "installed skill: %s\n", t)
	}
	return nil
}

func cmdUninstall(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if !f.bools["skills"] {
		return fmt.Errorf("usage: ctx-optimize uninstall --skills")
	}
	removed, err := skills.Uninstall()
	if err != nil {
		return err
	}
	for _, t := range removed {
		fmt.Fprintf(stdout, "removed skill: %s\n", t)
	}
	return nil
}

// ---- helpers ----

func emit(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func usage(w io.Writer) {
	fmt.Fprint(w, `ctx-optimize — gather once, refresh cheaply, answer from the store.

usage: ctx-optimize <command> [flags]

commands:
  init                        prepare the store for --path (default: cwd)
  add [<path>] [--json -|F]   gather built-ins + adapters from ctx-optimize.json;
                              --json is the universal adapter door
  query|ask "<question>"      answer from the local store  [--budget N] [--json]
  status                      store facts  [--json]
  remote init <url> [--local] write remote to the repo's ctx-optimize.json
                              (committable; --local = this machine's store only)
  remote push|pull            incremental sync with the configured remote
  install --skills            install the agent skill (~/.claude, +~/.agents with codex)
  uninstall --skills          remove the agent skill
  version                     print version

flags:  --path DIR   module the store is keyed by (default: cwd)
        --store DIR  store root (default: $CTX_OPTIMIZE_STORE or ~/ctxoptimize)

The store lives at ~/ctxoptimize/<repo-name>/ (name: in ctx-optimize.json
overrides the folder name).

ctx-optimize.json (in the repo, commit it):
  {"name": "my-module",
   "remote": {"type": "s3", "url": "s3://bucket/prefix",
              "credentials": {"access_key_id": "${TEAM_KEY_ID}",
                              "secret_access_key": "${TEAM_SECRET}",
                              "region": "auto", "endpoint": "${R2_ENDPOINT}"}},
   "adapters": [{"name": "kafka", "run": "node hooks/kafka.js"}]}
remote may also be a plain string URL; ${VAR} resolves from the environment
at sync time (values are never written or printed). Adapter commands print
batch JSON to stdout; add runs them all.

The binary is deterministic: no LLM, no DB, no network except your remote.
`)
}
