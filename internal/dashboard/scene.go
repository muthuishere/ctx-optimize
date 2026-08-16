package dashboard

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/muthuishere/ctx-optimize/internal/scene"
)

// handleScene serves the DERIVED architecture scene for one module — the data
// behind the Flow canvas viewer.
//
// It is a READ route and stays one: no token, no audit, no Ops closure, and
// openModule refuses a key with no store dir rather than creating layout for a
// typo (same posture as /api/graph and /api/query).
//
// It cannot leak a secret VALUE. The payload is built only from
// internal/scene.Scene, whose Door type carries a port's NAME plus two
// booleans; a port node's value is never in the store to begin with, and
// TestSceneEndpointHasNoValueKey walks the encoded JSON to keep it that way.
func (s *server) handleScene(w http.ResponseWriter, r *http.Request) {
	module := r.URL.Query().Get("module")
	st, ok := openModule(w, s.root, module)
	if !ok {
		return
	}
	opt := scene.Options{}
	if v, err := strconv.Atoi(r.URL.Query().Get("cards")); err == nil && v > 0 {
		opt.Cards = min(v, 12)
	}
	if r.URL.Query().Get("tests") == "1" {
		opt.IncludeTests = true
	}
	// `root` scopes the scene to one directory for drill-down. It is matched as
	// a string prefix against directories already in the store and never reaches
	// the filesystem, so it cannot traverse — but it is user input, so it is
	// bounded, and a root that matches nothing yields a scene that says so
	// rather than one that silently looks like the top level.
	if v := r.URL.Query().Get("root"); v != "" && len(v) <= 512 {
		opt.Root = v
	}

	// The cache is consulted BEFORE the graph is read, which is the whole point:
	// on linux, reading nodes and edges costs 2.4s and Derive another 3.2s, and
	// the viewer was paying all of it on every single load — including for a
	// drill-down that only changes `root`.
	//
	// A scene is a pure function of the graph ("same nodes and edges in,
	// byte-identical scene out"), so caching it cannot make it wrong; the only
	// question is when to throw it away, and the answer is when the graph files
	// change. Keyed on their size+mtime, exactly as the store-detail cache is.
	key, stamp, ok := sceneKey(st.Dir, module, opt)
	if ok {
		if sc, hit := s.scenes.get(key, stamp); hit {
			jsonOK(w, sc)
			return
		}
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
	sc := scene.Derive(module, nodes, edges, opt)
	if ok {
		s.scenes.put(key, stamp, sc)
	}
	jsonOK(w, sc)
}

// sceneKey identifies one derivation, and stamp identifies the graph it was
// derived from. Separating them is what lets a drill-down into a different
// root reuse nothing and yet invalidate together the moment a re-gather lands.
func sceneKey(dir, module string, opt scene.Options) (key, stamp string, ok bool) {
	n, err1 := os.Stat(filepath.Join(dir, "graph", "nodes.ndjson"))
	e, err2 := os.Stat(filepath.Join(dir, "graph", "edges.ndjson"))
	if err1 != nil || err2 != nil {
		return "", "", false // no graph on disk: derive every time, cache nothing
	}
	key = fmt.Sprintf("%s\x00%s\x00%d\x00%t", module, opt.Root, opt.Cards, opt.IncludeTests)
	stamp = fmt.Sprintf("%d-%d-%d-%d", n.Size(), n.ModTime().UnixNano(), e.Size(), e.ModTime().UnixNano())
	return key, stamp, true
}

// sceneCache holds derived scenes against the graph they came from. Bounded:
// a store has one scene per drilled level a user visits, and a session that
// wanders deep into a big monorepo should not grow memory without limit.
type sceneCache struct {
	mu    sync.Mutex
	m     map[string]sceneEntry
	stamp string // the graph stamp every entry belongs to
}

type sceneEntry struct {
	sc scene.Scene
}

const sceneCacheMax = 64

func newSceneCache() *sceneCache { return &sceneCache{m: map[string]sceneEntry{}} }

func (c *sceneCache) get(key, stamp string) (scene.Scene, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stamp != stamp {
		return scene.Scene{}, false
	}
	e, ok := c.m[key]
	return e.sc, ok
}

func (c *sceneCache) put(key, stamp string, sc scene.Scene) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// A new graph stamp means a re-gather landed: every scene derived from the
	// old graph is stale, and keeping any of it would show yesterday's picture.
	if c.stamp != stamp {
		c.m = map[string]sceneEntry{}
		c.stamp = stamp
	}
	if len(c.m) >= sceneCacheMax {
		for k := range c.m { // evict an arbitrary entry; all are equally cheap to rebuild
			delete(c.m, k)
			break
		}
	}
	c.m[key] = sceneEntry{sc: sc}
}
