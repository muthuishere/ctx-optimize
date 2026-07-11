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

	"github.com/muthuishere/ctx-optimize/internal/query"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

//go:embed index.html
var indexHTML []byte

// Module is one store folder as listed by /api/modules.
type Module struct {
	Key   string `json:"key"`
	Nodes int    `json:"nodes"`
	Edges int    `json:"edges"`
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
		entries, err := os.ReadDir(root)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		mods := []Module{}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			nodesPath := filepath.Join(root, e.Name(), "graph", "nodes.ndjson")
			if _, err := os.Stat(nodesPath); err != nil {
				continue // not a store folder
			}
			mods = append(mods, Module{
				Key:   e.Name(),
				Nodes: countLines(nodesPath),
				Edges: countLines(filepath.Join(root, e.Name(), "graph", "edges.ndjson")),
			})
		}
		sort.Slice(mods, func(i, j int) bool { return mods[i].Key < mods[j].Key })
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

// openModule validates the module exists (read-only: never creates layout for
// a typo'd name) before opening it.
func openModule(w http.ResponseWriter, root, key string) (*store.Store, bool) {
	if key == "" {
		jsonError(w, http.StatusBadRequest, "missing module parameter")
		return nil, false
	}
	key = store.SanitizeKey(key)
	if _, err := os.Stat(filepath.Join(root, key, "graph")); err != nil {
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
