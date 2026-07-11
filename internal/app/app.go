// Package app is the CLI: a deliberately dumb front over the internal
// packages. Hand-rolled dispatch and flags (house style — no cobra), --json
// on every read command, errors to stderr as "ctx-optimize: msg". The binary
// never calls a model, a database, or the network except `remote push/pull`
// against the URL the user configured.
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/markdown"
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

// openStore resolves --path (default cwd) + --store into the module's store.
func openStore(f *flags) (*store.Store, error) {
	path := f.strs["path"]
	if path == "" {
		var err error
		path, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	root, err := store.Root(f.strs["store"])
	if err != nil {
		return nil, err
	}
	key, err := store.ModuleKey(path)
	if err != nil {
		return nil, err
	}
	return store.Open(root, key)
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
	cfg, err := s.Config()
	if err != nil {
		return err
	}
	st := map[string]any{
		"store": s.Dir, "nodes": len(nodes), "edges": len(edges), "remote": cfg.Remote,
	}
	if f.bools["json"] {
		return emit(stdout, st)
	}
	fmt.Fprintf(stdout, "store:  %s\nnodes:  %d\nedges:  %d\nremote: %s\n", s.Dir, len(nodes), len(edges), orNone(cfg.Remote))
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
	switch sub {
	case "init":
		if len(f.args) == 0 {
			return fmt.Errorf("usage: ctx-optimize remote init <s3://bucket/prefix | file:///dir>")
		}
		url := f.args[0]
		if _, err := remote.Open(url); err != nil { // validate before persisting
			return err
		}
		cfg, err := s.Config()
		if err != nil {
			return err
		}
		cfg.Remote = url
		if err := s.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "remote set: %s\n", url)
		return nil
	case "push", "pull":
		// Ad-hoc URL wins over config — one-off sync to anywhere without
		// touching the stored remote: `ctx-optimize remote push file:///mnt/x`.
		target := ""
		if len(f.args) > 0 {
			target = f.args[0]
		} else {
			cfg, err := s.Config()
			if err != nil {
				return err
			}
			target = cfg.Remote
		}
		if target == "" {
			return fmt.Errorf("no remote — run `ctx-optimize remote init <url>` or pass one: ctx-optimize remote %s <url>", sub)
		}
		b, err := remote.Open(target)
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
  add [<path>] [--json -|F]   gather sources; --json is the universal adapter door
  query|ask "<question>"      answer from the local store  [--budget N] [--json]
  status                      store facts  [--json]
  remote init <url>           configure sync remote (s3://bucket/prefix | file:///dir)
  remote push|pull [url]      incremental sync; ad-hoc url wins over config  [--json]
  install --skills            install the agent skill (~/.claude, +~/.agents with codex)
  uninstall --skills          remove the agent skill
  version                     print version

flags:  --path DIR   module the store is keyed by (default: cwd)
        --store DIR  store root (default: $CTX_OPTIMIZE_STORE or ~/.ctx-optimize/store)

The binary is deterministic: no LLM, no DB, no network except your remote.
`)
}
