// Package dashboard serves the local store visually — an embedded React app
// (go:embed, built offline, no CDN, ZERO external requests) plus a JSON API
// over the same files queries use. It binds localhost by default: this is a
// window onto YOUR store, not a service. The remote is never touched — like
// every other read path, the dashboard answers from disk.
//
// Reads never create store dirs. Mutations exist too (onboard, re-gather,
// config edit, store delete, remote sync) but they are triple-guarded:
//
//  1. loopback-only — every mutation endpoint refuses a non-loopback peer
//     even when --host widened the listener (read may widen; write never does);
//  2. CSRF token — a per-process random token served at GET /api/token
//     (itself loopback-only) must ride in the X-Ctx-Token header. A hostile
//     web page can neither read the token (no CORS headers → the response is
//     opaque cross-origin) nor set the header from a form, so plain CSRF dies;
//  3. same doors — every mutation calls the SAME command funcs the CLI
//     dispatches to (injected as Ops closures by the app layer), so the
//     dashboard can write nothing the CLI couldn't, with the same loud errors.
//
// Every mutation appends one line to <store-root>/audit.ndjson (internal/audit).
package dashboard

import (
	"bufio"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/query"
	"github.com/muthuishere/ctx-optimize/internal/scan"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
	"github.com/muthuishere/ctx-optimize/internal/usage"
)

//go:embed all:ui
var uiFS embed.FS

// Ops are the mutation doors, injected by the CLI layer. Each closure calls
// the exact command functions the CLI dispatches to — never a parallel
// implementation. A nil Ops (or nil member) turns that mutation off with a
// clear error instead of a panic.
type Ops struct {
	// Scan previews module discovery for a repo path (read-only, same as
	// `ctx-optimize scan`).
	Scan func(path string) (*scan.Result, error)
	// OnboardConfirm is `init [--scan --yes --modules ...]` + `add .` with
	// progress streamed to out.
	OnboardConfirm func(path, name string, modules []string, out io.Writer) error
	// Gather is `add <path>` — the re-gather trigger.
	Gather func(path string, out io.Writer) error
	// RemoteSync is `remote push|pull --path <path>`.
	RemoteSync func(path, verb string, out io.Writer) error
	// AddPack installs/scaffolds a routes|manifests pack — `routes add` /
	// `manifests add`. global routes it to the machine dir; otherwise path's
	// repo dir. source is a name (scaffold) or a github/json URL (fetch).
	AddPack func(axis, path, source string, global bool, out io.Writer) error
}

// Module is one store folder as listed by /api/modules. Multi-module repos
// nest stores (key "beam/sdks/java/harness"); Root groups them under their
// repo and Summary carries the navigator's one-liner when one exists.
type Module struct {
	Key     string `json:"key"`
	Root    string `json:"root"`
	Nodes   int    `json:"nodes"`
	Edges   int    `json:"edges"`
	Summary string `json:"summary,omitempty"`
}

type server struct {
	root  string
	ops   *Ops
	token string
}

// NewHandler serves the dashboard for every module under the store root.
// ops may be nil: the read API works, mutations answer 503.
func NewHandler(root string, ops *Ops) http.Handler {
	buf := make([]byte, 24)
	rand.Read(buf)
	s := &server{root: root, ops: ops, token: hex.EncodeToString(buf)}
	mux := http.NewServeMux()

	ui, err := fs.Sub(uiFS, "ui")
	if err != nil {
		panic("dashboard ui not embedded: " + err.Error())
	}
	mux.Handle("/", http.FileServerFS(ui))

	// The CSRF token: loopback-only, fetched same-origin by the app shell.
	mux.HandleFunc("/api/token", func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r) {
			jsonError(w, http.StatusForbidden, "token is served to loopback clients only")
			return
		}
		jsonOK(w, map[string]string{"token": s.token})
	})

	// ---- reads ----
	mux.HandleFunc("/api/modules", func(w http.ResponseWriter, r *http.Request) {
		mods, err := listModules(root)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, mods)
	})
	mux.HandleFunc("/api/graph", s.handleGraph)
	mux.HandleFunc("/api/query", s.handleQuery)
	mux.HandleFunc("/api/usage", s.handleUsage)
	mux.HandleFunc("/api/stores", s.handleStores)
	mux.HandleFunc("/api/setup", s.handleSetup)
	mux.HandleFunc("/api/audit", s.handleAudit)

	// ---- mutations (loopback + token + audit; see manage.go) ----
	mux.HandleFunc("/api/onboard", s.mutation("POST", s.handleOnboardScan))
	mux.HandleFunc("/api/onboard/confirm", s.mutation("POST", s.handleOnboardConfirm))
	mux.HandleFunc("/api/repo/add", s.mutation("POST", s.handleRepoAdd))
	mux.HandleFunc("/api/config", s.mutation("PUT", s.handleConfigSet))
	mux.HandleFunc("/api/pack", s.mutation("POST", s.handlePackAdd))
	mux.HandleFunc("/api/store", s.mutation("DELETE", s.handleStoreDelete))
	mux.HandleFunc("/api/remote/push", s.mutation("POST", s.handleRemote("push")))
	mux.HandleFunc("/api/remote/pull", s.mutation("POST", s.handleRemote("pull")))

	return mux
}

// handleGraph serves a BUDGETED graph view — the server never ships the whole
// graph of a big store. Without center: the top `limit` nodes by degree
// (default 600). With center=<node-id>: the BFS neighborhood to `depth`
// (default 1, max 3) — the expand-on-click door.
func (s *server) handleGraph(w http.ResponseWriter, r *http.Request) {
	st, ok := openModule(w, s.root, r.URL.Query().Get("module"))
	if !ok {
		return
	}
	nodes, err := st.Nodes()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	edges, err := st.Edges()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	limit := 600
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, 5000)
	}
	var keptNodes []schema.Node
	if center := r.URL.Query().Get("center"); center != "" {
		depth := 1
		if v, err := strconv.Atoi(r.URL.Query().Get("depth")); err == nil && v > 0 {
			depth = min(v, 3)
		}
		keptNodes = neighborhood(nodes, edges, center, depth, limit)
		if keptNodes == nil {
			jsonError(w, http.StatusNotFound, "no node "+center)
			return
		}
	} else {
		keptNodes = topByDegree(nodes, edges, limit)
		// The v0.3 "special" kinds (routes, deps, k8s resources, tasks,
		// images, config) are usually low-degree and fall off the top-N cut.
		// Add them on top so a repo with routes/k8s/deps actually shows them —
		// capped so a pathological store can't blow the payload.
		keptNodes = includeSpecialKinds(nodes, keptNodes, min(len(nodes), limit+2000))
	}
	keep := make(map[string]bool, len(keptNodes))
	for _, n := range keptNodes {
		keep[n.ID] = true
	}
	keptEdges := []schema.Edge{}
	for _, e := range edges {
		if keep[e.Source] && keep[e.Target] {
			keptEdges = append(keptEdges, e)
		}
	}
	jsonOK(w, map[string]any{
		"nodes": keptNodes, "edges": keptEdges,
		"total_nodes": len(nodes), "total_edges": len(edges),
		"truncated": len(keptNodes) < len(nodes),
	})
}

// topByDegree returns the `limit` best-connected nodes (whole graph when it
// already fits).
func topByDegree(nodes []schema.Node, edges []schema.Edge, limit int) []schema.Node {
	if len(nodes) <= limit {
		return nodes
	}
	deg := make(map[string]int, len(nodes))
	for _, e := range edges {
		deg[e.Source]++
		deg[e.Target]++
	}
	sorted := make([]schema.Node, len(nodes))
	copy(sorted, nodes)
	sort.SliceStable(sorted, func(i, j int) bool { return deg[sorted[i].ID] > deg[sorted[j].ID] })
	return sorted[:limit]
}

// specialKinds are the v0.3 first-class kinds that the degree budget must
// never drop — they carry the routes/deps/k8s/task signal a low degree hides.
var specialKinds = map[string]bool{
	"route": true, "dependency": true, "task": true,
	"resource": true, "image": true, "config": true,
}

// includeSpecialKinds appends every special-kind node not already kept, up to
// an overall cap. Order is preserved so the degree-ranked head stays first.
func includeSpecialKinds(all, kept []schema.Node, cap int) []schema.Node {
	have := make(map[string]bool, len(kept))
	for _, n := range kept {
		have[n.ID] = true
	}
	for _, n := range all {
		if len(kept) >= cap {
			break
		}
		if specialKinds[n.Kind] && !have[n.ID] {
			have[n.ID] = true
			kept = append(kept, n)
		}
	}
	return kept
}

// neighborhood BFS-expands from center over both edge directions. nil when
// the center id does not exist.
func neighborhood(nodes []schema.Node, edges []schema.Edge, center string, depth, limit int) []schema.Node {
	byID := make(map[string]schema.Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}
	if _, ok := byID[center]; !ok {
		return nil
	}
	adj := map[string][]string{}
	for _, e := range edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
	}
	seen := map[string]bool{center: true}
	frontier := []string{center}
	out := []schema.Node{byID[center]}
	for d := 0; d < depth && len(out) < limit; d++ {
		var next []string
		for _, id := range frontier {
			for _, nb := range adj[id] {
				if seen[nb] || len(out) >= limit {
					continue
				}
				seen[nb] = true
				if n, ok := byID[nb]; ok {
					out = append(out, n)
					next = append(next, nb)
				}
			}
		}
		frontier = next
	}
	return out
}

func (s *server) handleQuery(w http.ResponseWriter, r *http.Request) {
	st, ok := openModule(w, s.root, r.URL.Query().Get("module"))
	if !ok {
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		jsonError(w, http.StatusBadRequest, "missing q parameter")
		return
	}
	budget := 2000
	if b, err := strconv.Atoi(r.URL.Query().Get("budget")); err == nil && b > 0 {
		budget = b
	}
	nodes, err := st.Nodes()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	edges, err := st.Edges()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, query.Run(nodes, edges, q, budget))
}

func (s *server) handleUsage(w http.ResponseWriter, r *http.Request) {
	mod := store.SanitizeKeyPath(r.URL.Query().Get("module"))
	if mod == "" {
		jsonError(w, http.StatusBadRequest, "module required")
		return
	}
	mod = filepath.FromSlash(mod)
	if r.URL.Query().Get("format") == "csv" {
		f, err := os.Open(usage.Path(filepath.Join(s.root, mod)))
		if err != nil {
			jsonError(w, http.StatusNotFound, "no usage recorded yet")
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=ctx-optimize-usage.csv")
		w.Write([]byte("ts,verb,arg,hits,bytes,ms\n"))
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			var e usage.Event
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue
			}
			w.Write([]byte(e.TS.Format("2006-01-02T15:04:05") + "," + e.Verb + "," +
				strconv.Quote(e.Arg) + "," + strconv.Itoa(e.Hits) + "," +
				strconv.Itoa(e.Bytes) + "," + strconv.FormatInt(e.MS, 10) + "\n"))
		}
		return
	}
	sum, err := usage.Summarize(filepath.Join(s.root, mod))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sum)
}

// listModules walks the store root for every store dir — including the
// NESTED stores a multi-module repo mirrors (beam/sdks/java/harness). Store
// artifact subdirs are pruned; a repo-root navigator (modules.json)
// contributes each module's one-line summary.
func listModules(root string) ([]Module, error) {
	skip := map[string]bool{"graph": true, "wiki": true, "cards": true,
		"hooks": true, "memory": true, "reflections": true, "grammars": true, "toolchain": true}
	summaries := map[string]string{} // key → navigator summary
	mods := []Module{}
	var walk func(dir, rel string, depth int) error
	walk = func(dir, rel string, depth int) error {
		if depth > 12 {
			return nil
		}
		if rel != "" {
			if _, err := os.Stat(filepath.Join(dir, "graph", "nodes.ndjson")); err == nil {
				top := rel
				if i := strings.IndexByte(rel, '/'); i > 0 {
					top = rel[:i]
				}
				mods = append(mods, Module{
					Key: rel, Root: top,
					Nodes: countLines(filepath.Join(dir, "graph", "nodes.ndjson")),
					Edges: countLines(filepath.Join(dir, "graph", "edges.ndjson")),
				})
				if data, err := os.ReadFile(filepath.Join(dir, "modules.json")); err == nil {
					var idx struct {
						Modules []struct{ Store, Summary string } `json:"modules"`
					}
					if json.Unmarshal(data, &idx) == nil {
						for _, m := range idx.Modules {
							if m.Summary != "" {
								summaries[m.Store] = m.Summary
							}
						}
					}
				}
			}
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil // unreadable subtree: show the rest
		}
		for _, e := range entries {
			if !e.IsDir() || skip[e.Name()] || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			child := e.Name()
			if rel != "" {
				child = rel + "/" + e.Name()
			}
			if err := walk(filepath.Join(dir, e.Name()), child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, "", 0); err != nil {
		return nil, err
	}
	for i := range mods {
		if s, ok := summaries[mods[i].Key]; ok {
			mods[i].Summary = s
		}
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].Key < mods[j].Key })
	return mods, nil
}

// openModule validates the module exists (read-only: never creates layout for
// a typo'd name) before opening it. Keys may be nested store paths; sanitize
// per segment (traversal-safe: ".." collapses to nothing).
func openModule(w http.ResponseWriter, root, key string) (*store.Store, bool) {
	if key == "" {
		jsonError(w, http.StatusBadRequest, "missing module parameter")
		return nil, false
	}
	key = store.SanitizeKeyPath(key)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(key), "graph")); err != nil {
		jsonError(w, http.StatusNotFound, "no module "+key)
		return nil, false
	}
	s, err := store.Open(root, key)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return s, true
}

// isLoopback reports whether the request's PEER (RemoteAddr, not a spoofable
// header) is a loopback address — the write-side gate that holds even when
// --host widened the read listener.
func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			n++
		}
	}
	return n
}
