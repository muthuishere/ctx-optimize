// Multi-module support: scope resolution (the git-style upward walk), the
// scan verb, fan-out add, and navigator-routed federation. Design:
// openspec/changes/2026-07-13-multi-module-init/. Single-module repos never
// enter these paths — their behavior (and output bytes) are unchanged.
package app

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/extract/code"
	"github.com/muthuishere/ctx-optimize/internal/freshness"
	"github.com/muthuishere/ctx-optimize/internal/extract/markdown"
	"github.com/muthuishere/ctx-optimize/internal/navigator"
	"github.com/muthuishere/ctx-optimize/internal/project"
	"github.com/muthuishere/ctx-optimize/internal/remote"
	"github.com/muthuishere/ctx-optimize/internal/scan"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
	"github.com/muthuishere/ctx-optimize/internal/wiki"
)

type scopeKind int

const (
	scopeSingle scopeKind = iota // today's world: one dir, one store
	scopeModule                  // inside a declared module of a root
	scopeRoot                    // at a multi-module root (or its residual tree)
)

// scope is where a question was asked, resolved against the nearest config.
type scope struct {
	kind       scopeKind
	dir        string // where the question was asked (abs)
	rootDir    string // multi-module root dir (abs; == config dir for single)
	rootKey    string // root store key
	storeKey   string // module (or single) store key; == rootKey at root
	moduleName string // navigator label when kind == scopeModule
	modulePath string // repo-relative module path when kind == scopeModule
	cfg        *project.Config
	modules    []scan.Module // expanded, concrete (root configs only)
}

// resolveScope walks up from --path (default cwd) to the nearest
// .ctxoptimize/config.json — how git finds .git. No config anywhere → single
// scope keyed by basename (compat with pre-config stores).
func resolveScope(f *flags) (*scope, error) {
	start, err := resolvePath(f)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	dir := abs
	for {
		cfgPath := filepath.Join(dir, filepath.FromSlash(project.FileName))
		if _, err := os.Stat(cfgPath); err == nil {
			cfg, err := project.Load(dir)
			if err != nil {
				return nil, err
			}
			return classifyScope(abs, dir, cfg)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// No config found: today's behavior — the asked dir is its own module.
	key, err := store.ModuleKey(abs)
	if err != nil {
		return nil, err
	}
	return &scope{kind: scopeSingle, dir: abs, rootDir: abs, rootKey: key, storeKey: key}, nil
}

func classifyScope(asked, cfgDir string, cfg *project.Config) (*scope, error) {
	// Opt-in child config: the module declares itself; its root key rides in.
	if cfg.ModuleOf != "" {
		rootKey := store.SanitizeKey(cfg.ModuleOf)
		rootDir, rel := findRootAbove(cfgDir)
		if rootDir == "" {
			// Standalone use (cloned alone): behave like a single module.
			key := store.SanitizeKey(cfg.Name)
			if key == "" {
				var err error
				if key, err = store.ModuleKey(cfgDir); err != nil {
					return nil, err
				}
			}
			return &scope{kind: scopeSingle, dir: asked, rootDir: cfgDir, rootKey: key, storeKey: key, cfg: cfg}, nil
		}
		return &scope{
			kind: scopeModule, dir: asked, rootDir: rootDir, rootKey: rootKey,
			storeKey:   store.SanitizeKeyPath(rootKey + "/" + rel),
			moduleName: moduleLabel(cfg.Name, rel), modulePath: rel, cfg: cfg,
		}, nil
	}
	rootKey := store.SanitizeKey(cfg.Name)
	if rootKey == "" {
		var err error
		if rootKey, err = store.ModuleKey(cfgDir); err != nil {
			return nil, err
		}
	}
	if len(cfg.Modules) == 0 {
		return &scope{kind: scopeSingle, dir: asked, rootDir: cfgDir, rootKey: rootKey, storeKey: rootKey, cfg: cfg}, nil
	}
	mods, err := scan.Expand(cfgDir, cfg.Modules)
	if err != nil {
		return nil, err
	}
	sc := &scope{dir: asked, rootDir: cfgDir, rootKey: rootKey, cfg: cfg, modules: mods}
	rel, err := filepath.Rel(cfgDir, asked)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)
	if rel != "." && !strings.HasPrefix(rel, "..") {
		// Longest-prefix match: nested modules resolve to the innermost.
		var best *scan.Module
		for i := range mods {
			p := mods[i].Path
			if rel == p || strings.HasPrefix(rel, p+"/") {
				if best == nil || len(p) > len(best.Path) {
					best = &mods[i]
				}
			}
		}
		if best != nil {
			sc.kind = scopeModule
			sc.storeKey = store.SanitizeKeyPath(rootKey + "/" + best.Path)
			sc.moduleName = moduleLabel(best.Name, best.Path)
			sc.modulePath = best.Path
			return sc, nil
		}
	}
	sc.kind = scopeRoot
	sc.storeKey = rootKey
	return sc, nil
}

// findRootAbove looks upward from a module dir for the enclosing root config
// (one with modules[]); returns its dir and the module's rel path.
func findRootAbove(moduleDir string) (rootDir, rel string) {
	dir := filepath.Dir(moduleDir)
	for {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(project.FileName))); err == nil {
			if cfg, err := project.Load(dir); err == nil && len(cfg.Modules) > 0 {
				if r, err := filepath.Rel(dir, moduleDir); err == nil {
					return dir, filepath.ToSlash(r)
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}

func moduleLabel(name, path string) string {
	if name != "" {
		return name
	}
	return scan.DefaultName(path)
}

// ---- scan verb ----

// cmdScan is the READ-ONLY generator preview: finds ALL module roots within
// the depth bound and prints the tree plus the exact config.json that
// `init --scan` would write. Changes nothing.
func cmdScan(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	path, err := resolvePath(f)
	if err != nil {
		return err
	}
	cfg, err := project.Load(path)
	if err != nil {
		return err
	}
	opts := scan.Options{}
	if cfg.Scan != nil {
		opts = *cfg.Scan
	}
	if v, ok := f.strs["depth"]; ok {
		d, err := strconv.Atoi(v)
		if err != nil || d < 1 {
			return fmt.Errorf("bad --depth %q", v)
		}
		opts.Depth = d
	}
	res, err := scan.Scan(path, opts)
	if err != nil {
		return err
	}
	if f.bools["json"] {
		return emit(stdout, res)
	}
	if len(res.Modules) == 0 {
		fmt.Fprintf(stdout, "no modules found (depth %d) — single-module repo; plain `ctx-optimize init` is all you need\n", res.Depth)
		return nil
	}
	fmt.Fprintf(stdout, "%d modules found (depth %d):\n", len(res.Modules), res.Depth)
	for _, m := range res.Modules {
		fmt.Fprintf(stdout, "  %-50s (%s)\n", m.Path, m.Marker)
	}
	if res.Clipped {
		fmt.Fprintf(stdout, "note: markers exist just past the depth bound — rerun with --depth %d or more\n", res.Depth+2)
	}
	name, err := store.ModuleKey(path)
	if err != nil {
		return err
	}
	if cfg.Name != "" {
		name = cfg.Name
	}
	proposed := &project.Config{Name: name, Modules: stripMarkers(res.Modules)}
	data, _ := jsonIndent(proposed)
	fmt.Fprintf(stdout, "\nproposed %s:\n%s", project.FileName, data)
	fmt.Fprintf(stdout, "\nwrite it with: ctx-optimize init --scan --yes\n")
	return nil
}

func stripMarkers(in []scan.Module) []scan.Module {
	out := make([]scan.Module, len(in))
	for i, m := range in {
		out[i] = scan.Module{Path: m.Path}
	}
	return out
}

func jsonIndent(v any) (string, error) {
	var buf bytes.Buffer
	if err := emit(&buf, v); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ---- init --scan ----

// initScan runs the generator and writes the FULL found list into the root
// config — Rails-generator style: generated once, owned by the user after.
func initScan(f *flags, path string, stdout io.Writer) error {
	cfg, err := project.Load(path)
	if err != nil {
		return err
	}
	opts := scan.Options{}
	if cfg.Scan != nil {
		opts = *cfg.Scan
	}
	if v, ok := f.strs["depth"]; ok {
		d, err := strconv.Atoi(v)
		if err != nil || d < 1 {
			return fmt.Errorf("bad --depth %q", v)
		}
		opts.Depth = d
	}
	var mods []scan.Module
	if globs := f.strs["modules"]; globs != "" {
		for _, g := range strings.Split(globs, ",") {
			if g = strings.TrimSpace(g); g != "" {
				mods = append(mods, scan.Module{Path: g})
			}
		}
	} else {
		res, err := scan.Scan(path, opts)
		if err != nil {
			return err
		}
		mods = stripMarkers(res.Modules)
		if len(mods) == 0 {
			fmt.Fprintln(stdout, "no modules found — falling back to single-module init")
			return nil
		}
		fmt.Fprintf(stdout, "%d modules found (depth %d):\n", len(mods), res.Depth)
		for _, m := range res.Modules {
			fmt.Fprintf(stdout, "  %-50s (%s)\n", m.Path, m.Marker)
		}
		if res.Clipped {
			fmt.Fprintf(stdout, "note: markers exist just past the depth bound — rerun with --depth %d or more\n", res.Depth+2)
		}
	}
	if !f.bools["yes"] {
		fmt.Fprint(stdout, "write these to "+project.FileName+" modules[]? [y/N] ")
		sc := bufio.NewScanner(os.Stdin)
		if !sc.Scan() || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(sc.Text())), "y") {
			return fmt.Errorf("aborted — nothing written (use --yes to skip the prompt)")
		}
	}
	cfg.Modules = mods
	if cfg.Name == "" {
		if cfg.Name, err = store.ModuleKey(path); err != nil {
			return err
		}
	}
	if err := project.Save(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s written: %d modules — the list is yours now (edit, add, prune)\n", project.FileName, len(cfg.Modules))
	return nil
}

// adoptIfDeclaredModule handles plain `init` inside a dir some ancestor root
// already declares as a module: write the minimal child config (module_of)
// and open the MIRRORED store — never a shadow store keyed by basename.
func adoptIfDeclaredModule(f *flags, path string, stdout io.Writer) (bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(filepath.Join(abs, filepath.FromSlash(project.FileName))); err == nil {
		return false, nil // has its own config — normal init
	}
	rootDir, rel := findRootAbove(abs)
	if rootDir == "" || rel == "" {
		return false, nil
	}
	rootCfg, err := project.Load(rootDir)
	if err != nil {
		return false, err
	}
	mods, err := scan.Expand(rootDir, rootCfg.Modules)
	if err != nil {
		return false, err
	}
	var decl *scan.Module
	for i := range mods {
		if rel == mods[i].Path {
			decl = &mods[i]
			break
		}
	}
	if decl == nil {
		return false, nil // inside the root's residual tree — normal init
	}
	rootKey := store.SanitizeKey(rootCfg.Name)
	if rootKey == "" {
		if rootKey, err = store.ModuleKey(rootDir); err != nil {
			return false, err
		}
	}
	if err := project.Save(abs, &project.Config{
		Name: moduleLabel(decl.Name, decl.Path), ModuleOf: rootKey,
	}); err != nil {
		return false, err
	}
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return false, err
	}
	s, err := store.Open(storeRoot, store.SanitizeKeyPath(rootKey+"/"+decl.Path))
	if err != nil {
		return false, err
	}
	if _, err := s.UpdateManifest(); err != nil {
		return false, err
	}
	fmt.Fprintf(stdout, "adopted as module %q of root %q — store: %s\n%s written (module_of) — commit it\n",
		decl.Path, rootKey, s.Dir, project.FileName)
	return true, nil
}

// ---- fan-out add ----

type gatherTask struct {
	dir      string   // abs dir to gather
	storeKey string   // mirrored store key
	label    string   // print label ("services/api" or "." for the root residual)
	excludes []string // abs subtree roots NOT to walk (child module dirs)
}

// planTasks expands a root config into one task per module (recursing into
// child configs that declare their own modules — multi-level) plus the
// residual task for the root tree minus module subtrees. Declared modules
// may NEST (beam: a maven module under another's src/main/resources) — every
// task excludes the other declared dirs inside its own tree, so no file is
// ever extracted twice.
func planTasks(rootDir, rootKey string, mods []scan.Module, seen map[string]bool) ([]gatherTask, error) {
	var tasks []gatherTask
	var allDirs []string
	for _, m := range mods {
		abs := filepath.Join(rootDir, filepath.FromSlash(m.Path))
		if seen[abs] {
			continue // gathered once per run; first declaration wins
		}
		seen[abs] = true
		allDirs = append(allDirs, abs)
		key := store.SanitizeKeyPath(rootKey + "/" + m.Path)
		childCfg, err := project.Load(abs)
		if err != nil {
			return nil, err
		}
		if len(childCfg.Modules) > 0 {
			childMods, err := scan.Expand(abs, childCfg.Modules)
			if err != nil {
				return nil, err
			}
			sub, err := planTasks(abs, key, childMods, seen)
			if err != nil {
				return nil, err
			}
			// Rebase labels so output reads repo-relative from the top.
			for _, t := range sub {
				if t.label == "." {
					t.label = m.Path
				} else {
					t.label = m.Path + "/" + t.label
				}
				tasks = append(tasks, t)
			}
			continue
		}
		tasks = append(tasks, gatherTask{dir: abs, storeKey: key, label: m.Path})
	}
	sep := string(filepath.Separator)
	for i := range tasks {
		for _, d := range allDirs {
			if d != tasks[i].dir && strings.HasPrefix(d, tasks[i].dir+sep) {
				tasks[i].excludes = append(tasks[i].excludes, d)
			}
		}
	}
	tasks = append(tasks, gatherTask{dir: rootDir, storeKey: rootKey, label: ".", excludes: allDirs})
	return tasks, nil
}

// gatherInto runs the standard single-module gather (markdown + code +
// adapters) into the given store. All prints go to out (buffered per worker
// in fan-out mode so scheduling never reorders bytes).
func gatherInto(s *store.Store, dir string, excludes []string, force bool, out io.Writer) error {
	var batches []*schema.Batch
	b, err := markdown.ExtractExcluding(dir, excludes)
	if err != nil {
		return err
	}
	batches = append(batches, b)
	cb, err := code.ExtractExcluding(dir, excludes)
	if err != nil {
		return err
	}
	// Always Replace, even when empty: an empty batch against an empty
	// producer is a no-op, but against previous code nodes it must hit the
	// shrink guard — skipping it here silently kept deleted code in the graph.
	batches = append(batches, cb)
	if len(cb.Nodes) > 0 {
		fmt.Fprintf(out, "code: %d nodes, %d edges\n", len(cb.Nodes), len(cb.Edges))
	}
	pc, err := project.Load(dir)
	if err != nil {
		return err
	}
	adapters := append([]project.Adapter{}, pc.Adapters...)
	declared := map[string]bool{}
	for _, a := range adapters {
		declared[a.Name] = true
	}
	discovered, err := project.DiscoverAdapters(dir)
	if err != nil {
		return err
	}
	for _, a := range discovered {
		if !declared[a.Name] {
			adapters = append(adapters, a)
		}
	}
	for _, a := range adapters {
		ab, err := runAdapter(dir, a)
		if err != nil {
			return fmt.Errorf("adapter %s: %w", a.Name, err)
		}
		batches = append(batches, ab)
		fmt.Fprintf(out, "adapter %s: %d nodes, %d edges\n", a.Name, len(ab.Nodes), len(ab.Edges))
	}
	totalN, totalPruned := 0, 0
	for _, b := range batches {
		n, pruned, err := s.Replace(b, force)
		if err != nil {
			return err
		}
		totalN += n
		totalPruned += pruned
	}
	pages, err := wiki.Generate(s)
	if err != nil {
		return err
	}
	if _, err := s.UpdateManifest(); err != nil {
		return err
	}
	// Record source provenance so freshness can later tell whether this
	// store still reflects the repo. Best-effort: a non-git dir records
	// nothing. Every gather path (single, module, fan-out worker) runs
	// through here, so module stores carry their own provenance.
	if abs, aerr := filepath.Abs(dir); aerr == nil {
		if head, headUnix, ok := gitHead(abs); ok {
			if err := s.RecordSource(freshness.Source{
				Path: abs, Head: head, HeadUnix: headUnix, AddedUnix: time.Now().Unix(),
			}); err != nil {
				return err
			}
		}
	}
	fmt.Fprintf(out, "added %d nodes", totalN)
	if totalPruned > 0 {
		fmt.Fprintf(out, ", pruned %d stale", totalPruned)
	}
	fmt.Fprintf(out, " → %s\n", s.Dir)
	fmt.Fprintf(out, "wiki: %d pages → %s\n", pages, filepath.Join(s.Dir, "wiki"))
	return nil
}

// runMultiAdd fans the gather across every module concurrently, then
// regenerates the navigator. NO merged store — merge stays an explicit verb.
func runMultiAdd(sc *scope, f *flags, stdout io.Writer) error {
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return err
	}
	tasks, err := planTasks(sc.rootDir, sc.rootKey, sc.modules, map[string]bool{})
	if err != nil {
		return err
	}
	jobs := min(runtime.NumCPU(), 8)
	if v, ok := f.strs["jobs"]; ok {
		j, err := strconv.Atoi(v)
		if err != nil || j < 1 {
			return fmt.Errorf("bad --jobs %q", v)
		}
		jobs = j
	}
	force := f.bools["force"]

	type result struct {
		out bytes.Buffer
		err error
	}
	results := make([]result, len(tasks))
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	for i := range tasks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t := tasks[i]
			s, err := store.Open(storeRoot, t.storeKey)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].err = gatherInto(s, t.dir, t.excludes, force, &results[i].out)
		}(i)
	}
	wg.Wait()

	var failed []string
	for i, t := range tasks {
		fmt.Fprintf(stdout, "== %s\n", t.label)
		io.Copy(stdout, &results[i].out)
		if results[i].err != nil {
			fmt.Fprintf(stdout, "FAILED: %v\n", results[i].err)
			failed = append(failed, t.label)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d modules failed: %s", len(failed), len(tasks), strings.Join(failed, ", "))
	}

	// Navigator: the root artifact. Every completed multi-module add
	// refreshes it.
	entries := make([]navigator.ModuleEntry, 0, len(tasks)-1)
	for _, t := range tasks {
		if t.label == "." {
			continue
		}
		entries = append(entries, navigator.ModuleEntry{
			Name: scan.DefaultName(t.label), Path: t.label, Store: t.storeKey,
		})
	}
	idx, err := navigator.Build(sc.rootDir, storeRoot, sc.rootKey, entries)
	if err != nil {
		return err
	}
	rootStore, err := store.Open(storeRoot, sc.rootKey)
	if err != nil {
		return err
	}
	if err := idx.Write(rootStore.Dir); err != nil {
		return err
	}
	if _, err := rootStore.UpdateManifest(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "== navigator\n%d modules → %s\n", len(idx.Modules), filepath.Join(rootStore.Dir, "navigator.md"))
	return nil
}

// refreshNavigatorEntry rebuilds the root navigator after a module-scoped
// add — only when one already exists (a root add created it); a module used
// standalone never conjures root artifacts.
func refreshNavigatorEntry(sc *scope, storeRoot string) error {
	rootStore, err := store.Open(storeRoot, sc.rootKey)
	if err != nil {
		return err
	}
	idx, err := navigator.Load(rootStore.Dir)
	if err != nil || idx == nil {
		return err
	}
	rootCfg, err := project.Load(sc.rootDir)
	if err != nil {
		return err
	}
	mods, err := scan.Expand(sc.rootDir, rootCfg.Modules)
	if err != nil {
		return err
	}
	entries := make([]navigator.ModuleEntry, 0, len(mods))
	for _, m := range mods {
		entries = append(entries, navigator.ModuleEntry{
			Name: moduleLabel(m.Name, m.Path), Path: m.Path,
			Store: store.SanitizeKeyPath(sc.rootKey + "/" + m.Path),
		})
	}
	fresh, err := navigator.Build(sc.rootDir, storeRoot, sc.rootKey, entries)
	if err != nil {
		return err
	}
	return fresh.Write(rootStore.Dir)
}

// ---- federated read path ----

// namespacedLoad loads one module store with node IDs, edge endpoints, and
// Source paths prefixed by the module path — collision-free concat, and
// sources resolve from the repo root (cards can read bodies).
func namespacedLoad(storeRoot, key, modPath string) ([]schema.Node, []schema.Edge, error) {
	s, err := store.Open(storeRoot, key)
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
	if modPath == "" {
		return nodes, edges, nil
	}
	pre := strings.Trim(modPath, "/") + "/"
	for i := range nodes {
		nodes[i].ID = pre + nodes[i].ID
		if nodes[i].Source != "" && !strings.Contains(nodes[i].Source, "://") {
			nodes[i].Source = pre + nodes[i].Source
		}
	}
	for i := range edges {
		edges[i].Source = pre + edges[i].Source
		edges[i].Target = pre + edges[i].Target
	}
	return nodes, edges, nil
}

// loadFederated concatenates the selected modules (nil = all) plus the root
// residual store into one namespaced in-memory graph.
func loadFederated(sc *scope, storeRoot string, only []scan.Module) ([]schema.Node, []schema.Edge, error) {
	mods := only
	if mods == nil {
		mods = sc.modules
	}
	var nodes []schema.Node
	var edges []schema.Edge
	rn, re, err := namespacedLoad(storeRoot, sc.rootKey, "")
	if err != nil {
		return nil, nil, err
	}
	nodes, edges = append(nodes, rn...), append(edges, re...)
	for _, m := range mods {
		key := store.SanitizeKeyPath(sc.rootKey + "/" + m.Path)
		mn, me, err := namespacedLoad(storeRoot, key, m.Path)
		if err != nil {
			return nil, nil, err
		}
		nodes, edges = append(nodes, mn...), append(edges, me...)
	}
	return nodes, edges, nil
}

// scopeStoreRels enumerates the store rel paths a sync scope covers,
// relative to the ROOT store dir: the whole tree at a multi-module root,
// just the module's subtree (nested stores included) inside a module.
func scopeStoreRels(sc *scope, storeRoot string) ([]string, error) {
	if sc.kind == scopeModule {
		modDir := filepath.Join(storeRoot, filepath.FromSlash(sc.storeKey))
		rels, err := remote.LocalStoreRels(modDir)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(rels))
		for _, r := range rels {
			if r == "" {
				out = append(out, sc.modulePath)
			} else {
				out = append(out, sc.modulePath+"/"+r)
			}
		}
		return out, nil
	}
	return remote.LocalStoreRels(filepath.Join(storeRoot, filepath.FromSlash(sc.rootKey)))
}

// expandRootModules loads the root config's module list for a scope that
// arrived without one (child-config module scope).
func expandRootModules(sc *scope) ([]scan.Module, error) {
	rootCfg, err := project.Load(sc.rootDir)
	if err != nil {
		return nil, err
	}
	return scan.Expand(sc.rootDir, rootCfg.Modules)
}

func modulePaths(mods []scan.Module) string {
	var ps []string
	for _, m := range mods {
		ps = append(ps, m.Path)
	}
	return strings.Join(ps, ", ")
}

// boundaryNote is the honesty line for module-scoped graph analysis: the
// module store cannot contain cross-module edges, so a blast radius or path
// may be truncated at the boundary.
const boundaryNote = "note: module-scoped — cross-module edges are not in this graph; run with --root for repo-wide impact"

// federatedAll loads the whole repo's namespaced graph from a module scope —
// the escalation target for analysis verbs whose symbol isn't local.
func federatedAll(sc *scope, storeRoot string) ([]schema.Node, []schema.Edge, error) {
	if len(sc.modules) == 0 {
		mods, err := expandRootModules(sc)
		if err != nil {
			return nil, nil, err
		}
		sc.modules = mods
	}
	return loadFederated(sc, storeRoot, nil)
}

// crossModuleEcho reports whether the navigator knows ANOTHER module whose
// hub directory carries this label — the cheap signal that a module-scoped
// answer may be truncated at the boundary. No navigator (or no way to
// check) → true: stay honest rather than silently confident.
func crossModuleEcho(sc *scope, storeRoot, label string) bool {
	idx, err := navigator.Load(filepath.Join(storeRoot, filepath.FromSlash(sc.rootKey)))
	if err != nil || idx == nil {
		return true
	}
	for _, m := range idx.OwnerOf(label) {
		if m.Path != sc.modulePath {
			return true
		}
	}
	return false
}

// moduleOwnerOf maps a namespaced (repo-relative) source path back to the
// declared module that owns it — for labeling escalated answers.
func moduleOwnerOf(sc *scope, source string) string {
	owner := "root"
	for _, m := range sc.modules {
		if source == m.Path || strings.HasPrefix(source, m.Path+"/") {
			if owner == "root" || len(m.Path) > len(owner) {
				owner = m.Path
			}
		}
	}
	return owner
}
