// Management surface: the read endpoints that make the whole setup visible
// (/api/stores, /api/setup, /api/audit) and the guarded mutation endpoints.
// Design: openspec/changes/2026-07-14-dashboard-management/proposal.md.
//
// Every mutation is (1) loopback-only by peer address, (2) X-Ctx-Token
// checked, (3) routed through the same doors the CLI uses, and (4) appended
// to <store-root>/audit.ndjson.
package dashboard

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/audit"
	"github.com/muthuishere/ctx-optimize/internal/extract/code"
	"github.com/muthuishere/ctx-optimize/internal/freshness"
	"github.com/muthuishere/ctx-optimize/internal/gitinfo"
	"github.com/muthuishere/ctx-optimize/internal/project"
	"github.com/muthuishere/ctx-optimize/internal/skills"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

// mutation wraps a handler with the write-side guards. Method mismatch is
// 405; a non-loopback peer or a missing/bad token is 403 — checked in that
// order so a remote probe learns nothing about the token.
func (s *server) mutation(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			jsonError(w, http.StatusMethodNotAllowed, "method "+r.Method+" not allowed (want "+method+")")
			return
		}
		if !isLoopback(r) {
			jsonError(w, http.StatusForbidden, "mutations are loopback-only — even with --host widened, writes never leave 127.0.0.1")
			return
		}
		if r.Header.Get("X-Ctx-Token") != s.token {
			jsonError(w, http.StatusForbidden, "missing or bad X-Ctx-Token (fetch GET /api/token same-origin)")
			return
		}
		next(w, r)
	}
}

// record appends one audit line (actor: dashboard). Fail-silent by design —
// a full disk must not turn a completed mutation into a reported failure —
// but the tests assert lines land.
func (s *server) record(action, target, beforeHash, afterHash string) {
	audit.Append(s.root, audit.Line{
		Actor: "dashboard", Action: action, Target: target,
		BeforeHash: beforeHash, AfterHash: afterHash,
	})
}

// ---- reads ----

// StoreInfo is one store as listed by /api/stores — the Repos screen row.
type StoreInfo struct {
	Key        string             `json:"key"`
	Root       string             `json:"root"`
	Nodes      int                `json:"nodes"`
	Edges      int                `json:"edges"`
	Summary    string             `json:"summary,omitempty"`
	Fresh      string             `json:"fresh"` // fresh|stale|unknown
	SourcePath string             `json:"source_path,omitempty"`
	AgeSeconds int64              `json:"age_seconds,omitempty"`
	Producers  map[string]int     `json:"producers,omitempty"`
	Reports    []freshness.Report `json:"freshness,omitempty"`
}

func (s *server) handleStores(w http.ResponseWriter, r *http.Request) {
	mods, err := listModules(s.root)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]StoreInfo, 0, len(mods))
	now := time.Now().Unix()
	for _, m := range mods {
		dir := filepath.Join(s.root, filepath.FromSlash(m.Key))
		info := StoreInfo{
			Key: m.Key, Root: m.Root, Nodes: m.Nodes, Edges: m.Edges,
			Summary: m.Summary, Fresh: string(freshness.Unknown),
			Producers: producerCounts(filepath.Join(dir, "graph", "nodes.ndjson")),
		}
		// source.json read directly (never store.Open here: the read path
		// must not create layout).
		if srcs := readSources(filepath.Join(dir, "source.json")); len(srcs) > 0 {
			reports := make([]freshness.Report, 0, len(srcs))
			for _, src := range srcs {
				head, headUnix, _ := gitinfo.Head(src.Path)
				reports = append(reports, freshness.Evaluate(src, head, headUnix, now))
			}
			info.Fresh = string(freshness.Overall(reports))
			info.Reports = reports
			info.SourcePath = srcs[0].Path
			info.AgeSeconds = reports[0].AgeSeconds
		}
		out = append(out, info)
	}
	jsonOK(w, out)
}

func readSources(path string) []freshness.Source {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var srcs []freshness.Source
	if json.Unmarshal(data, &srcs) != nil {
		return nil
	}
	return srcs
}

// producerCounts tallies nodes per producer by streaming the ndjson — only
// the metadata.producer field is decoded.
func producerCounts(nodesPath string) map[string]int {
	f, err := os.Open(nodesPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	counts := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var n struct {
			Metadata map[string]string `json:"metadata"`
		}
		if json.Unmarshal(sc.Bytes(), &n) != nil {
			continue
		}
		p := n.Metadata["producer"]
		if p == "" {
			p = "(unknown)"
		}
		counts[p]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

// configKV is one effective setting with the level that decided it.
type configKV struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"` // project|global|default
}

// handleSetup renders the whole extension surface: effective config (both
// levels, source shown), packs per axis, adapters, remote — everything with
// its owning FILE path, because the file stays the source of truth. Pass
// ?path=<repo> for the project-level half; without it the view is global.
func (s *server) handleSetup(w http.ResponseWriter, r *http.Request) {
	gcfg, err := store.LoadGlobalConfig(s.root)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := map[string]any{
		"store_root": s.root,
		"global":     map[string]any{"file": filepath.Join(s.root, "config.json"), "config": gcfg},
	}
	repoPath := r.URL.Query().Get("path")
	var pcfg *project.Config
	if repoPath != "" {
		if fi, err := os.Stat(repoPath); err != nil || !fi.IsDir() {
			jsonError(w, http.StatusBadRequest, "path is not a directory: "+repoPath)
			return
		}
		pcfg, err = project.Load(repoPath)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp["project"] = map[string]any{
			"path":   repoPath,
			"file":   filepath.Join(repoPath, filepath.FromSlash(project.FileName)),
			"config": pcfg,
		}
		if pcfg.Remote != nil && pcfg.Remote.URL != "" {
			// Raw form only — ${VAR} placeholders, never resolved values.
			resp["remote"] = map[string]string{"url": pcfg.Remote.URL, "from": project.FileName}
		}
	}

	// Effective config table (project > global > default), source labeled.
	pick := func(proj, glob string) (string, string) {
		if pcfg != nil && proj != "" {
			return proj, "project"
		}
		if glob != "" {
			return glob, "global"
		}
		return "ALL", "default"
	}
	var effective []configKV
	for _, k := range [][3]string{
		{"instructions", cfgStr(pcfg, func(c *project.Config) string { return c.Instructions }), gcfg.Instructions},
		{"skills", cfgStr(pcfg, func(c *project.Config) string { return c.Skills }), gcfg.Skills},
		{"hooks", cfgStr(pcfg, func(c *project.Config) string { return c.Hooks }), gcfg.Hooks},
	} {
		v, src := pick(k[1], k[2])
		effective = append(effective, configKV{Key: k[0], Value: v, Source: src})
	}
	resp["effective"] = effective

	// Axes. Grammar packs are discoverable files (view via their paths);
	// routes/manifests are built into the code producer on this build (no
	// pack loader) — say so instead of pretending.
	axes := []map[string]any{}
	packsRepo := repoPath
	if packsRepo == "" {
		packsRepo = s.root // no repo selected: global grammars dir only
	}
	grammarAxis := map[string]any{"axis": "grammars", "kind": "packs",
		"note": "drop <name>.wasm + <name>.json into ~/ctxoptimize/grammars/ or <repo>/.ctxoptimize/grammars/ (view-only here; build with `ctx-optimize languages add`)"}
	if packs, err := code.LoadPacks(packsRepo); err != nil {
		grammarAxis["error"] = err.Error()
	} else {
		list := []map[string]any{}
		for _, p := range packs {
			list = append(list, map[string]any{
				"name": p.Lang.Name, "exts": p.Lang.Exts,
				"wasm": p.WasmPath, "config": strings.TrimSuffix(p.WasmPath, ".wasm") + ".json",
			})
		}
		grammarAxis["packs"] = list
	}
	axes = append(axes, grammarAxis)
	axes = append(axes, map[string]any{"axis": "routes", "kind": "builtin",
		"note": "route extraction (FastAPI/Flask/Express/Nest) is built into the code producer on this build — no route packs to edit"})
	axes = append(axes, map[string]any{"axis": "manifests", "kind": "builtin",
		"note": "manifest/config-file extraction (package.json, pom.xml, go.mod, Dockerfile, …) is built into the code producer — no manifest packs to edit"})
	adapterAxis := map[string]any{"axis": "adapters", "kind": "scripts",
		"note": "drop .js/.py/.sh into <repo>/.ctxoptimize/adapters/ — dropping the file IS the registration"}
	if repoPath != "" {
		if discovered, err := project.DiscoverAdapters(repoPath); err == nil {
			list := []map[string]string{}
			for _, a := range discovered {
				list = append(list, map[string]string{
					"name": a.Name, "run": a.Run,
					"file": filepath.Join(repoPath, filepath.FromSlash(project.AdaptersDir)),
				})
			}
			for _, a := range declaredAdapters(pcfg) {
				list = append(list, a)
			}
			adapterAxis["adapters"] = list
		}
	}
	axes = append(axes, adapterAxis)
	resp["axes"] = axes
	jsonOK(w, resp)
}

func cfgStr(c *project.Config, get func(*project.Config) string) string {
	if c == nil {
		return ""
	}
	return get(c)
}

func declaredAdapters(pcfg *project.Config) []map[string]string {
	if pcfg == nil {
		return nil
	}
	var out []map[string]string
	for _, a := range pcfg.Adapters {
		out = append(out, map[string]string{"name": a.Name, "run": a.Run, "file": project.FileName})
	}
	return out
}

func (s *server) handleAudit(w http.ResponseWriter, r *http.Request) {
	lines, err := audit.List(s.root)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if lines == nil {
		lines = []audit.Line{}
	}
	jsonOK(w, lines)
}

// ---- mutations ----

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		jsonError(w, http.StatusBadRequest, "bad json body: "+err.Error())
		return false
	}
	return true
}

func validRepoPath(w http.ResponseWriter, path string) bool {
	if path == "" {
		jsonError(w, http.StatusBadRequest, "path required")
		return false
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		jsonError(w, http.StatusBadRequest, "path is not a directory: "+path)
		return false
	}
	return true
}

// handleOnboardScan previews module discovery — the first onboarding step.
func (s *server) handleOnboardScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeBody(w, r, &req) || !validRepoPath(w, req.Path) {
		return
	}
	if s.ops == nil || s.ops.Scan == nil {
		jsonError(w, http.StatusServiceUnavailable, "scan unavailable in this handler")
		return
	}
	res, err := s.ops.Scan(req.Path)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.record("onboard.scan", req.Path, "", "")
	jsonOK(w, res)
}

// streamStart switches the response to a flushed plain-text progress stream.
// Errors after this point ride IN the stream as a final "ERROR: …" line —
// the status code is already committed.
func streamStart(w http.ResponseWriter) io.Writer {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		return flushWriter{w, f}
	}
	return w
}

type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func (fw flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	fw.f.Flush()
	return n, err
}

// handleOnboardConfirm is `init [--scan --yes --modules a,b] && add .` with
// live progress — the same two verbs a CLI onboarding runs, streamed.
func (s *server) handleOnboardConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string   `json:"path"`
		Name    string   `json:"name"`
		Modules []string `json:"modules"`
	}
	if !decodeBody(w, r, &req) || !validRepoPath(w, req.Path) {
		return
	}
	if s.ops == nil || s.ops.OnboardConfirm == nil {
		jsonError(w, http.StatusServiceUnavailable, "onboarding unavailable in this handler")
		return
	}
	cfgFile := filepath.Join(req.Path, filepath.FromSlash(project.FileName))
	before := audit.FileHash(cfgFile)
	out := streamStart(w)
	err := s.ops.OnboardConfirm(req.Path, req.Name, req.Modules, out)
	s.record("onboard.confirm", req.Path, before, audit.FileHash(cfgFile))
	if err != nil {
		fmt.Fprintf(out, "ERROR: %v\n", err)
		return
	}
	fmt.Fprintln(out, "DONE")
}

// handleRepoAdd is the re-gather trigger — `add <path>`, streamed.
func (s *server) handleRepoAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeBody(w, r, &req) || !validRepoPath(w, req.Path) {
		return
	}
	if s.ops == nil || s.ops.Gather == nil {
		jsonError(w, http.StatusServiceUnavailable, "re-gather unavailable in this handler")
		return
	}
	out := streamStart(w)
	err := s.ops.Gather(req.Path, out)
	s.record("repo.add", req.Path, "", "")
	if err != nil {
		fmt.Fprintf(out, "ERROR: %v\n", err)
		return
	}
	fmt.Fprintln(out, "DONE")
}

// handleConfigSet writes one settings key at either level through the same
// validated doors cmdConfig uses (same validators, same Save funcs, same
// loud errors). before/after hashes of the touched file land in the audit.
func (s *server) handleConfigSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level string `json:"level"` // global|project
		Path  string `json:"path"`  // repo path, required for project
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	v := strings.ToUpper(strings.TrimSpace(req.Value))
	var validate func(string) error
	switch req.Key {
	case "instructions":
		validate = func(v string) error { _, err := project.PointerTargets(v); return err }
	case "skills":
		validate = func(v string) error { _, err := skills.SkillTargets(v); return err }
	case "hooks":
		validate = func(v string) error { _, err := skills.HookPlatforms(v); return err }
	default:
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("unknown config key %q — keys: instructions, skills, hooks", req.Key))
		return
	}
	if err := validate(v); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch req.Level {
	case "global":
		file := filepath.Join(s.root, "config.json")
		before := audit.FileHash(file)
		gcfg, err := store.LoadGlobalConfig(s.root)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setKey(gcfgFields(gcfg), req.Key, v)
		if err := store.SaveGlobalConfig(s.root, gcfg); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.record("config.set "+req.Key+"="+v, file, before, audit.FileHash(file))
		jsonOK(w, map[string]string{"key": req.Key, "value": v, "level": "global", "file": file})
	case "project":
		if !validRepoPath(w, req.Path) {
			return
		}
		file := filepath.Join(req.Path, filepath.FromSlash(project.FileName))
		before := audit.FileHash(file)
		pcfg, err := project.Load(req.Path)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setKey(pcfgFields(pcfg), req.Key, v)
		if err := project.Save(req.Path, pcfg); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.record("config.set "+req.Key+"="+v, file, before, audit.FileHash(file))
		jsonOK(w, map[string]string{"key": req.Key, "value": v, "level": "project", "file": file})
	default:
		jsonError(w, http.StatusBadRequest, "level must be global or project")
	}
}

// field maps: one place that knows which struct field a key names.
func gcfgFields(g *store.GlobalConfig) map[string]*string {
	return map[string]*string{"instructions": &g.Instructions, "skills": &g.Skills, "hooks": &g.Hooks}
}

func pcfgFields(p *project.Config) map[string]*string {
	return map[string]*string{"instructions": &p.Instructions, "skills": &p.Skills, "hooks": &p.Hooks}
}

func setKey(fields map[string]*string, key, value string) {
	if f, ok := fields[key]; ok {
		*f = value
	}
}

// handleStoreDelete removes one store dir — confirm-gated, sanitized, and
// only ever a dir that actually IS a store (has graph/).
func (s *server) handleStoreDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key     string `json:"key"`
		Confirm bool   `json:"confirm"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	key := store.SanitizeKeyPath(req.Key)
	if key == "" {
		jsonError(w, http.StatusBadRequest, "key required")
		return
	}
	if !req.Confirm {
		jsonError(w, http.StatusBadRequest, "confirm:true required — deletion is permanent")
		return
	}
	dir := filepath.Join(s.root, filepath.FromSlash(key))
	if _, err := os.Stat(filepath.Join(dir, "graph")); err != nil {
		jsonError(w, http.StatusNotFound, "no store "+key)
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.record("store.delete", key, "", "")
	jsonOK(w, map[string]string{"deleted": key})
}

// handleRemote triggers push/pull for a repo path — `remote push|pull`,
// streamed (S3 syncs can take a while).
func (s *server) handleRemote(verb string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path string `json:"path"`
		}
		if !decodeBody(w, r, &req) || !validRepoPath(w, req.Path) {
			return
		}
		if s.ops == nil || s.ops.RemoteSync == nil {
			jsonError(w, http.StatusServiceUnavailable, "remote sync unavailable in this handler")
			return
		}
		out := streamStart(w)
		err := s.ops.RemoteSync(req.Path, verb, out)
		s.record("remote."+verb, req.Path, "", "")
		if err != nil {
			fmt.Fprintf(out, "ERROR: %v\n", err)
			return
		}
		fmt.Fprintln(out, "DONE")
	}
}
