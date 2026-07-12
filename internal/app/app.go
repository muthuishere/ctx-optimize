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
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/analyze"
	"github.com/muthuishere/ctx-optimize/internal/dashboard"
	"github.com/muthuishere/ctx-optimize/internal/export"
	"github.com/muthuishere/ctx-optimize/internal/extract/code"
	"github.com/muthuishere/ctx-optimize/internal/extract/markdown"
	"github.com/muthuishere/ctx-optimize/internal/feedback"
	"github.com/muthuishere/ctx-optimize/internal/grammar"
	"github.com/muthuishere/ctx-optimize/internal/project"
	"github.com/muthuishere/ctx-optimize/internal/query"
	"github.com/muthuishere/ctx-optimize/internal/remote"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/skills"
	"github.com/muthuishere/ctx-optimize/internal/store"
	"github.com/muthuishere/ctx-optimize/internal/version"
	"github.com/muthuishere/ctx-optimize/internal/wiki"
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
	case "save-result":
		err = cmdSaveResult(rest, stdout)
	case "reflect":
		err = cmdReflect(rest, stdout)
	case "path":
		err = cmdPath(rest, stdout)
	case "explain":
		err = cmdExplain(rest, stdout)
	case "card":
		err = cmdCard(rest, stdout)
	case "affected":
		err = cmdAffected(rest, stdout)
	case "hubs":
		err = cmdHubs(rest, stdout)
	case "wiki":
		err = cmdWiki(rest, stdout)
	case "merge":
		err = cmdMerge(rest, stdout)
	case "export":
		err = cmdExport(rest, stdout)
	case "serve", "dashboard":
		err = cmdServe(rest, stdout)
	case "languages", "grammar": // `grammar` kept as an alias
		err = cmdLanguages(rest, stdout)
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
	path, err := resolvePath(f)
	if err != nil {
		return err
	}
	// Scaffold the committable .ctxoptimize/ (config.json + adapters/ with an
	// inert template) before opening the store, so the store key can honor a
	// pre-existing "name".
	name, err := store.ModuleKey(path)
	if err != nil {
		return err
	}
	if err := project.Scaffold(path, name); err != nil {
		return err
	}
	pointed, err := project.EnsureAgentPointer(path, name)
	if err != nil {
		return err
	}
	s, err := openStore(f)
	if err != nil {
		return err
	}
	if _, err := s.UpdateManifest(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "store ready: %s\n%s/ scaffolded — commit it (config.json + adapters/)\n", s.Dir, project.Dir)
	if len(pointed) > 0 {
		fmt.Fprintf(stdout, "agent pointer written to %s — commit these too; they make agent CLIs use the store unprompted\n", strings.Join(pointed, " + "))
	}
	return nil
}

// cmdAdd is both the built-in producer runner (`add <path>`) and the
// universal door (`add --json -` / `add --json file`): every adapter in the
// world enters here, strictly validated.
func cmdAdd(args []string, stdout io.Writer, stdin io.Reader) error {
	f := parseFlags(args)
	// A positional target IS the module: `add ~/other-repo` must open
	// other-repo's store, never Replace the cwd's graph with foreign code.
	if f.strs["path"] == "" && len(f.args) > 0 {
		f.strs["path"] = f.args[0]
	}
	s, err := openStore(f)
	if err != nil {
		return err
	}

	// The --json door UPSERTS (a one-off pipe may be partial); the gather
	// path below REPLACES per producer (a re-gather is that producer's whole
	// truth — deleted sources leave the graph, shrink-guarded by --force).
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
		n, e, err := s.Merge(&b) // Merge validates — the door fails closed
		if err != nil {
			return err
		}
		pages, err := wiki.Generate(s) // wiki-by-default: every add refreshes it
		if err != nil {
			return err
		}
		if _, err := s.UpdateManifest(); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "added %d nodes, %d edges → %s\n", n, e, s.Dir)
		fmt.Fprintf(stdout, "wiki: %d pages → %s\n", pages, filepath.Join(s.Dir, "wiki"))
		return nil
	}

	var batches []*schema.Batch
	{
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
		cb, err := code.Extract(target)
		if err != nil {
			return err
		}
		if len(cb.Nodes) > 0 {
			batches = append(batches, cb)
			fmt.Fprintf(stdout, "code: %d nodes, %d edges\n", len(cb.Nodes), len(cb.Edges))
		}

		// Adapters: scripts dropped in .ctxoptimize/adapters/ (discovered by
		// extension) plus any commands declared in config.json — a config
		// entry wins on a name clash. Each runs with stdout as a batch
		// through the same fail-closed door. This is `add` = refresh the
		// world: one command re-gathers all declared sources.
		pc, err := project.Load(target)
		if err != nil {
			return err
		}
		adapters := append([]project.Adapter{}, pc.Adapters...)
		declared := map[string]bool{}
		for _, a := range adapters {
			declared[a.Name] = true
		}
		discovered, err := project.DiscoverAdapters(target)
		if err != nil {
			return err
		}
		for _, a := range discovered {
			if !declared[a.Name] {
				adapters = append(adapters, a)
			}
		}
		for _, a := range adapters {
			b, err := runAdapter(target, a)
			if err != nil {
				return fmt.Errorf("adapter %s: %w", a.Name, err)
			}
			batches = append(batches, b)
			fmt.Fprintf(stdout, "adapter %s: %d nodes, %d edges\n", a.Name, len(b.Nodes), len(b.Edges))
		}
	}

	totalN, totalPruned := 0, 0
	for _, b := range batches {
		n, pruned, err := s.Replace(b, f.bools["force"]) // validates; fail-closed
		if err != nil {
			return err
		}
		totalN += n
		totalPruned += pruned
	}
	pages, err := wiki.Generate(s) // wiki-by-default: every add refreshes it
	if err != nil {
		return err
	}
	if _, err := s.UpdateManifest(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "added %d nodes", totalN)
	if totalPruned > 0 {
		fmt.Fprintf(stdout, ", pruned %d stale", totalPruned)
	}
	fmt.Fprintf(stdout, " → %s\n", s.Dir)
	fmt.Fprintf(stdout, "wiki: %d pages → %s\n", pages, filepath.Join(s.Dir, "wiki"))
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

// cmdSaveResult records how a store answer worked out — the agent's side of
// the learning loop. time.Now() lives HERE; the feedback package only ever
// sees injected times, so its tests stay deterministic.
func cmdSaveResult(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if f.strs["question"] == "" {
		return fmt.Errorf(`usage: ctx-optimize save-result --question Q --answer A [--type query|path|explain|affected] [--nodes "id1,id2"] [--outcome useful|dead_end|corrected] [--correction C]`)
	}
	s, err := openStore(f)
	if err != nil {
		return err
	}
	var nodes []string
	for _, n := range strings.Split(f.strs["nodes"], ",") {
		if n = strings.TrimSpace(n); n != "" {
			nodes = append(nodes, n)
		}
	}
	r := feedback.Result{
		Question:   f.strs["question"],
		Answer:     f.strs["answer"],
		Type:       f.strs["type"],
		Nodes:      nodes,
		Outcome:    f.strs["outcome"],
		Correction: f.strs["correction"],
		When:       time.Now().UTC(),
	}
	if err := feedback.Save(s, r); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "saved result (%d nodes cited) → %s\n", len(nodes), filepath.Join(s.Dir, "memory", "results.ndjson"))
	return nil
}

// cmdReflect aggregates saved results into reflections/LESSONS.md — pure
// deterministic tallying (half-life decay), no LLM anywhere.
func cmdReflect(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	s, err := openStore(f)
	if err != nil {
		return err
	}
	halfLife := 30.0
	if v, ok := f.strs["half-life-days"]; ok {
		hl, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("bad --half-life-days %q", v)
		}
		halfLife = hl
	}
	minCorr := 2
	if v, ok := f.strs["min-corroboration"]; ok {
		mc, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("bad --min-corroboration %q", v)
		}
		minCorr = mc
	}
	l, err := feedback.Reflect(s, time.Now().UTC(), halfLife, minCorr)
	if err != nil {
		return err
	}
	if f.bools["json"] {
		return emit(stdout, l)
	}
	for _, ns := range l.PreferredNodes {
		fmt.Fprintf(stdout, "prefer   %s  (score %.3f, %d useful)\n", ns.Node, ns.Score, ns.Useful)
	}
	for _, ns := range l.DeadEnds {
		fmt.Fprintf(stdout, "dead end %s  (score %.3f)\n", ns.Node, ns.Score)
	}
	for _, c := range l.Corrections {
		fmt.Fprintf(stdout, "corrected %q → %s\n", c.Question, c.Correction)
	}
	fmt.Fprintf(stdout, "%d preferred, %d dead ends, %d corrections → %s\n",
		len(l.PreferredNodes), len(l.DeadEnds), len(l.Corrections), filepath.Join(s.Dir, "reflections", "LESSONS.md"))
	return nil
}

// loadGraph is the shared read path for the analysis verbs.
func loadGraph(f *flags) ([]schema.Node, []schema.Edge, error) {
	s, err := openStore(f)
	if err != nil {
		return nil, nil, err
	}
	nodes, err := s.Nodes()
	if err != nil {
		return nil, nil, err
	}
	edges, err := s.Edges()
	if err != nil {
		return nil, nil, err
	}
	return nodes, edges, nil
}

func cmdPath(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if len(f.args) != 2 {
		return fmt.Errorf(`usage: ctx-optimize path "A" "B"`)
	}
	nodes, edges, err := loadGraph(f)
	if err != nil {
		return err
	}
	steps, err := analyze.ShortestPath(nodes, edges, f.args[0], f.args[1])
	if err != nil {
		return err
	}
	if f.bools["json"] {
		return emit(stdout, map[string]any{"steps": steps})
	}
	if len(steps) == 0 {
		fmt.Fprintln(stdout, "same node")
		return nil
	}
	fmt.Fprintln(stdout, steps[0].From)
	for _, st := range steps {
		fmt.Fprintf(stdout, "  %s %s %s\n", st.Dir, st.Relation, st.To)
	}
	return nil
}

func cmdExplain(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if len(f.args) != 1 {
		return fmt.Errorf(`usage: ctx-optimize explain "X"`)
	}
	nodes, edges, err := loadGraph(f)
	if err != nil {
		return err
	}
	ex, err := analyze.Explain(nodes, edges, f.args[0])
	if err != nil {
		return err
	}
	if f.bools["json"] {
		return emit(stdout, ex)
	}
	fmt.Fprint(stdout, analyze.RenderExplanation(ex))
	return nil
}

func cmdCard(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if len(f.args) != 1 {
		return fmt.Errorf(`usage: ctx-optimize card "X"`)
	}
	nodes, edges, err := loadGraph(f)
	if err != nil {
		return err
	}
	c, err := analyze.Card(nodes, edges, f.args[0])
	if err != nil {
		return err
	}
	c.Body = bodyHead(f.strs["path"], c.Node)
	if f.bools["json"] {
		return emit(stdout, c)
	}
	fmt.Fprint(stdout, analyze.RenderCard(c))
	return nil
}

// bodyHead returns the first lines of the node's source span, read from the
// local file at card time — the S1e finding: a card without the body forces
// the agent into a follow-up read that costs more than the whole card. The
// file is only reachable when the card is asked from (or --path points at)
// the repo; anywhere else the card silently omits the body.
const bodyHeadLines = 30
const bodyHeadBytes = 1600

func bodyHead(root string, n schema.Node) string {
	m := regexp.MustCompile(`^L(\d+)(?:-L(\d+))?$`).FindStringSubmatch(n.Location)
	if m == nil || n.Source == "" || strings.Contains(n.Source, "://") {
		return ""
	}
	start, _ := strconv.Atoi(m[1])
	end := start
	if m[2] != "" {
		end, _ = strconv.Atoi(m[2])
	}
	if root == "" {
		root = "."
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(n.Source)))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if start < 1 || start > len(lines) {
		return ""
	}
	last := min(end, len(lines))
	shown := min(last, start+bodyHeadLines-1)
	body := strings.Join(lines[start-1:shown], "\n")
	if len(body) > bodyHeadBytes {
		body = body[:bodyHeadBytes]
		if i := strings.LastIndexByte(body, '\n'); i > 0 {
			body = body[:i]
		}
	}
	if shown < last {
		body += fmt.Sprintf("\n… (%d more lines to %s)", last-shown, n.Location)
	}
	return body
}

func cmdAffected(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if len(f.args) != 1 {
		return fmt.Errorf(`usage: ctx-optimize affected "X" [--depth N] [--relation R]`)
	}
	nodes, edges, err := loadGraph(f)
	if err != nil {
		return err
	}
	depth := 2
	if v, ok := f.strs["depth"]; ok {
		if d, err := strconv.Atoi(v); err == nil {
			depth = d
		}
	}
	var relations []string
	if r, ok := f.strs["relation"]; ok {
		relations = append(relations, r)
	}
	target, impacts, err := analyze.Affected(nodes, edges, f.args[0], depth, relations)
	if err != nil {
		return err
	}
	if f.bools["json"] {
		return emit(stdout, map[string]any{"target": target, "affected": impacts})
	}
	fmt.Fprintf(stdout, "changing %s impacts %d nodes (depth %d):\n", target.Label, len(impacts), depth)
	for _, im := range impacts {
		fmt.Fprintf(stdout, "  d%d %s  [%s]  via %s on %s\n", im.Depth, im.Node.Label, im.Node.Kind, im.Via, im.DependsOn)
	}
	return nil
}

func cmdHubs(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	nodes, edges, err := loadGraph(f)
	if err != nil {
		return err
	}
	top := 10
	if v, ok := f.strs["top"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			top = n
		}
	}
	hubs := analyze.Hubs(nodes, edges, top)
	if f.bools["json"] {
		return emit(stdout, map[string]any{"hubs": hubs})
	}
	for _, h := range hubs {
		fmt.Fprintf(stdout, "%4d (%d in / %d out)  %s  [%s]  %s\n", h.In+h.Out, h.In, h.Out, h.Node.Label, h.Node.Kind, h.Node.Source)
	}
	return nil
}

// cmdWiki regenerates the deterministic markdown wiki from the graph. Every
// successful `add` already does this; the verb rebuilds on demand.
func cmdWiki(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	s, err := openStore(f)
	if err != nil {
		return err
	}
	pages, err := wiki.Generate(s)
	if err != nil {
		return err
	}
	if _, err := s.UpdateManifest(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wiki: %d pages → %s\n", pages, filepath.Join(s.Dir, "wiki"))
	return nil
}

// cmdMerge combines module stores into one merged view — the multi-module
// answer: per-module graphs stay canonical, a merged store is derived and
// re-derivable. Sources are store keys (folder names under the root) or
// repo paths.
func cmdMerge(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	into := store.SanitizeKey(f.strs["into"])
	if into == "" || len(f.args) == 0 {
		return fmt.Errorf("usage: ctx-optimize merge <module|path>... --into <name>")
	}
	root, err := store.Root(f.strs["store"])
	if err != nil {
		return err
	}
	target, err := store.Open(root, into)
	if err != nil {
		return err
	}
	totalN, totalE := 0, 0
	for _, src := range f.args {
		key := store.SanitizeKey(src)
		if fi, err := os.Stat(src); err == nil && fi.IsDir() {
			// A path resolves like openStore does: config name > basename.
			pc, err := project.Load(src)
			if err != nil {
				return err
			}
			key = store.SanitizeKey(pc.Name)
			if key == "" {
				if key, err = store.ModuleKey(src); err != nil {
					return err
				}
			}
		}
		if _, err := os.Stat(filepath.Join(root, key, "graph")); err != nil {
			return fmt.Errorf("no module %q in %s — run `ctx-optimize add` there first", key, root)
		}
		ss, err := store.Open(root, key)
		if err != nil {
			return err
		}
		nodes, err := ss.Nodes()
		if err != nil {
			return err
		}
		edges, err := ss.Edges()
		if err != nil {
			return err
		}
		// Original producer metadata survives — Merge only stamps when absent.
		n, e, err := target.Merge(&schema.Batch{Producer: "merge:" + key, Nodes: nodes, Edges: edges})
		if err != nil {
			return err
		}
		totalN += n
		totalE += e
		fmt.Fprintf(stdout, "merged %s: %d new nodes, %d new edges\n", key, n, e)
	}
	if _, err := target.UpdateManifest(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "merged → %s (%d nodes, %d edges added)\n", target.Dir, totalN, totalE)
	return nil
}

// cmdExport dumps the graph for other tools: json (default), dot (Graphviz),
// graphml (yEd/Gephi/networkx), csv (nodes.csv + edges.csv), obsidian (a
// wikilinked vault — requires --out DIR), or all (every format under --out DIR).
func cmdExport(args []string, stdout io.Writer) error {
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
	out := f.strs["out"]
	format := f.strs["format"]
	if format == "" {
		format = "json"
	}
	switch format {
	case "json", "dot", "graphml":
		var w io.Writer = stdout
		if out != "" {
			file, err := os.Create(out)
			if err != nil {
				return err
			}
			defer file.Close()
			w = file
		}
		switch format {
		case "json":
			return emit(w, map[string]any{"nodes": nodes, "edges": edges})
		case "dot":
			return writeDOT(w, nodes, edges)
		default:
			return export.GraphML(w, nodes, edges)
		}
	case "csv":
		if out == "" {
			// No --out: both tables to stdout as labeled sections.
			fmt.Fprintln(stdout, "# nodes.csv")
			var eb bytes.Buffer
			if err := export.CSV(stdout, &eb, nodes, edges); err != nil {
				return err
			}
			fmt.Fprintln(stdout, "# edges.csv")
			_, err := eb.WriteTo(stdout)
			return err
		}
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		if err := writeCSVFiles(out, nodes, edges); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote %s\nwrote %s\n", filepath.Join(out, "nodes.csv"), filepath.Join(out, "edges.csv"))
		return nil
	case "obsidian":
		if out == "" {
			return fmt.Errorf("export --format obsidian requires --out DIR (the vault directory)")
		}
		files, err := export.Obsidian(out, nodes, edges)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote %s (obsidian vault, %d files)\n", out, files)
		return nil
	case "all":
		if out == "" {
			return fmt.Errorf("export --format all requires --out DIR")
		}
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		writeArtifact := func(name string, render func(io.Writer) error) error {
			p := filepath.Join(out, name)
			file, err := os.Create(p)
			if err != nil {
				return err
			}
			defer file.Close()
			if err := render(file); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "wrote %s\n", p)
			return nil
		}
		if err := writeArtifact("graph.json", func(w io.Writer) error {
			return emit(w, map[string]any{"nodes": nodes, "edges": edges})
		}); err != nil {
			return err
		}
		if err := writeArtifact("graph.dot", func(w io.Writer) error { return writeDOT(w, nodes, edges) }); err != nil {
			return err
		}
		if err := writeArtifact("graph.graphml", func(w io.Writer) error { return export.GraphML(w, nodes, edges) }); err != nil {
			return err
		}
		if err := writeCSVFiles(out, nodes, edges); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote %s\nwrote %s\n", filepath.Join(out, "nodes.csv"), filepath.Join(out, "edges.csv"))
		vault := filepath.Join(out, "obsidian")
		files, err := export.Obsidian(vault, nodes, edges)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote %s (obsidian vault, %d files)\n", vault, files)
		return nil
	default:
		return fmt.Errorf("unknown export format %q (json | dot | graphml | csv | obsidian | all)", format)
	}
}

// writeDOT renders the Graphviz form (unchanged from the original inline export).
func writeDOT(w io.Writer, nodes []schema.Node, edges []schema.Edge) error {
	fmt.Fprintln(w, "digraph ctxoptimize {")
	fmt.Fprintln(w, "  rankdir=LR; node [shape=box];")
	for _, n := range nodes {
		fmt.Fprintf(w, "  %q [label=%q];\n", n.ID, n.Label+"\n("+n.Kind+")")
	}
	for _, e := range edges {
		fmt.Fprintf(w, "  %q -> %q [label=%q];\n", e.Source, e.Target, e.Relation)
	}
	_, err := fmt.Fprintln(w, "}")
	return err
}

// writeCSVFiles writes nodes.csv + edges.csv under dir.
func writeCSVFiles(dir string, nodes []schema.Node, edges []schema.Edge) error {
	nf, err := os.Create(filepath.Join(dir, "nodes.csv"))
	if err != nil {
		return err
	}
	defer nf.Close()
	ef, err := os.Create(filepath.Join(dir, "edges.csv"))
	if err != nil {
		return err
	}
	defer ef.Close()
	return export.CSV(nf, ef, nodes, edges)
}

// cmdServe hosts the local dashboard — embedded single-file UI + read-only
// JSON API over the store root. Localhost by default: it is a window onto
// your store, not a service (pass --host to expose deliberately, e.g. behind
// a tunnel you control).
func cmdServe(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	root, err := store.Root(f.strs["store"])
	if err != nil {
		return err
	}
	host := f.strs["host"]
	if host == "" {
		host = "127.0.0.1"
	}
	port := f.strs["port"]
	if port == "" {
		port = "4747"
	}
	addr := net.JoinHostPort(host, port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	// If a repo path was given, land the browser on that module directly.
	link := "http://" + addr + "/"
	if p := f.strs["path"]; p != "" {
		pc, _ := project.Load(p)
		key := store.SanitizeKey(pc.Name)
		if key == "" {
			key, _ = store.ModuleKey(p)
		}
		if key != "" {
			link += "?module=" + key
		}
	}
	fmt.Fprintf(stdout, "dashboard: %s  (store root: %s) — Ctrl-C to stop\n", link, root)
	return http.Serve(ln, dashboard.NewHandler(root))
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

// cmdLanguages manages language packs. add: a known name ("kotlin"), any
// tree-sitter grammar dir, or a github URL → <name>.wasm + suggested
// <name>.json in the grammars dir — no shell script, no preinstalled
// toolchain (zig auto-downloads once, sha256-verified). list: embedded
// languages + discovered packs. remove: delete a pack.
func cmdLanguages(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ctx-optimize languages <add <name|dir|github-url> [--name N] [--ref R] [--out DIR] | list | remove <name>>")
	}
	sub, rest := args[0], args[1:]
	f := parseFlags(rest)
	switch sub {
	case "add", "build": // build = the original verb, same thing
		if len(f.args) != 1 {
			return fmt.Errorf("usage: ctx-optimize languages add <name | grammar-dir | https://github.com/owner/repo> [--name N] [--ref R] [--out DIR]")
		}
		wasmPath, cfgPath, err := grammar.Build(grammar.Options{
			Source: f.args[0], Name: f.strs["name"], OutDir: f.strs["out"], Ref: f.strs["ref"],
		}, stdout)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "pack ready: %s + %s — next `ctx-optimize add` picks it up\n", wasmPath, cfgPath)
		return nil
	case "remove":
		if len(f.args) != 1 {
			return fmt.Errorf("usage: ctx-optimize languages remove <name> [--out DIR]")
		}
		dir := f.strs["out"]
		if dir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			dir = filepath.Join(home, "ctxoptimize", "grammars")
		}
		name := f.args[0]
		removed := false
		for _, file := range []string{name + ".wasm", name + ".json"} {
			p := filepath.Join(dir, file)
			if err := os.Remove(p); err == nil {
				fmt.Fprintf(stdout, "removed %s\n", p)
				removed = true
			}
		}
		if !removed {
			return fmt.Errorf("no pack %q in %s", name, dir)
		}
		return nil
	case "list":
		path, err := resolvePath(f)
		if err != nil {
			return err
		}
		packs, err := code.LoadPacks(path)
		if err != nil {
			return err
		}
		if f.bools["json"] {
			names := []string{}
			for _, l := range code.Languages {
				names = append(names, l.Name)
			}
			pnames := []map[string]string{}
			for _, p := range packs {
				pnames = append(pnames, map[string]string{"name": p.Lang.Name, "wasm": p.WasmPath})
			}
			return emit(stdout, map[string]any{"embedded": names, "packs": pnames})
		}
		fmt.Fprint(stdout, "embedded: ")
		for i, l := range code.Languages {
			if i > 0 {
				fmt.Fprint(stdout, ", ")
			}
			fmt.Fprint(stdout, l.Name)
		}
		fmt.Fprintln(stdout)
		for _, p := range packs {
			fmt.Fprintf(stdout, "pack:     %s (%s)\n", p.Lang.Name, p.WasmPath)
		}
		if len(packs) == 0 {
			fmt.Fprintln(stdout, "packs:    (none)")
		}
		known := make([]string, 0, len(grammar.KnownGrammars))
		for name := range grammar.KnownGrammars {
			known = append(known, name)
		}
		sort.Strings(known)
		fmt.Fprintf(stdout, "addable by name (`ctx-optimize languages add <name>`): %s\n", strings.Join(known, ", "))
		fmt.Fprintln(stdout, "anything else: `ctx-optimize languages add <github-url-of-tree-sitter-grammar>`")
		return nil
	default:
		return fmt.Errorf("unknown languages subcommand %q (add | list | remove)", sub)
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
  init                        scaffold .ctxoptimize/ + prepare the store (--path, default: cwd)
  add [<path>] [--json -|F]   gather built-ins + every adapter script in
                              .ctxoptimize/adapters/; re-gather prunes stale nodes
                              (--force to allow >50%% shrink); --json door upserts
  query|ask "<question>"      answer from the local store  [--budget N] [--json]
  path "A" "B"                shortest path between two nodes  [--json]
  explain "X"                 plain-language node + neighborhood  [--json]
  card "X"                    symbol card: signature, doc, location, callers,
                              callees — cite without opening the file  [--json]
  affected "X"                reverse impact: what breaks if X changes
                              [--depth N] [--relation R] [--json]
  hubs                        most-connected nodes (god nodes)  [--top N] [--json]
  wiki                        regenerate the markdown wiki in the store's wiki/
                              dir (deterministic, from nodes+edges only; every
                              add already regenerates it)
  status                      store facts  [--json]
  save-result --question Q    record how a store answer worked out
                              [--answer A] [--type query|path|explain|affected]
                              [--nodes "id1,id2"] [--outcome useful|dead_end|corrected]
                              [--correction C]
  reflect                     aggregate saved results (half-life decay) into
                              reflections/LESSONS.md  [--half-life-days N]
                              [--min-corroboration N] [--json]
  merge <module>... --into N  combine module stores into one merged view
  export [--format json|dot|graphml|csv|obsidian|all]
                              dump the graph  [--out FILE|DIR]
                              csv: --out DIR → nodes.csv + edges.csv (stdout
                              sections without); obsidian + all REQUIRE --out DIR
                              (all → graph.{json,dot,graphml} + csvs + obsidian/)
  serve|dashboard             local dashboard over the whole store
                              [--port 4747] [--host 127.0.0.1]
  languages add <name|url>    add a language: known name (kotlin, ruby, lua…)
                              or any tree-sitter grammar dir/github url —
                              compiles a drop-in pack, no toolchain to install
  languages list              embedded + packs + names addable by name
  languages remove <name>     delete a pack
  remote init <url> [--local] write remote to .ctxoptimize/config.json
                              (committable; --local = this machine's store only)
  remote push|pull            incremental sync with the configured remote (no url —
                              the config file is the single source of truth)
  install --skills            install the agent skill (~/.claude, +~/.agents with codex)
  uninstall --skills          remove the agent skill
  version                     print version

flags:  --path DIR   module the store is keyed by (default: cwd)
        --store DIR  store root (default: $CTX_OPTIMIZE_STORE or ~/ctxoptimize)

The store lives at ~/ctxoptimize/<repo-name>/ ("name" in config.json overrides).

.ctxoptimize/ (in the repo, commit it):
  config.json    {"name": "my-module",
                  "remote": {"type": "s3", "url": "s3://bucket/prefix",
                             "credentials": {"access_key_id": "${TEAM_KEY_ID}",
                                             "secret_access_key": "${TEAM_SECRET}",
                                             "region": "auto", "endpoint": "${R2_ENDPOINT}"}}}
  adapters/      drop scripts here — every .js/.py/.sh runs on add and must
                 print batch JSON to stdout (template: example.js.sample)
remote may also be a plain string URL; ${VAR} resolves from the environment
at sync time (values are never written or printed).

The binary is deterministic: no LLM, no DB, no network except your remote.
`)
}
