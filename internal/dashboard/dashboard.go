// Package dashboard serves the local store visually — a single embedded HTML
// page (no CDN, no external requests, works offline) plus a tiny read-only
// JSON API over the same files queries use. It binds localhost by default:
// this is a window onto YOUR store, not a service. The remote is never
// touched — like every other read path, the dashboard answers from disk.
package dashboard

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/query"
	"github.com/muthuishere/ctx-optimize/internal/store"
	"github.com/muthuishere/ctx-optimize/internal/usage"
)

//go:embed index.html
var indexHTML []byte

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

// NewHandler serves the dashboard for every module under the store root.
func NewHandler(root string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	mux.HandleFunc("/api/modules", func(w http.ResponseWriter, r *http.Request) {
		mods, err := listModules(root)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, mods)
	})

	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		s, ok := openModule(w, root, r.URL.Query().Get("module"))
		if !ok {
			return
		}
		nodes, err := s.Nodes()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		edges, err := s.Edges()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, map[string]any{"nodes": nodes, "edges": edges})
	})

	mux.HandleFunc("/api/usage", func(w http.ResponseWriter, r *http.Request) {
		mod := store.SanitizeKeyPath(r.URL.Query().Get("module"))
		if mod == "" {
			jsonError(w, http.StatusBadRequest, "module required")
			return
		}
		mod = filepath.FromSlash(mod)
		if r.URL.Query().Get("format") == "csv" {
			f, err := os.Open(usage.Path(filepath.Join(root, mod)))
			if err != nil {
				jsonError(w, http.StatusNotFound, "no usage recorded yet")
				return
			}
			defer f.Close()
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", "attachment; filename=ctx-optimize-usage-"+mod+".csv")
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
		sum, err := usage.Summarize(filepath.Join(root, mod))
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sum)
	})

	mux.HandleFunc("/api/query", func(w http.ResponseWriter, r *http.Request) {
		s, ok := openModule(w, root, r.URL.Query().Get("module"))
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
		nodes, err := s.Nodes()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		edges, err := s.Edges()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, query.Run(nodes, edges, q, budget))
	})

	return mux
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
