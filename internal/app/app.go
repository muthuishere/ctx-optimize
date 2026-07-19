// Package app is the CLI: a deliberately dumb front over the internal
// packages. Hand-rolled dispatch and flags (house style — no cobra), --json
// on every read command, errors to stderr as "ctx-optimize: msg". The binary
// never calls a model, a database, or the network — except the three moments
// the user explicitly invokes: `remote push/pull` against the configured
// URL, `grammar build`'s one-time zig download, and `update`'s release
// check/download. Nothing network-shaped ever runs unasked.
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
	"github.com/muthuishere/ctx-optimize/internal/audit"
	"github.com/muthuishere/ctx-optimize/internal/dashboard"
	"github.com/muthuishere/ctx-optimize/internal/export"
	"github.com/muthuishere/ctx-optimize/internal/extract/code"
	"github.com/muthuishere/ctx-optimize/internal/feedback"
	"github.com/muthuishere/ctx-optimize/internal/freshness"
	"github.com/muthuishere/ctx-optimize/internal/grammar"
	"github.com/muthuishere/ctx-optimize/internal/project"
	"github.com/muthuishere/ctx-optimize/internal/query"
	"github.com/muthuishere/ctx-optimize/internal/scan"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/selfupdate"
	"github.com/muthuishere/ctx-optimize/internal/skills"
	"github.com/muthuishere/ctx-optimize/internal/sources"
	"github.com/muthuishere/ctx-optimize/internal/store"
	metrics "github.com/muthuishere/ctx-optimize/internal/usage"
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
	// Main binary carries zero driver imports: real connector dials exec the
	// ctx-optimize-adapters companion. In-process registrations (test stubs,
	// the companion's own build) always win over the bridge.
	sources.ArmExecBridge()
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "ctx-optimize %s (%s, %s)\n", version.Version, version.Commit, version.Date)
	case "init":
		err = cmdInit(rest, stdout)
	case "scan":
		err = cmdScan(rest, stdout)
	case "add":
		err = cmdAdd(rest, stdout, os.Stdin)
	case "up":
		err = cmdUp(rest, stdout)
	case "sync": // fast lane: `add .` minus adapter scripts — always the repo you're in
		if f := parseFlags(rest); len(f.args) > 0 {
			err = fmt.Errorf("sync takes no path — it always syncs the repo you're in (use `add <path>` for another repo)")
		} else {
			err = cmdAdd(append(rest, ".", "--no-adapters"), stdout, os.Stdin)
		}
	case "capture": // one source connector → Batch JSON on stdout, no store write
		err = cmdCapture(rest, stdout)
	case "adapters":
		err = cmdAdapters(rest, stdout)
	case "query", "ask": // `ask` — same verb graphify users reach for
		err = cmdQuery(rest, stdout)
	case "status":
		err = cmdStatus(rest, stdout)
	case "fresh":
		return cmdFresh(rest, stdout, stderr)
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
	case "verify":
		err = cmdVerify(rest, stdout)
	case "affected":
		err = cmdAffected(rest, stdout)
	case "change-plan", "plan":
		err = cmdChangePlan(rest, stdout)
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
	case "routes":
		err = cmdRoutes(rest, stdout)
	case "manifests":
		err = cmdManifests(rest, stdout)
	case "remote":
		err = cmdRemote(rest, stdout)
	case "config":
		err = cmdConfig(rest, stdout)
	case "log":
		err = cmdLog(rest, stdout)
	case "install":
		err = cmdInstall(rest, stdout)
	case "hook-context":
		err = cmdHookContext(rest, stdout)
	case "update":
		err = cmdUpdate(rest, stdout)
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

// ---- commands ----

// countingWriter measures bytes served so usage analytics reflect actual
// output volume (est tokens = bytes/4, same heuristic as the query budget).
type countingWriter struct {
	w io.Writer
	n int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += n
	return n, err
}

// served records one answered read verb into the store's usage metrics.
// Fail-silent by design: analytics never break an answer.
func served(s *store.Store, verb, arg string, hits int, cw *countingWriter, t0 time.Time) {
	if s == nil {
		return
	}
	metrics.Record(s.Dir, metrics.Event{
		Verb: verb, Arg: arg, Hits: hits, Bytes: cw.n,
		MS: time.Since(t0).Milliseconds(),
	})
}

// cmdUp is THE front door (ADR 2026-07-16-up-verb, amended): make a usable
// store exist and be current, by whatever means, idempotently —
// docker-compose `up` semantics. Every decision a newcomer would otherwise
// have to make (is this repo even set up? did the team publish a store, or
// do I build one?) is made by the tool:
//
//	no config anywhere            → bootstrap: init (monorepo → --scan --yes) + full gather
//	no store + remote.pull        → run the pull (fall back to gather)
//	no store, no pull             → gather from source (full add)
//	store stale vs git HEAD       → fast re-gather (sync lane)
//	store fresh                   → no-op
//	freshness unknown (no git)    → report present, touch nothing
//
// `init` stays for authors who want control (--scan review,
// --instructions, module curation); `up` reports what it decided so the
// written config stays inspectable.
func cmdUp(args []string, stdout io.Writer) error {
	if err := upCore(args, stdout); err != nil {
		return err
	}
	// The committed usage card follows the binary: refresh the managed block
	// in .ctxoptimize/instructions.md (upgrade-only; user text outside the
	// markers untouched) — one person upgrades + commits, the whole team's
	// agents upgrade. Bootstrap already wrote it via init's Scaffold; this
	// covers the existing-config lanes.
	if sc, err := resolveScope(parseFlags(args)); err == nil && sc.cfg != nil {
		changed, err := project.EnsureInstructions(sc.rootDir)
		if err != nil {
			return err
		}
		if changed {
			fmt.Fprintf(stdout, "%s refreshed — commit it (teammates' agents read it)\n", project.InstructionsFile)
		}
	}
	// Sources are the slow lane, run AFTER whatever the core decided (ADR
	// 2026-07-17): recorded env-var sources re-capture under the 24h TTL
	// rule (--sources=always|never), plus the reconcile report. A repo with
	// no sources declared adds zero cost here.
	return upSources(args, stdout)
}

func upCore(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	sc, err := resolveScope(f)
	if err != nil {
		return err
	}
	// Bootstrap lane: nothing is set up yet — author the config, then fall
	// through to the gather below (the store is necessarily empty).
	if sc.cfg == nil {
		repoPath, err := resolvePath(f)
		if err != nil {
			return err
		}
		initArgs := []string{"--path", repoPath}
		if st := f.strs["store"]; st != "" {
			initArgs = append(initArgs, "--store", st)
		}
		res, err := scan.Scan(repoPath, scan.Options{})
		if err == nil && len(res.Modules) > 0 {
			fmt.Fprintf(stdout, "no config — bootstrapping as a MULTI-MODULE repo (%d modules found; edit .ctxoptimize/config.json to curate):\n", len(res.Modules))
			initArgs = append(initArgs, "--scan", "--yes")
		} else {
			fmt.Fprintln(stdout, "no config — bootstrapping:")
		}
		if err := cmdInit(initArgs, stdout); err != nil {
			return err
		}
		if sc, err = resolveScope(f); err != nil {
			return err
		}
	}
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return err
	}
	// Flag pass-through for the gather lanes.
	pass := []string{}
	if p := f.strs["path"]; p != "" {
		pass = append(pass, "--path", p)
	}
	if st := f.strs["store"]; st != "" {
		pass = append(pass, "--store", st)
	}
	if f.bools["force"] {
		pass = append(pass, "--force")
	}
	if j := f.strs["jobs"]; j != "" {
		pass = append(pass, "--jobs", j)
	}
	s, err := store.Open(storeRoot, sc.storeKey)
	if err != nil {
		return err
	}
	nodes, err := s.Nodes()
	if err != nil {
		return err
	}
	// Multi-module root: reconcile the DECLARED module set against what is
	// on disk (ADR 2026-07-19-config-reconciliation). The root store's node
	// count gates nothing here — a broken/empty residual must re-gather the
	// residual, never trigger a full fan-out while populated module stores
	// sit on disk. A module just added to config (committed or not) is
	// simply a missing store and gets gathered on this run.
	if sc.kind == scopeRoot && len(sc.modules) > 0 {
		tasks, err := planTasks(sc.rootDir, sc.rootKey, sc.modules, map[string]bool{})
		if err != nil {
			return err
		}
		var missing []gatherTask
		for _, t := range tasks {
			ts, err := store.Open(storeRoot, t.storeKey)
			if err != nil {
				return err
			}
			tn, err := ts.Nodes()
			if err != nil {
				return err
			}
			if len(tn) == 0 {
				missing = append(missing, t)
			}
		}
		if len(missing) > 0 && len(missing) < len(tasks) {
			fmt.Fprintf(stdout, "up: %d of %d declared stores missing — gathering only those:\n", len(missing), len(tasks))
			for _, t := range missing {
				ts, err := store.Open(storeRoot, t.storeKey)
				if err != nil {
					return err
				}
				fmt.Fprintf(stdout, "== %s\n", t.label)
				if err := gatherInto(ts, t.base, t.dirs, t.excludes, f.bools["force"] || t.residual, f.bools["no-adapters"], stdout); err != nil {
					return err
				}
			}
			if err := writeNavigator(sc, storeRoot, tasks, stdout); err != nil {
				return err
			}
			fmt.Fprintln(stdout, "up: store ready — missing modules gathered")
			return nil
		}
		// All missing → the bootstrap lane below (pull, else full gather).
		// None missing → the freshness lane below. Both key off the ROOT
		// store here only because reconcile proved it representative.
		if len(missing) == len(tasks) {
			nodes = nil
		}
	}
	if len(nodes) == 0 {
		pc, err := project.Load(sc.rootDir)
		if err != nil {
			return err
		}
		if pull := pc.RemoteCommand("pull"); pull != "" {
			fmt.Fprintln(stdout, "no local store — pulling the team's prebuilt graph:")
			if err := runSyncCommand("pull", pull, sc, storeRoot, stdout); err != nil {
				fmt.Fprintf(stdout, "pull failed (%v) — gathering from source instead\n", err)
			} else if pulled, err := s.Nodes(); err == nil && len(pulled) > 0 {
				fmt.Fprintln(stdout, "up: store ready (pulled)")
				return nil
			} else {
				fmt.Fprintln(stdout, "pull landed no store content — gathering from source instead")
			}
		} else {
			fmt.Fprintln(stdout, "no local store and no remote.pull declared — gathering from source:")
		}
		if err := cmdAdd(append(pass, "."), stdout, strings.NewReader("")); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "up: store ready — try: ctx-optimize query \"<2-4 terms>\"")
		return nil
	}
	reports, overall := freshnessReports(s)
	switch overall {
	case freshness.Fresh:
		fmt.Fprintln(stdout, "up: store ready — up to date with git HEAD")
		return nil
	case freshness.Unknown:
		fmt.Fprintf(stdout, "up: store present (%d nodes; freshness unknown — no git provenance). `ctx-optimize sync` to force a refresh\n", len(nodes))
		return nil
	}
	fmt.Fprintf(stdout, "store is stale (%s) — fast re-gather, adapter scripts skipped:\n", freshnessLine(reports, overall))
	if err := cmdAdd(append(pass, ".", "--no-adapters"), stdout, strings.NewReader("")); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "up: store refreshed")
	return nil
}

func cmdInit(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	path, err := resolvePath(f)
	if err != nil {
		return err
	}
	// Adoption rule: plain init inside a dir an ancestor root already
	// declares as a module joins that root's world (child config with
	// module_of, mirrored store) — never a shadow store keyed by basename.
	if !f.bools["scan"] {
		if adopted, err := adoptIfDeclaredModule(f, path, stdout); err != nil || adopted {
			return err
		}
	}
	// Clone case: the repo already carries a COMMITTED config.json with a
	// remote.pull command, and nothing is on disk yet. Authoring already
	// happened — init has no job here; `up` is the get-me-a-store verb
	// (pull, with a gather fallback). Redirect instead of silently
	// rebuilding. `--force` skips this for a local rebuild.
	if !f.bools["scan"] && !f.bools["force"] {
		if _, statErr := os.Stat(filepath.Join(path, filepath.FromSlash(project.FileName))); statErr == nil {
			cfg, err := project.Load(path)
			if err != nil {
				return err
			}
			if cfg.RemoteCommand("pull") != "" {
				if s, err := openStore(f); err == nil {
					if nodes, err := s.Nodes(); err == nil && len(nodes) == 0 {
						fmt.Fprintln(stdout, "already configured (remote.pull declared) with no local store — nothing to init.\nrun: ctx-optimize up   (pulls the team's prebuilt graph; falls back to a local gather)")
						return nil
					}
				}
			}
		}
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
	if f.bools["scan"] {
		if err := initScan(f, path, stdout); err != nil {
			return err
		}
	}
	// Re-load: init --scan may just have written modules[] — the pointer
	// block and the store key both follow the final config.
	cfg, err := project.Load(path)
	if err != nil {
		return err
	}
	if cfg.Name != "" {
		name = store.SanitizeKey(cfg.Name)
	}
	// Which instruction files to touch: project setting first (committed,
	// team-pinned), then the machine-global config, then ALL.
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return err
	}
	gcfg, err := store.LoadGlobalConfig(storeRoot)
	if err != nil {
		return err
	}
	instr := cfg.Instructions
	if instr == "" {
		instr = gcfg.Instructions
	}
	// --instructions CLAUDE|AGENTS|ALL|NONE overrides for this init AND
	// persists into the project config (committed), so the choice sticks —
	// accepted loosely ("agents.md" → AGENTS) but validated loudly: a typo
	// must never silently fall back to writing both files.
	if v, ok := f.strs["instructions"]; ok {
		norm := strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(v)), ".MD")
		if _, err := project.PointerTargets(norm); err != nil {
			return fmt.Errorf("--instructions %q: want CLAUDE, AGENTS, ALL, or NONE (agents.md/claude.md accepted)", v)
		}
		instr = norm
		if cfg.Instructions != norm {
			cfg.Instructions = norm
			if err := project.Save(path, cfg); err != nil {
				return err
			}
		}
	}
	targets, err := project.PointerTargetsFor(path, instr)
	if err != nil {
		return err
	}
	pointed, err := project.EnsureAgentPointer(path, name, len(cfg.Modules), targets)
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
	fmt.Fprintf(stdout, "store ready: %s\n%s/ scaffolded — commit it (config.json + instructions.md + adapters/)\n", s.Dir, project.Dir)
	// The scaffolded .ctxoptimize/.gitignore covers .env* going forward, but
	// a file committed BEFORE the ignore stays tracked (the index wins) —
	// detect and say so loudly.
	warnTrackedEnv(path, stdout)
	if len(pointed) > 0 {
		fmt.Fprintf(stdout, "agent pointer written to %s — commit these too; they make agent CLIs use the store unprompted\n", strings.Join(pointed, " + "))
	} else if len(targets) == 0 {
		fmt.Fprintln(stdout, "instructions = NONE — no CLAUDE.md/AGENTS.md touched")
	} else {
		// Re-init with identical content: say so explicitly — the files were
		// NOT rewritten (mtime untouched), which is the idempotency promise.
		fmt.Fprintf(stdout, "agent pointer already current in %s — nothing rewritten\n", strings.Join(targets, " + "))
	}
	return nil
}

// orAll renders an empty settings choice as its default for messages.
func orAll(v string) string {
	if v == "" {
		return "ALL"
	}
	return v
}

// cmdConfig gets/sets settings at two levels, git-style: machine-global
// (~/ctxoptimize/config.json, the default) and per-project with --project
// (.ctxoptimize/config.json, committable — a team can pin a repo's
// behavior). Project overrides global. Keys are flat artifact nouns, values
// name who gets the artifact:
//
//	instructions = CLAUDE | AGENTS | ALL | NONE  (files init writes)
//	skills       = CLAUDE | AGENTS | ALL         (dirs install --skills writes)
//	hooks        = CLAUDE | AGENTS | ALL | NONE  (platform hook files install writes)
//
// Meant to be scripted (an npm install step can run
// `ctx-optimize config instructions CLAUDE`).
func cmdConfig(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return err
	}
	gcfg, err := store.LoadGlobalConfig(storeRoot)
	if err != nil {
		return err
	}
	// Nearest project config, git-style upward walk from --path/cwd.
	sc, err := resolveScope(f)
	if err != nil {
		return err
	}
	pcfg := sc.cfg
	if pcfg == nil {
		pcfg = &project.Config{}
	}
	type key struct {
		validate func(string) error
		glob     *string
		proj     *string
	}
	keys := map[string]key{
		"instructions": {
			validate: func(v string) error { _, err := project.PointerTargets(v); return err },
			glob:     &gcfg.Instructions, proj: &pcfg.Instructions,
		},
		"skills": {
			validate: func(v string) error { _, err := skills.SkillTargets(v); return err },
			glob:     &gcfg.Skills, proj: &pcfg.Skills,
		},
		"hooks": {
			validate: func(v string) error { _, err := skills.HookPlatforms(v); return err },
			glob:     &gcfg.Hooks, proj: &pcfg.Hooks,
		},
	}
	keyNames := []string{"instructions", "skills", "hooks"}
	toProject := f.bools["project"]
	effective := func(k key) (string, string) { // value, source
		if *k.proj != "" {
			return *k.proj, "project"
		}
		if *k.glob != "" {
			return *k.glob, "global"
		}
		return "ALL", "default"
	}
	switch len(f.args) {
	case 0: // list
		for _, name := range keyNames {
			v, src := effective(keys[name])
			if src == "default" {
				fmt.Fprintf(stdout, "%s = %s\n", name, v)
			} else {
				fmt.Fprintf(stdout, "%s = %s  (%s)\n", name, v, src)
			}
		}
		fmt.Fprintf(stdout, "global: %s\n", filepath.Join(storeRoot, "config.json"))
		if sc.cfg != nil {
			fmt.Fprintf(stdout, "project: %s\n", filepath.Join(sc.rootDir, filepath.FromSlash(project.FileName)))
		}
		return nil
	case 1, 2:
		k, ok := keys[f.args[0]]
		if !ok {
			return fmt.Errorf("unknown config key %q — keys: %s", f.args[0], strings.Join(keyNames, ", "))
		}
		if len(f.args) == 1 {
			v, _ := effective(k)
			fmt.Fprintln(stdout, v)
			return nil
		}
		v := strings.ToUpper(strings.TrimSpace(f.args[1]))
		if err := k.validate(v); err != nil {
			return err
		}
		if toProject {
			file := filepath.Join(sc.rootDir, filepath.FromSlash(project.FileName))
			before := audit.FileHash(file)
			*k.proj = v
			if err := project.Save(sc.rootDir, pcfg); err != nil {
				return err
			}
			// Same writer the dashboard uses: every mutation door logs.
			audit.Append(storeRoot, audit.Line{Actor: "cli",
				Action: "config.set " + f.args[0] + "=" + v, Target: file,
				BeforeHash: before, AfterHash: audit.FileHash(file)})
			fmt.Fprintf(stdout, "%s = %s  (project: %s — commit it)\n", f.args[0], v, file)
			return nil
		}
		file := filepath.Join(storeRoot, "config.json")
		before := audit.FileHash(file)
		*k.glob = v
		if err := store.SaveGlobalConfig(storeRoot, gcfg); err != nil {
			return err
		}
		audit.Append(storeRoot, audit.Line{Actor: "cli",
			Action: "config.set " + f.args[0] + "=" + v, Target: file,
			BeforeHash: before, AfterHash: audit.FileHash(file)})
		fmt.Fprintf(stdout, "%s = %s\n", f.args[0], v)
		return nil
	}
	return fmt.Errorf("usage: config [<key> [<value>]] [--project] — keys: %s", strings.Join(keyNames, ", "))
}

// cmdAdd is both the built-in producer runner (`add <path>`) and the
// universal door (`add --json -` / `add --json file`): every adapter in the
// world enters here, strictly validated.
func cmdAdd(args []string, stdout io.Writer, stdin io.Reader) error {
	f := parseFlags(args)
	// Source lane (ADR 2026-07-17, H2): a var-name-shaped positional
	// (^[A-Z_][A-Z0-9_]*$) is a SOURCE — resolve the env var, dial its URL,
	// capture, merge, record. Anything else keeps today's dir semantics.
	if _, jsonDoor := f.strs["json"]; !jsonDoor && !f.bools["json"] &&
		len(f.args) == 1 && sources.IsEnvName(f.args[0]) {
		return cmdAddSource(f, f.args[0], stdout)
	}
	// A positional target IS the module: `add ~/other-repo` must open
	// other-repo's store, never Replace the cwd's graph with foreign code.
	if f.strs["path"] == "" && len(f.args) > 0 {
		f.strs["path"] = f.args[0]
	}
	sc, err := resolveScope(f)
	if err != nil {
		return err
	}
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return err
	}

	// The --json door UPSERTS (a one-off pipe may be partial); the gather
	// path below REPLACES per producer (a re-gather is that producer's whole
	// truth — deleted sources leave the graph, shrink-guarded by --force).
	// The door targets the SCOPE's store: piped from a module dir, the batch
	// lands in that module's mirrored store.
	if src, ok := f.strs["json"]; ok || f.bools["json"] {
		s, err := store.Open(storeRoot, sc.storeKey)
		if err != nil {
			return err
		}
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

	// Multi-module root: fan out (one worker per module, navigator refresh).
	if sc.kind == scopeRoot && len(sc.modules) > 0 {
		return runMultiAdd(sc, f, stdout)
	}

	// Module scope: gather the WHOLE module dir (asking from a subdir still
	// refreshes the module, not a shadow store keyed by the subdir).
	// Single scope: gather the config's dir (or the asked dir when no config
	// exists anywhere — today's behavior).
	// base is the rel-path root for node IDs; dirs are the folders to walk.
	// A multi-path module (ADR 2026-07-14) gathers ALL its scattered dirs
	// into the one store, base==rootDir; a single-path module or single repo
	// gathers one dir.
	base := sc.rootDir
	dirs := []string{sc.rootDir}
	if sc.kind == scopeModule && sc.mod != nil && sc.mod.Multi() {
		dirs = nil
		for _, p := range sc.mod.Dirs() {
			dirs = append(dirs, filepath.Join(sc.rootDir, filepath.FromSlash(p)))
		}
	} else if sc.kind == scopeModule {
		base = filepath.Join(sc.rootDir, filepath.FromSlash(sc.modulePath))
		dirs = []string{base}
	}
	s, err := store.Open(storeRoot, sc.storeKey)
	if err != nil {
		return err
	}
	if err := gatherInto(s, base, dirs, nil, f.bools["force"], f.bools["no-adapters"], stdout); err != nil {
		return err
	}
	if sc.kind == scopeModule {
		if err := refreshNavigatorEntry(sc, storeRoot); err != nil {
			return err
		}
	}
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

// cmdAdapters is the slow lane split out of the gather: `sync` skips adapter
// scripts, this verb runs them on demand (all, or one by name). Replace stays
// producer-scoped, so running one adapter never disturbs the code graph.
func cmdAdapters(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	sub := "list"
	if len(f.args) > 0 {
		sub = f.args[0]
	}
	sc, err := resolveScope(f)
	if err != nil {
		return err
	}
	base := sc.rootDir
	adapters, err := repoAdapters(base)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		if len(adapters) == 0 {
			fmt.Fprintln(stdout, "no adapters — drop .js/.py/.sh scripts in .ctxoptimize/adapters/ (see example.js.sample)")
		}
		for _, a := range adapters {
			fmt.Fprintf(stdout, "%s\t%s\n", a.Name, a.Run)
		}
		// Native sources ride the same catalog: recorded entries + the
		// scheme set the built-in connectors route.
		pc, err := project.Load(sc.rootDir)
		if err != nil {
			return err
		}
		listSources(pc, stdout)
		return nil
	case "help": // setup card, generated from the connector's own Params()
		if len(f.args) < 2 {
			return fmt.Errorf("usage: ctx-optimize adapters help <scheme> — schemes: %s", strings.Join(sources.SupportedSchemes(), " "))
		}
		card, err := sources.HelpCard(f.args[1])
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, card)
		return nil
	case "run":
		name := ""
		if len(f.args) > 1 {
			name = f.args[1]
		}
		storeRoot, err := store.Root(f.strs["store"])
		if err != nil {
			return err
		}
		s, err := store.Open(storeRoot, sc.storeKey)
		if err != nil {
			return err
		}
		ran := 0
		for _, a := range adapters {
			if name != "" && a.Name != name {
				continue
			}
			ab, err := runAdapter(base, a)
			if err != nil {
				return fmt.Errorf("adapter %s: %w", a.Name, err)
			}
			if _, _, err := s.Replace(ab, f.bools["force"]); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "adapter %s: %d nodes, %d edges\n", a.Name, len(ab.Nodes), len(ab.Edges))
			ran++
		}
		if ran == 0 {
			if name != "" {
				return fmt.Errorf("no adapter named %q — `ctx-optimize adapters list` shows what exists", name)
			}
			fmt.Fprintln(stdout, "no adapters — drop .js/.py/.sh scripts in .ctxoptimize/adapters/ (see example.js.sample)")
			return nil
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
	return fmt.Errorf("usage: ctx-optimize adapters <list | run [name] | help <scheme>>")
}

func cmdQuery(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if len(f.args) == 0 {
		return fmt.Errorf("usage: ctx-optimize query \"<question>\" [--budget N] [--json] [--modules all|a,b] [--root]")
	}
	sc, err := resolveScope(f)
	if err != nil {
		return err
	}
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return err
	}
	budget := 2000
	if v, ok := f.strs["budget"]; ok {
		if b, err := strconv.Atoi(v); err == nil {
			budget = b
		}
	}
	q := strings.Join(f.args, " ")
	t0 := time.Now()
	cw := &countingWriter{w: stdout}

	// Single scope: today's behavior, byte-identical.
	if sc.kind == scopeSingle {
		s, err := store.Open(storeRoot, sc.storeKey)
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
		res := query.Run(nodes, edges, q, budget)
		defer func() { served(s, "query", q, len(res.Hits), cw, t0) }()
		if f.bools["json"] {
			return emit(cw, res)
		}
		fmt.Fprint(cw, query.Render(res))
		return nil
	}

	// Module scope: innermost first — the module's own store; zero hits (or
	// --root) escalates to root federation. Every block labels its scope.
	if sc.kind == scopeModule && !f.bools["root"] {
		s, err := store.Open(storeRoot, sc.storeKey)
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
		res := query.Run(nodes, edges, q, budget)
		if len(res.Hits) > 0 {
			defer func() { served(s, "query", q, len(res.Hits), cw, t0) }()
			if f.bools["json"] {
				return emit(cw, map[string]any{"scope": sc.moduleName, "result": res})
			}
			fmt.Fprintf(cw, "[%s]\n", sc.moduleName)
			fmt.Fprint(cw, query.Render(res))
			return nil
		}
		fmt.Fprintf(cw, "[%s] no hits — escalating to root\n", sc.moduleName)
	}
	return federatedQuery(sc, storeRoot, f, q, budget, cw, t0)
}

// federatedQuery answers from the multi-module root: EVERY module store (+
// the root residual) concatenated into one namespaced pass — graphify's
// one-graph-one-search simplicity, kept because it's cheap (beam's 310
// modules / 188k nodes load + rank in ~0.6s). No ranking gate, no widen
// dance; --modules a,b narrows explicitly when the user wants less.
func federatedQuery(sc *scope, storeRoot string, f *flags, q string, budget int, cw *countingWriter, t0 time.Time) error {
	if len(sc.modules) == 0 {
		mods, err := expandRootModules(sc)
		if err != nil {
			return err
		}
		sc.modules = mods
	}
	mods := sc.modules
	scopeLabel := "root: all modules"
	if v, ok := f.strs["modules"]; ok && v != "all" {
		want := map[string]bool{}
		for _, p := range strings.Split(v, ",") {
			want[strings.Trim(strings.TrimSpace(p), "/")] = true
		}
		var narrowed []scan.Module
		for _, m := range sc.modules {
			if want[m.KeySeg()] || want[moduleLabel(m.Name, m.KeySeg())] {
				narrowed = append(narrowed, m)
			}
		}
		if len(narrowed) == 0 {
			return fmt.Errorf("--modules %q matched nothing; declared: %s", v, modulePaths(sc.modules))
		}
		mods = narrowed
		scopeLabel = "root: " + modulePaths(mods)
	}
	nodes, edges, err := loadFederated(sc, storeRoot, mods)
	if err != nil {
		return err
	}
	res := query.Run(nodes, edges, q, budget)
	if rs, err := store.Open(storeRoot, sc.rootKey); err == nil {
		defer func() { served(rs, "query", q, len(res.Hits), cw, t0) }()
	}
	if f.bools["json"] {
		return emit(cw, map[string]any{"scope": scopeLabel, "result": res})
	}
	fmt.Fprintf(cw, "[%s]\n", scopeLabel)
	fmt.Fprint(cw, query.Render(res))
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
	pc, err := project.Load(repoPath)
	if err != nil {
		return err
	}
	remoteLine := ""
	switch {
	case pc.RemoteCommand("push") != "" && pc.RemoteCommand("pull") != "":
		remoteLine = "push + pull declared"
	case pc.RemoteCommand("push") != "":
		remoteLine = "push declared (no pull)"
	case pc.RemoteCommand("pull") != "":
		remoteLine = "pull declared (no push)"
	}
	reports, overall := freshnessReports(s)
	st := map[string]any{
		"store": s.Dir, "nodes": len(nodes), "edges": len(edges),
		"remote":    remoteLine,
		"freshness": reports, "fresh": string(overall),
	}
	if stamps, err := sources.SourceStamps(s.Dir); err == nil && len(stamps) > 0 {
		st["sources"] = stamps // id → last-captured unix (sanitized ids only)
	}
	sum, sumErr := metrics.Summarize(s.Dir)
	if sumErr == nil && sum.Total > 0 {
		st["served"] = sum
	}
	if f.bools["json"] {
		return emit(stdout, st)
	}
	fmt.Fprintf(stdout, "store:  %s\nnodes:  %d\nedges:  %d\nremote: %s", s.Dir, len(nodes), len(edges), orNone(remoteLine))
	fmt.Fprintf(stdout, "\nfresh:  %s\n", freshnessLine(reports, overall))
	if line := sourcesStatusLine(s.Dir, time.Now()); line != "" {
		fmt.Fprintf(stdout, "sources: %s\n", line)
	}
	// The money line: what answering from the store (instead of a
	// grep-and-read chain) has saved so far, per the usage estimator.
	if sumErr == nil && sum.Total > 0 {
		fmt.Fprintf(stdout, "served: %d answers · ~%d tokens saved (~$%.2f)\n", sum.Total, sum.EstSaved, sum.EstUSD)
	}
	return nil
}

// freshnessReports evaluates every recorded source against its repo's CURRENT
// git HEAD. Pure comparison in internal/freshness; the git read is best-effort
// here (a moved/removed repo yields an unknown, never an error).
func freshnessReports(s *store.Store) ([]freshness.Report, freshness.State) {
	srcs, err := s.Sources()
	if err != nil || len(srcs) == 0 {
		return nil, freshness.Unknown
	}
	now := time.Now().Unix()
	reports := make([]freshness.Report, 0, len(srcs))
	for _, src := range srcs {
		curHead, curUnix, _ := gitHead(src.Path)
		reports = append(reports, freshness.Evaluate(src, curHead, curUnix, now))
	}
	return reports, freshness.Overall(reports)
}

// freshnessLine renders a one-line human verdict for status / fresh.
func freshnessLine(reports []freshness.Report, overall freshness.State) string {
	switch overall {
	case freshness.Fresh:
		return "✓ up to date with git HEAD"
	case freshness.Unknown:
		if len(reports) == 0 {
			return "(unknown — no git provenance; run `add` in a git repo to enable)"
		}
		return "(unknown — source repo not found)"
	default: // stale
		for _, r := range reports {
			if r.State == freshness.Stale {
				return fmt.Sprintf("✗ STALE — store at %s, repo now at %s; run: ctx-optimize add .",
					shortSHA(r.StoreHead), shortSHA(r.CurrentHead))
			}
		}
		return "✗ STALE"
	}
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	if s == "" {
		return "?"
	}
	return s
}

// cmdFresh is the agent/hook gate: is the store still current with git HEAD?
// It only READS the store (never creates dirs) and exits 0 fresh / 1 stale /
// 2 unknown so a hook can decide whether to re-add before trusting an answer.
func cmdFresh(args []string, stdout, stderr io.Writer) int {
	f := parseFlags(args)
	s, err := openStore(f)
	if err != nil {
		fmt.Fprintf(stderr, "ctx-optimize: %v\n", err)
		return 2
	}
	reports, overall := freshnessReports(s)
	if f.bools["json"] {
		if err := emit(stdout, map[string]any{"fresh": string(overall), "freshness": reports}); err != nil {
			fmt.Fprintf(stderr, "ctx-optimize: %v\n", err)
			return 2
		}
	} else {
		fmt.Fprintln(stdout, freshnessLine(reports, overall))
	}
	return freshness.ExitCode(overall)
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

// loadGraph is the shared read path for the analysis verbs. Scope-aware:
// single → that store (unchanged); module → the module's mirrored store;
// multi-module root → the federated concat of every module + the residual
// (namespaced, collision-free), so path/affected/explain see the whole repo.
func loadGraph(f *flags) ([]schema.Node, []schema.Edge, error) {
	nodes, edges, _, _, err := loadGraphScoped(f)
	return nodes, edges, err
}

func loadGraphScoped(f *flags) ([]schema.Node, []schema.Edge, *scope, string, error) {
	sc, err := resolveScope(f)
	if err != nil {
		return nil, nil, nil, "", err
	}
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return nil, nil, nil, "", err
	}
	// --root from inside a module: answer repo-wide (what the boundary note
	// tells users to do), same federated graph the root scope gets.
	if sc.kind == scopeModule && f.bools["root"] {
		nodes, edges, err := federatedAll(sc, storeRoot)
		if err != nil {
			return nil, nil, nil, "", err
		}
		sc.kind = scopeRoot
		sc.storeKey = sc.rootKey
		return nodes, edges, sc, storeRoot, nil
	}
	if sc.kind == scopeRoot && len(sc.modules) > 0 {
		nodes, edges, err := loadFederated(sc, storeRoot, nil)
		return nodes, edges, sc, storeRoot, err
	}
	s, err := store.Open(storeRoot, sc.storeKey)
	if err != nil {
		return nil, nil, nil, "", err
	}
	nodes, err := s.Nodes()
	if err != nil {
		return nil, nil, nil, "", err
	}
	edges, err := s.Edges()
	if err != nil {
		return nil, nil, nil, "", err
	}
	return nodes, edges, sc, storeRoot, nil
}

func cmdPath(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if len(f.args) != 2 {
		return fmt.Errorf(`usage: ctx-optimize path "A" "B" [--root]`)
	}
	nodes, edges, sc, storeRoot, err := loadGraphScoped(f)
	if err != nil {
		return err
	}
	t0 := time.Now()
	cw := &countingWriter{w: stdout}
	stdout = cw
	st, _ := openStore(f)
	defer func() { served(st, "path", strings.Join(f.args, " → "), 1, cw, t0) }()
	steps, perr := analyze.ShortestPath(nodes, edges, f.args[0], f.args[1])
	scopeNote := ""
	// Module-scope miss (an endpoint isn't local): retry repo-wide, labeled.
	if perr != nil && sc != nil && sc.kind == scopeModule {
		if fn, fe, ferr := federatedAll(sc, storeRoot); ferr == nil {
			if s2, err2 := analyze.ShortestPath(fn, fe, f.args[0], f.args[1]); err2 == nil {
				scopeNote = fmt.Sprintf("[not in %s — answered repo-wide]", sc.moduleName)
				steps, perr = s2, nil
				sc = nil // repo-wide now: the boundary note no longer applies
			}
		}
	}
	if perr != nil {
		return perr
	}
	note := ""
	if sc != nil && sc.kind == scopeModule &&
		(crossModuleEcho(sc, storeRoot, f.args[0]) || crossModuleEcho(sc, storeRoot, f.args[1])) {
		note = boundaryNote
	}
	if f.bools["json"] {
		out := map[string]any{"steps": steps}
		if note != "" {
			out["note"] = note
		}
		if scopeNote != "" {
			out["scope"] = scopeNote
		}
		return emit(stdout, out)
	}
	if scopeNote != "" {
		fmt.Fprintln(stdout, scopeNote)
	}
	if len(steps) == 0 {
		fmt.Fprintln(stdout, "same node")
		return nil
	}
	fmt.Fprintln(stdout, steps[0].From)
	for _, st := range steps {
		fmt.Fprintf(stdout, "  %s %s %s\n", st.Dir, st.Relation, st.To)
	}
	if note != "" {
		fmt.Fprintln(stdout, note)
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
	t0 := time.Now()
	cw := &countingWriter{w: stdout}
	stdout = cw
	st, _ := openStore(f)
	defer func() { served(st, "explain", f.args[0], 1, cw, t0) }()
	ex, err := analyze.Explain(nodes, edges, f.args[0])
	if id, ok := fuzzyPick(err, f); ok {
		if ex, err = analyze.Explain(nodes, edges, id); err == nil {
			ex.ResolvedVia = "fuzzy" // --fuzzy took a candidate: stay labeled
		}
	}
	if err != nil {
		return err
	}
	if f.bools["json"] {
		return emit(stdout, ex)
	}
	if ex.ResolvedVia == "fuzzy" || ex.ResolvedVia == "last-segment" {
		fmt.Fprintf(stdout, "[resolved via %s → %s]\n", ex.ResolvedVia, ex.Node.ID)
	}
	fmt.Fprint(stdout, analyze.RenderExplanation(ex))
	return nil
}

func cmdCard(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if len(f.args) != 1 {
		return fmt.Errorf(`usage: ctx-optimize card "X"`)
	}
	nodes, edges, sc, storeRoot, err := loadGraphScoped(f)
	if err != nil {
		return err
	}
	t0 := time.Now()
	cw := &countingWriter{w: stdout}
	c, cerr := analyze.Card(nodes, edges, f.args[0])
	if id, ok := fuzzyPick(cerr, f); ok {
		if c, cerr = analyze.Card(nodes, edges, id); cerr == nil {
			c.ResolvedVia = "fuzzy" // --fuzzy took a candidate: stay labeled
		}
	}
	bodyRoot := f.strs["path"]
	if bodyRoot == "" && sc != nil {
		switch sc.kind {
		case scopeModule: // module sources are module-relative
			bodyRoot = filepath.Join(sc.rootDir, filepath.FromSlash(sc.modulePath))
		case scopeRoot: // federated sources are namespaced repo-relative
			bodyRoot = sc.rootDir
		}
	}
	// Module-scope miss: don't fail — the symbol likely lives in a sibling
	// module. Retry against the federated root graph and say where it was.
	if cerr != nil && sc != nil && sc.kind == scopeModule {
		if len(sc.modules) == 0 {
			if sc.modules, err = expandRootModules(sc); err != nil {
				return cerr
			}
		}
		fn, fe, ferr := loadFederated(sc, storeRoot, nil)
		if ferr == nil {
			if fc, ferr2 := analyze.Card(fn, fe, f.args[0]); ferr2 == nil {
				owner := moduleOwnerOf(sc, fc.Node.Source)
				fmt.Fprintf(cw, "[not in %s — found in %s]\n", sc.moduleName, owner)
				c, cerr = fc, nil
				bodyRoot = sc.rootDir // namespaced sources resolve from the repo root
			}
		}
	}
	if cerr != nil {
		return cerr
	}
	c.Body = bodyHead(bodyRoot, c.Node)
	st, _ := openStore(f) // path resolution only — cheap; nil is fine
	defer func() { served(st, "card", f.args[0], 1, cw, t0) }()
	if f.bools["json"] {
		return emit(cw, c)
	}
	if c.ResolvedVia == "fuzzy" || c.ResolvedVia == "last-segment" {
		fmt.Fprintf(cw, "[resolved via %s → %s]\n", c.ResolvedVia, c.Node.ID)
	}
	fmt.Fprint(cw, analyze.RenderCard(c))
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

// cmdHookContext is the generic agent-hook entry point: harness hooks (e.g.
// Claude Code UserPromptSubmit) run it once per prompt; it prints a short
// store pointer ONLY when the cwd repo carries .ctxoptimize/ and its store
// has nodes — otherwise it stays silent and costs nothing. Deterministic,
// no flags, safe to wire into any hook system that captures stdout.
func cmdHookContext(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	// Scope-aware like every read verb: the upward walk finds the repo's
	// config from any subdir, and the message matches where the prompt is —
	// a module dir points at that module's graph, a multi-module root points
	// at the navigator.
	sc, err := resolveScope(f)
	if err != nil || sc.cfg == nil {
		return nil // hooks must never fail the prompt; no config → silent
	}
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return nil
	}
	s, err := store.Open(storeRoot, sc.storeKey)
	if err != nil {
		return nil
	}
	nodes, err := s.Nodes()
	if err != nil {
		return nil
	}
	var msg string
	switch {
	case sc.kind == scopeModule:
		if len(nodes) == 0 {
			return nil
		}
		msg = fmt.Sprintf("You are inside module %q of a multi-module repo with a pre-built ctx-optimize knowledge store (%d nodes for this module). Use it INSTEAD of grep-and-read. PICK BY INTENT — find something: `ctx-optimize query \"<2-4 terms>\"` · inspect a known symbol (sig/doc/callers, no file read): `ctx-optimize card <symbol>` · ABOUT TO EDIT a symbol (one call = callers + blast radius + which tests to run): `ctx-optimize change-plan <symbol>` · blast radius only: `ctx-optimize affected <symbol>`. Answers are scoped to this module; zero hits auto-escalate repo-wide (`--root` forces it). Output is parsed fact with file:line — cite it directly. TOOL CHOICE: symbols/structure/callers → store verbs; exact literal strings, config VALUES, comments, member fields → grep directly (the store does not index those — say so and grep). Two store misses = switch tools, not words. When the answer depends on BEHAVIOR, read the cited range — that is the point of the location. Before a human acts on a citation: `ctx-optimize verify \"<label or file:L10-L20>\"`.", sc.moduleName, len(nodes))
	case sc.kind == scopeRoot && len(sc.modules) > 0:
		total := len(nodes)
		count := 0
		for _, m := range sc.modules {
			ms, err := store.Open(storeRoot, store.SanitizeKeyPath(sc.rootKey+"/"+m.KeySeg()))
			if err != nil {
				continue
			}
			if mn, err := ms.Nodes(); err == nil && len(mn) > 0 {
				total += len(mn)
				count++
			}
		}
		if total == 0 {
			return nil
		}
		msg = fmt.Sprintf("This is a multi-module repo with a pre-built ctx-optimize knowledge store: %d modules, %d nodes total, plus a navigator (module map + hubs at `~/ctxoptimize/%s/navigator.md`). Use it INSTEAD of grep-and-read. PICK BY INTENT — find something: `ctx-optimize query \"<2-4 terms>\"` · inspect a known symbol (sig/doc/callers, no file read): `ctx-optimize card <symbol>` · ABOUT TO EDIT a symbol (one call = callers + blast radius + which tests to run): `ctx-optimize change-plan <symbol>` · blast radius only: `ctx-optimize affected <symbol>`. From the root, query federates across the best-matching modules; run inside a module dir to scope to it. Output is parsed fact with file:line — cite it directly. TOOL CHOICE: symbols/structure/callers → store verbs; exact literal strings, config VALUES, comments, member fields → grep directly (the store does not index those — say so and grep). Two store misses = switch tools, not words. When the answer depends on BEHAVIOR, read the cited range — that is the point of the location. Before a human acts on a citation: `ctx-optimize verify \"<label or file:L10-L20>\"`.", count, total, sc.rootKey)
	default:
		if len(nodes) == 0 {
			return nil
		}
		msg = fmt.Sprintf("This repo has a pre-built ctx-optimize knowledge store (%d nodes). Use it INSTEAD of grep-and-read. PICK BY INTENT — find something: `ctx-optimize query \"<2-4 terms>\"` · inspect a known symbol (sig/doc/callers, no file read): `ctx-optimize card <symbol>` · ABOUT TO EDIT a symbol (one call = callers + blast radius + which tests to run): `ctx-optimize change-plan <symbol>` · blast radius only: `ctx-optimize affected <symbol>`. Output is parsed fact with file:line — cite it directly; open files only for what the store lacks. TOOL CHOICE: symbols/structure/callers → store verbs; exact literal strings, config VALUES, comments, member fields → grep directly (the store does not index those — say so and grep). Two store misses = switch tools, not words. When the answer depends on BEHAVIOR, read the cited range — that is the point of the location. Before a human acts on a citation: `ctx-optimize verify \"<label or file:L10-L20>\"`.", len(nodes))
	}
	t0 := time.Now()
	cw := &countingWriter{w: stdout}
	stdout = cw
	defer func() { served(s, "hook-context", "", 1, cw, t0) }()
	// Two wire formats: the Claude hook contract (also understood by Codex
	// and Devin — the ecosystem converged on it) and Copilot's sessionStart
	// contract. Plain text is Claude-only, so JSON is the default.
	switch f.strs["format"] {
	case "copilot":
		return emit(stdout, map[string]string{"additionalContext": msg})
	case "text":
		fmt.Fprintln(stdout, msg)
		return nil
	default:
		return emit(stdout, map[string]any{
			"hookSpecificOutput": map[string]string{
				"hookEventName":     "UserPromptSubmit",
				"additionalContext": msg,
			},
		})
	}
}

func cmdAffected(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if len(f.args) != 1 {
		return fmt.Errorf(`usage: ctx-optimize affected "X" [--depth N] [--relation R] [--root]`)
	}
	nodes, edges, sc, storeRoot, err := loadGraphScoped(f)
	if err != nil {
		return err
	}
	t0 := time.Now()
	cw := &countingWriter{w: stdout}
	stdout = cw
	st, _ := openStore(f)
	defer func() { served(st, "affected", f.args[0], 1, cw, t0) }()
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
	target, impacts, aerr := analyze.Affected(nodes, edges, f.args[0], depth, relations)
	if id, ok := fuzzyPick(aerr, f); ok {
		target, impacts, aerr = analyze.Affected(nodes, edges, id, depth, relations)
	}
	scopeNote := ""
	// Module-scope miss: the symbol likely lives in a sibling module —
	// answer repo-wide and say where it was (mirrors cmdCard).
	if aerr != nil && sc != nil && sc.kind == scopeModule {
		if fn, fe, ferr := federatedAll(sc, storeRoot); ferr == nil {
			if t2, i2, err2 := analyze.Affected(fn, fe, f.args[0], depth, relations); err2 == nil {
				scopeNote = fmt.Sprintf("[not in %s — found in %s]", sc.moduleName, moduleOwnerOf(sc, t2.Source))
				target, impacts, aerr = t2, i2, nil
				sc = nil // repo-wide now: the boundary note no longer applies
			}
		}
	}
	if aerr != nil {
		return aerr
	}
	note := ""
	if sc != nil && sc.kind == scopeModule && crossModuleEcho(sc, storeRoot, target.Label) {
		note = boundaryNote
	}
	if f.bools["json"] {
		out := map[string]any{"target": target, "affected": impacts}
		if note != "" {
			out["note"] = note
		}
		if scopeNote != "" {
			out["scope"] = scopeNote
		}
		return emit(stdout, out)
	}
	if scopeNote != "" {
		fmt.Fprintln(stdout, scopeNote)
	}
	fmt.Fprintf(stdout, "changing %s impacts %d nodes (depth %d):\n", target.Label, len(impacts), depth)
	for _, im := range impacts {
		fmt.Fprintf(stdout, "  d%d %s  [%s]  via %s on %s\n", im.Depth, im.Node.Label, im.Node.Kind, im.Via, im.DependsOn)
	}
	if note != "" {
		fmt.Fprintln(stdout, note)
	}
	return nil
}

func cmdHubs(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	nodes, edges, err := loadGraph(f)
	if err != nil {
		return err
	}
	t0 := time.Now()
	cw := &countingWriter{w: stdout}
	stdout = cw
	st, _ := openStore(f)
	defer func() { served(st, "hubs", "", 1, cw, t0) }()
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

// cmdServe hosts the local dashboard — embedded React app + JSON API over
// the store root. Localhost by default: it is a window onto your store, not
// a service (pass --host to expose the READ side deliberately, e.g. behind a
// tunnel you control — mutation endpoints stay loopback-only regardless).
// The Ops closures below ARE the mutation doors: each one calls the same
// command function the CLI dispatches to, so the dashboard can do nothing
// the CLI couldn't.
func cmdServe(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	root, err := store.Root(f.strs["store"])
	if err != nil {
		return err
	}
	ops := serveOps(f.strs["store"])
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
	return http.Serve(ln, dashboard.NewHandler(root, ops))
}

// serveOps builds the dashboard's mutation doors. storeFlag rides into every
// closure so a --store override applies to dashboard-triggered verbs too.
func serveOps(storeFlag string) *dashboard.Ops {
	withStore := func(args []string) []string {
		if storeFlag != "" {
			return append(args, "--store", storeFlag)
		}
		return args
	}
	return &dashboard.Ops{
		Scan: func(path string) (*scan.Result, error) {
			cfg, err := project.Load(path)
			if err != nil {
				return nil, err
			}
			opts := scan.Options{}
			if cfg.Scan != nil {
				opts = *cfg.Scan
			}
			return scan.Scan(path, opts)
		},
		OnboardConfirm: func(path, name string, modules []string, out io.Writer) error {
			if name != "" {
				cfg, err := project.Load(path)
				if err != nil {
					return err
				}
				if cfg.Name != name {
					cfg.Name = name
					if err := project.Save(path, cfg); err != nil {
						return err
					}
				}
			}
			initArgs := []string{"--path", path}
			if len(modules) > 0 {
				initArgs = append(initArgs, "--scan", "--yes", "--modules", strings.Join(modules, ","))
			}
			if err := cmdInit(withStore(initArgs), out); err != nil {
				return err
			}
			return cmdAdd(withStore([]string{"--path", path}), out, strings.NewReader(""))
		},
		Gather: func(path string, out io.Writer) error {
			return cmdAdd(withStore([]string{"--path", path}), out, strings.NewReader(""))
		},
		RemoteSync: func(path, verb string, out io.Writer) error {
			return cmdRemote(append([]string{verb}, withStore([]string{"--path", path})...), out)
		},
		AddPack: func(axis, path, source string, global bool, out io.Writer) error {
			// Same door as `ctx-optimize routes add` / `manifests add`: build
			// the CLI args and dispatch to the exact command func. Route/
			// manifest packs live in dirs (repo or machine), not the store, so
			// no --store threading is needed.
			args := []string{"add", source}
			if path != "" {
				args = append(args, "--path", path)
			}
			if global {
				args = append(args, "--global")
			}
			switch axis {
			case "routes":
				return cmdRoutes(args, out)
			case "manifests":
				return cmdManifests(args, out)
			default:
				return fmt.Errorf("unknown pack axis %q (routes|manifests)", axis)
			}
		},
	}
}

// cmdLog prints the append-only mutation audit (<store-root>/audit.ndjson) —
// read-only, creates nothing.
func cmdLog(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	root, err := store.Root(f.strs["store"])
	if err != nil {
		return err
	}
	lines, err := audit.List(root)
	if err != nil {
		return err
	}
	if f.bools["json"] {
		if lines == nil {
			lines = []audit.Line{}
		}
		return emit(stdout, lines)
	}
	if len(lines) == 0 {
		fmt.Fprintln(stdout, "no changes recorded yet — mutations (dashboard or `config` set) append to "+audit.Path(root))
		return nil
	}
	for _, l := range lines {
		hashes := ""
		if l.BeforeHash != "" || l.AfterHash != "" {
			hashes = "  " + shortSHA(l.BeforeHash) + "→" + shortSHA(l.AfterHash)
		}
		fmt.Fprintf(stdout, "%s  %-9s %-24s %s%s\n", l.TS, l.Actor, l.Action, l.Target, hashes)
	}
	return nil
}

// cmdRemote is the whole sharing surface: `remote push` / `remote pull` run
// the transport COMMAND declared in the committed config.json —
//
//	{"remote": {"push": "node .ctxoptimize/push.js", "pull": "node .ctxoptimize/pull.js"}}
//
// The binary ships no transport of its own (ADR
// 2026-07-16-scripted-remote-transports — sync bytes move through YOUR
// script): the command owns the copy; the binary resolves scope, hands the
// store context over in env, and fails loudly on a non-zero exit. `remote
// init` is retired — config.json is the single source.
func cmdRemote(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ctx-optimize remote <push|pull>")
	}
	sub, rest := args[0], args[1:]
	f := parseFlags(rest)
	switch sub {
	case "init", "create":
		return fmt.Errorf("`remote %s` is gone (v0.4) — the remote IS your script now: declare {\"remote\": {\"push\": \"<cmd>\", \"pull\": \"<cmd>\"}} in %s; recipes + ready samples: .ctxoptimize/remote.example.md, push.js.sample, pull.js.sample (written by init)", sub, project.FileName)
	case "push", "pull":
		if len(f.args) > 0 {
			return fmt.Errorf("remote %s takes no arguments — the transport command lives in %s under \"remote\"", sub, project.FileName)
		}
		sc, err := resolveScope(f)
		if err != nil {
			return err
		}
		storeRoot, err := store.Root(f.strs["store"])
		if err != nil {
			return err
		}
		pc, err := project.Load(sc.rootDir)
		if err != nil {
			return err
		}
		run := pc.RemoteCommand(sub)
		if run == "" {
			if pc.Remote != nil && pc.Remote.Empty() {
				return fmt.Errorf("legacy remote config (v0.3 URL form) — transports are scripts now: declare {\"remote\": {\"%s\": \"<shell command>\"}} in %s (see .ctxoptimize/remote.example.md)", sub, project.FileName)
			}
			return fmt.Errorf("no %s command — declare it in %s: {\"remote\": {\"%s\": \"<shell command>\"}} (recipes + samples: .ctxoptimize/remote.example.md, written by init)", sub, project.FileName, sub)
		}
		return runSyncCommand(sub, run, sc, storeRoot, stdout)
	default:
		return fmt.Errorf("unknown remote subcommand %q (push|pull)", sub)
	}
}

// runSyncCommand executes the declared transport command (cwd = the repo
// root, shell line — same trust model as adapters and npm scripts). The
// command OWNS the transport; the store context arrives in env:
//
//	CTX_STORE_DIR     local store tree (push: source / pull: destination, pre-created)
//	CTX_STORE_KEY     the store's key under the store root
//	CTX_SCOPE_PREFIX  module store-key segment when invoked inside a module, else ""
//	CTX_DIRECTION     "push" or "pull" — one script can serve both
//
// Exit != 0 fails the verb; the command's output streams through.
func runSyncCommand(sub, run string, sc *scope, storeRoot string, stdout io.Writer) error {
	dir := filepath.Join(storeRoot, filepath.FromSlash(sc.rootKey))
	if sub == "pull" { // give the command a place to land the tree
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	prefix := ""
	if sc.kind == scopeModule {
		prefix = sc.syncPrefix() // store-key segment under the root
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", run)
	} else {
		cmd = exec.Command("sh", "-c", run)
	}
	cmd.Dir = sc.rootDir
	cmd.Env = append(os.Environ(),
		"CTX_STORE_DIR="+dir,
		"CTX_STORE_KEY="+sc.rootKey,
		"CTX_SCOPE_PREFIX="+prefix,
		"CTX_DIRECTION="+sub,
	)
	cmd.Stdout, cmd.Stderr = stdout, stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s (%s): %w", sub, run, err)
	}
	fmt.Fprintf(stdout, "%s done (%s)\n", sub, run)
	return nil
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

// cmdInstall: graphify-style per-platform installs with a per-platform
// report. `install` alone = every platform detected on PATH (claude always);
// `--claude/--codex/--copilot/--devin` select platforms; `--skills`/`--hooks`
// narrow what gets installed. Skills land in the two standard dirs
// (~/.claude/skills for claude; ~/.agents/skills shared by codex/copilot/
// devin). The hook exists only for claude — the one supported CLI with a
// hook API; everyone else's trigger is the repo pointer `init` writes.
func cmdInstall(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	plats := []string{}
	for _, p := range []string{"claude", "codex", "copilot", "devin"} {
		if f.bools[p] {
			plats = append(plats, p)
		}
	}
	if len(plats) == 0 { // nothing named: everything detected
		plats = append(plats, "claude")
		for _, p := range []string{"codex", "copilot", "devin"} {
			if skills.OnPath(p) {
				plats = append(plats, p)
			}
		}
	}
	doSkills := f.bools["skills"] || !f.bools["hooks"]
	doHooks := f.bools["hooks"] || !f.bools["skills"]

	claudeDir, err := skills.ClaudeSkillDir()
	if err != nil {
		return err
	}
	agentsDir, err := skills.AgentsSkillDir()
	if err != nil {
		return err
	}
	// `skills` and `hooks` settings narrow what we may write — project
	// config first (committed, team-pinned), then machine-global.
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return err
	}
	gcfg, err := store.LoadGlobalConfig(storeRoot)
	if err != nil {
		return err
	}
	skillsChoice, hooksChoice := gcfg.Skills, gcfg.Hooks
	if sc, err := resolveScope(f); err == nil && sc.cfg != nil {
		if sc.cfg.Skills != "" {
			skillsChoice = sc.cfg.Skills
		}
		if sc.cfg.Hooks != "" {
			hooksChoice = sc.cfg.Hooks
		}
	}
	allowedDirs, err := skills.SkillTargets(skillsChoice)
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, d := range allowedDirs {
		allowed[d] = true
	}
	allowedHooks, err := skills.HookPlatforms(hooksChoice)
	if err != nil {
		return err
	}
	installed := map[string]bool{}
	skillFor := func(dir string) (string, error) {
		if !allowed[dir] {
			return "", nil
		}
		if installed[dir] {
			return dir, nil
		}
		if err := skills.InstallDir(dir); err != nil {
			return "", err
		}
		installed[dir] = true
		return dir, nil
	}

	for _, plat := range plats {
		skillNote, hookNote := "—", "—"
		dir := agentsDir
		if plat == "claude" {
			dir = claudeDir
		}
		if doSkills {
			d, err := skillFor(dir)
			if err != nil {
				return err
			}
			if d == "" {
				skillNote = fmt.Sprintf("skipped (config skills = %s)", strings.ToUpper(orAll(skillsChoice)))
			} else {
				skillNote = "✓ " + d
			}
		}
		if doHooks {
			var p string
			var changed bool
			var err error
			note := ""
			switch {
			case plat == "devin":
				// Devin writes no hook file: it reads the Claude hook AND
				// AGENTS.md natively — say which lane covers it here.
				switch {
				case allowedHooks["claude"]:
					hookNote = "✓ covered — devin reads the Claude hook in ~/.claude/settings.json natively"
				case len(allowedHooks) > 0:
					hookNote = "✓ covered — devin reads AGENTS.md + ~/.agents/skills natively"
				default:
					hookNote = "covered by AGENTS.md alone (global config hooks = NONE)"
				}
			case !allowedHooks[plat]:
				hookNote = fmt.Sprintf("skipped (config hooks = %s)", strings.ToUpper(orAll(hooksChoice)))
			case plat == "claude":
				p, changed, err = skills.InstallClaudeHook()
			case plat == "codex":
				p, changed, err = skills.InstallCodexHook()
				note = " · trust it once: run `/hooks` inside codex"
			case plat == "copilot":
				p, changed, err = skills.InstallCopilotHook()
				note = " · sessionStart (its prompt event can't inject context)"
			}
			if plat != "devin" && allowedHooks[plat] {
				switch {
				case err != nil:
					hookNote = fmt.Sprintf("skipped (%v)", err)
				case changed:
					hookNote = "✓ → `ctx-optimize hook-context` (" + p + ")" + note
				default:
					hookNote = "✓ already installed (" + p + ")" + note
				}
			}
		}
		fmt.Fprintf(stdout, "%-9s skill %s\n%-9s hook  %s\n", plat, skillNote, "", hookNote)
	}
	// GLOBAL always-on rule — the standing instruction across every repo:
	// use the store before grep where one exists, and OFFER to create the
	// config where none does. Written to the user's global agent-instruction
	// files for the installed platforms (claude→~/.claude/CLAUDE.md,
	// codex→~/.codex/AGENTS.md), mirroring how the skill installs globally.
	if doSkills {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		var gtargets []string
		for _, plat := range plats {
			switch plat {
			case "claude":
				if allowed[claudeDir] {
					gtargets = append(gtargets, filepath.Join(home, ".claude", "CLAUDE.md"))
				}
			case "codex":
				if allowed[agentsDir] {
					gtargets = append(gtargets, filepath.Join(home, ".codex", "AGENTS.md"))
				}
			}
		}
		if len(gtargets) > 0 {
			written, err := project.EnsureGlobalPointer(gtargets)
			if err != nil {
				return err
			}
			if len(written) > 0 {
				fmt.Fprintf(stdout, "\nglobal rule: added the always-on \"knowledge graph before grep\" block to %s\n", strings.Join(written, ", "))
			} else {
				fmt.Fprintf(stdout, "\nglobal rule: already present (%s)\n", strings.Join(gtargets, ", "))
			}
		}
	}
	fmt.Fprintf(stdout, "\nper-repo trigger: run `ctx-optimize init` in each repo — writes the CLAUDE.md + AGENTS.md pointer block (commit them; the whole team's agents inherit it)\n")
	return nil
}

// cmdUpdate: binary first, then surfaces. The binary lane is the one
// sanctioned network moment besides `grammar build`'s zig download — it runs
// ONLY because the user invoked `update`, never in the background. Channel
// picks the mechanism: npm installs delegate to `npm install -g` (keeps the
// wrapper's optionalDependencies in sync), goreleaser standalone binaries
// self-swap from GitHub Releases (sha256-verified), dev builds and anything
// unrecognized are left alone. Surfaces (skills + hooks + global rule) then
// refresh from whichever binary is current — via a subprocess when the
// binary just changed, so the NEW bundle lands, else in-process.
// `--check` reports without touching anything.
func cmdUpdate(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	apiBase := envOr("CTX_OPTIMIZE_UPDATE_API", "https://api.github.com")
	dlBase := envOr("CTX_OPTIMIZE_UPDATE_DL", "https://github.com")

	exe, err := os.Executable()
	if err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
	}

	updated := false
	switch {
	case version.Version == "0.0.0-dev":
		fmt.Fprintln(stdout, "binary: dev build — self-update skipped (rebuild from source)")
	default:
		latest, err := selfupdate.Latest(apiBase)
		switch {
		case err != nil:
			fmt.Fprintf(stdout, "binary: update check failed (%v) — continuing with the surfaces\n", err)
		case !selfupdate.Newer(version.Version, latest):
			fmt.Fprintf(stdout, "binary: up to date (ctx-optimize %s)\n", version.Version)
		case f.bools["check"]:
			fmt.Fprintf(stdout, "binary: %s available (current %s) — run `ctx-optimize update` to apply\n", latest, version.Version)
		case selfupdate.Channel(exe) == "npm":
			fmt.Fprintf(stdout, "binary: %s available — npm-managed install, running `npm install -g @muthuishere/ctx-optimize@latest`\n", latest)
			cmd := exec.Command("npm", "install", "-g", "@muthuishere/ctx-optimize@latest")
			cmd.Stdout, cmd.Stderr = stdout, stdout
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("npm install -g: %w", err)
			}
			updated = true
		default:
			fmt.Fprintf(stdout, "binary: %s available (current %s)\n", latest, version.Version)
			if err := selfupdate.Apply(dlBase, latest, exe, stdout); err != nil {
				return err
			}
			updated = true
		}
	}
	if f.bools["check"] {
		return nil
	}

	// Surfaces from the binary that is NOW current.
	if updated {
		bin := exe
		if p, err := exec.LookPath("ctx-optimize"); err == nil {
			bin = p // npm may have moved the platform package; the launcher knows
		}
		sub := []string{"install"}
		for _, p := range []string{"claude", "codex", "copilot", "devin", "skills", "hooks"} {
			if f.bools[p] {
				sub = append(sub, "--"+p)
			}
		}
		cmd := exec.Command(bin, sub...)
		cmd.Stdout, cmd.Stderr = stdout, stdout
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("refresh surfaces from updated binary: %w", err)
		}
		fmt.Fprintln(stdout, "\nupdated — skills + hooks now match the new binary")
		return nil
	}
	if err := cmdInstall(args, stdout); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\nskills + hooks now match this binary (ctx-optimize %s)\n", version.Version)
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// cmdUninstall removes everything `install` wrote: the skill dirs, the
// hook files, and the global always-on rule. `--skills` is accepted for
// back-compat but no longer required. Stores and per-repo pointer blocks
// are deliberately untouched — stores are data, and committed pointer
// blocks self-gate on `command -v ctx-optimize`, going inert on machines
// without the binary.
func cmdUninstall(args []string, stdout io.Writer) error {
	parseFlags(args) // tolerate --skills and friends
	removed, err := skills.Uninstall()
	if err != nil {
		return err
	}
	for _, t := range removed {
		fmt.Fprintf(stdout, "removed skill: %s\n", t)
	}
	hookFiles, err := skills.RemoveHooks()
	if err != nil {
		return err
	}
	for _, t := range hookFiles {
		fmt.Fprintf(stdout, "removed hook:  %s\n", t)
	}
	// Strip the global always-on rule that install wrote (marker-fenced, so
	// content outside it is preserved; a no-op when it was never installed).
	if home, err := os.UserHomeDir(); err == nil {
		gremoved, err := project.RemoveGlobalPointer([]string{
			filepath.Join(home, ".claude", "CLAUDE.md"),
			filepath.Join(home, ".codex", "AGENTS.md"),
		})
		if err != nil {
			return err
		}
		for _, t := range gremoved {
			fmt.Fprintf(stdout, "removed global rule from: %s\n", t)
		}
	}
	fmt.Fprintln(stdout, "stores at ~/ctxoptimize untouched (delete manually if wanted); committed repo pointer blocks go inert without the binary")
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
    --instructions CLAUDE|AGENTS|ALL|NONE
                              which agent files get the pointer block (accepts
                              claude.md/agents.md too); persists to the project
                              config. Re-init never rewrites identical content.
    --scan [--yes] [--depth N] [--modules "globs"]
                              multi-module: scan, confirm, write the FULL found
                              list to config.json modules[] (generated once,
                              yours to edit after)
  scan [--depth N] [--json]   READ-ONLY module discovery: prints every project
                              found + the exact config.json init --scan writes
  config [<key> [<value>]] [--project]
                              settings, git-style two levels: machine-global
                              (~/ctxoptimize/config.json) or --project
                              (.ctxoptimize/config.json, committable);
                              project overrides global
                              instructions CLAUDE|AGENTS|ALL|NONE — files init
                              writes the pointer block into (default ALL)
                              skills CLAUDE|AGENTS|ALL — dirs install --skills
                              writes (default ALL)
                              hooks CLAUDE|AGENTS|ALL|NONE — platform hook
                              files install writes (devin needs none: it reads
                              the Claude hook and AGENTS.md natively)
  up                          THE command — from any state to a store that
                              answers: bare repo → bootstrap (init, monorepos
                              via scan) + gather · fresh clone → pull the
                              team's store (gather fallback) · stale → fast
                              re-gather · fresh → no-op. Idempotent, run it
                              whenever. Recorded sources re-capture after the
                              gather (24h TTL; --sources=always|never ·
                              --strict fails on unset vars · --prune-sources
                              removes undeclared source producers)
  add <ENV_NAME>              native source: the env var's value is a URL, its
                              scheme picks the connector (postgres, mysql,
                              mongodb, redis, kafka, nats, s3, http(s) openapi,
                              or a file path). Resolves env → root .env →
                              ~/.config/ctx-optimize/.env, dials, captures,
                              merges, and records
                              the name in config sources (refreshed on up).
                              Names only on argv — never a raw URL
  add [<path>] [--json -|F]   gather built-ins + every adapter script in
                              .ctxoptimize/adapters/; re-gather prunes stale nodes
                              (--force to allow >50%% shrink); --no-adapters skips
                              scripts; --json door upserts
                              multi-module root: fans out one worker per module
                              [--jobs N] + refreshes the navigator (no auto-merge)
  sync                        fast re-gather of the repo you're in: "add ." minus
                              adapter scripts (code/docs/manifests/git only)
  capture <ENV_NAME>          one source connector → Batch JSON on stdout, no
                              store write (the composition/debug primitive;
                              adapter scripts call it back with their own env)
  adapters <list|run [name]>  the slow lane sync skips: run adapter scripts
                              (DB, docs, queues) on demand — all, or one by
                              name; list also shows recorded sources + schemes
  adapters help <scheme>      setup card: value format, credential/cert params
                              (percent-encoding hints), export example, and the
                              paste-ready add command
  query|ask "<question>"      answer from the local store  [--budget N] [--json]
                              scope = where you ask: module dir → that module,
                              zero hits escalate; root → navigator-ranked
                              federation  [--modules all|a,b] [--root]
  path "A" "B"                shortest path between two nodes  [--json]
  explain "X"                 plain-language node + neighborhood  [--json]
  card "X"                    symbol card: signature, doc, location, callers,
                              callees — cite without opening the file  [--json]
  affected "X"                reverse impact: what breaks if X changes
                              [--depth N] [--relation R] [--json]
  change-plan "X"             ONE composed answer for "I'm about to change X":
                              signature + callers + blast radius + WHICH TESTS
                              TO RUN + co-change history + confidence footer
                              (extracted vs inferred). Replaces a query/card/
                              affected/grep chain  [--depth N] [--json]
  verify "<claim>" ...        citation check before acting on one: node exists
                              (exact id/label — never fuzzy), file exists, line
                              range in bounds, drift vs gather-time git HEAD.
                              Claims: node-id | exact-label | file:L10-L20.
                              Exit 0 only when ALL claims hold  [--json]
  hubs                        most-connected nodes (god nodes)  [--top N] [--json]

  Name resolution on card/explain/affected/path/change-plan is honest by
  default: fuzzy matches announce themselves ([resolved via fuzzy → id]);
  a fuzzy TIE refuses with ranked candidates instead of guessing (--fuzzy
  takes the top candidate anyway).
  wiki                        regenerate the markdown wiki in the store's wiki/
                              dir (deterministic, from nodes+edges only; every
                              add already regenerates it)
  status                      store facts + freshness vs git HEAD  [--json]
  fresh                       is the store current with git HEAD? one-line
                              verdict; exit 0 fresh / 1 stale / 2 unknown
                              (agent/hook gate before trusting an answer)  [--json]
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
  serve|dashboard             local dashboard over the whole store: repos,
                              onboarding, graph viewer, query, settings,
                              change log  [--port 4747] [--host 127.0.0.1]
                              (mutations stay loopback-only even with --host)
  log                         print the mutation audit (<store>/audit.ndjson):
                              ts, actor, action, target, hashes  [--json]
  languages add <name|url>    add a language: known name (kotlin, ruby, lua…)
                              or any tree-sitter grammar dir/github url —
                              compiles a drop-in pack, no toolchain to install
  languages list              embedded + packs + names addable by name
  languages remove <name>     delete a pack
  routes add <name|url>       route pack: scaffold <name>.json in
                              .ctxoptimize/routes/ (--global: ~/ctxoptimize/routes),
                              or install from a github repo / pack-.json url
  routes list                 core route recognizers + discovered packs
  routes remove <name>        delete a route pack (repo first, then global)
  manifests add <name|url>    manifest pack: scaffold <name>.json in
                              .ctxoptimize/manifests/ (--global: ~/ctxoptimize/manifests),
                              or install from a github repo / pack-.json url
  manifests list              core manifest recognizers (npm, maven, csproj/sln,
                              go.mod, gradle, k8s) + discovered packs
  manifests remove <name>     delete a manifest pack (repo first, then global)
  remote push|pull            run the transport COMMAND declared in
                              .ctxoptimize/config.json — the remote IS your
                              script; the binary ships no transport. Env handed
                              to it: CTX_STORE_DIR, CTX_STORE_KEY,
                              CTX_SCOPE_PREFIX (module scope), CTX_DIRECTION.
                              cwd = repo root; non-zero exit fails the verb
  install                     skills + hooks for every agent CLI detected; report per platform
    --claude|--codex|--copilot|--devin   select platforms · --skills / --hooks narrow scope
  update                      update EVERYTHING: the binary itself (npm installs
                              via npm, standalone via GitHub Releases, sha256-
                              verified; dev builds left alone), then skills +
                              hooks + global rule from the new binary (exact
                              replace). --check reports without touching anything
  uninstall                   remove skills, hooks, and the global rule
                              (stores + committed repo pointers untouched)
  version                     print version

flags:  --path DIR   module the store is keyed by (default: cwd)
        --store DIR  store root (default: $CTX_OPTIMIZE_STORE or ~/ctxoptimize)

The store lives at ~/ctxoptimize/<repo-name>/ ("name" in config.json overrides).

.ctxoptimize/ (in the repo, commit it):
  config.json    {"name": "my-module",
                  "remote": {"push": "node .ctxoptimize/push.js",
                             "pull": "node .ctxoptimize/pull.js"}}
                 push/pull are ANY shell line (js, py, sh, or inline)
  push.js/…      your transport scripts (init writes inert *.sample pair +
                 remote.example.md with git/s3/custom recipes)
  adapters/      drop scripts here — every .js/.py/.sh runs on add and must
                 print batch JSON to stdout (template: example.js.sample)
Secrets stay env-var NAMES in commands and scripts; the shell expands them
at run time (values are never written or printed).

The binary is deterministic: no LLM, no DB, and network ONLY when you ask —
your remote (push/pull), update (releases), grammar build (zig, once).
`)
}
