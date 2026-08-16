package dashboard

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/muthuishere/ctx-optimize/internal/navigator"
	"github.com/muthuishere/ctx-optimize/internal/scene"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

// repoScene serves the MODULE-grain scene: the cards are a repo's modules and
// the arrows are the manifest declarations that join them (ADR 22 D4).
//
// Read route, same posture as handleScene: no token, no audit, and it never
// creates store layout for a name that does not exist.
func (s *server) handleRepoScene(w http.ResponseWriter, r *http.Request) {
	repo := store.SanitizeKeyPath(r.URL.Query().Get("repo"))
	if repo == "" || strings.Contains(repo, "/") {
		jsonError(w, http.StatusBadRequest, "repo must be a single store-root name")
		return
	}
	dir := filepath.Join(s.root, filepath.FromSlash(repo))
	idx, err := navigator.Load(dir)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if idx == nil {
		// Not a monorepo. Saying so is the answer: the caller falls back to the
		// module scene rather than showing an empty repo picture.
		jsonError(w, http.StatusNotFound, "no module index for "+repo+" — this is a single store, not a monorepo")
		return
	}

	opt := scene.Options{}
	if v, err := strconv.Atoi(r.URL.Query().Get("cards")); err == nil && v > 0 {
		opt.Cards = min(v, 24)
	}

	// The cache key must cover EVERY module store, not the repo root's: a
	// re-gather of one module leaves the root untouched, and keying on the root
	// alone would serve yesterday's picture of a repo that just changed.
	key := fmt.Sprintf("repo\x00%s\x00%d", repo, opt.Cards)
	stamp := repoStamp(s.root, idx)
	if sc, hit := s.scenes.get(key, stamp); hit {
		jsonOK(w, sc)
		return
	}

	mods := collectRepoModules(s.root, repo, idx)
	sc := scene.DeriveRepo(repo, mods, opt)
	s.scenes.put(key, stamp, sc)
	jsonOK(w, sc)
}

// repoStamp fingerprints every module graph in the repo. Sorted, so the same
// repo always produces the same stamp regardless of map iteration order.
func repoStamp(root string, idx *navigator.Index) string {
	keys := make([]string, 0, len(idx.Modules)+1)
	for _, m := range idx.Modules {
		keys = append(keys, m.Store)
	}
	keys = append(keys, idx.Root)
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		for _, f := range []string{"nodes.ndjson", "edges.ndjson"} {
			st, err := os.Stat(filepath.Join(root, filepath.FromSlash(k), "graph", f))
			if err != nil {
				b.WriteString(":-")
				continue
			}
			fmt.Fprintf(&b, ":%d-%d", st.Size(), st.ModTime().UnixNano())
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// collectRepoModules reads what the join needs and nothing else. Two streaming
// passes per module — never store.Nodes()/Edges(), which materialise the whole
// graph: the-factory is 229 modules and 318MB, and holding all of it in memory
// to answer "who declares whom" would cost gigabytes for a dozen arrows.
func collectRepoModules(root, repo string, idx *navigator.Index) []scene.RepoModule {
	out := make([]scene.RepoModule, len(idx.Modules))
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, m := range idx.Modules {
		wg.Add(1)
		go func(i int, m navigator.ModuleEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			rm := scene.RepoModule{
				Key: m.Store, Path: m.Path, Name: m.Name, Summary: m.Summary,
				Nodes: m.Nodes, Edges: m.Edges,
				Vendored: isVendoredPath(m.Path),
			}
			dir := filepath.Join(root, filepath.FromSlash(m.Store), "graph")
			okE := scanEdges(filepath.Join(dir, "edges.ndjson"), &rm)
			okN := scanPorts(filepath.Join(dir, "nodes.ndjson"), &rm)
			rm.Unread = !okE || !okN
			out[i] = rm
		}(i, m)
	}
	wg.Wait()
	return out
}

// vendorMarkers mirrors the manifest lane's list: a module under one of these
// is a copy of somebody else's package, not a product of this repo.
var vendorMarkers = []string{
	"vendor/", "third_party/", "thirdparty/", "node_modules/",
	"external/", "_vendor/", ".yarn/",
}

func isVendoredPath(p string) bool {
	p = strings.ToLower(strings.ReplaceAll(p, "\\", "/")) + "/"
	for _, m := range vendorMarkers {
		if strings.Contains(p, "/"+m) || strings.HasPrefix(p, m) {
			return true
		}
	}
	return false
}

// depEdge is the only part of an edge this join reads.
type depEdge struct {
	Source   string            `json:"source"`
	Target   string            `json:"target"`
	Relation string            `json:"relation"`
	Metadata map[string]string `json:"metadata"`
}

const scanBufMax = 8 * 1024 * 1024

// scanEdges pulls the module's published and declared package ids, and counts
// its code edges. The substring test is a REJECT filter and nothing more: a
// line that does not contain the word cannot be an edge with that relation,
// whatever order the JSON keys happen to be in. Surviving lines are parsed
// properly, so nothing here depends on field ordering.
func scanEdges(path string, rm *scene.RepoModule) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	pub, dec := []byte("publishes"), []byte("declares")
	imp, call := []byte(`"imports"`), []byte(`"calls"`)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), scanBufMax)
	seenPub, seenDec := map[string]bool{}, map[string]bool{}
	for sc.Scan() {
		line := sc.Bytes()
		if bytes.Contains(line, imp) || bytes.Contains(line, call) {
			rm.Code++
			continue
		}
		isPub, isDec := bytes.Contains(line, pub), bytes.Contains(line, dec)
		if !isPub && !isDec {
			continue
		}
		var e depEdge
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		if !strings.HasPrefix(e.Target, "dep:") {
			continue
		}
		switch e.Relation {
		case "publishes":
			// A vendored manifest names an UPSTREAM package. Letting it publish
			// would make third_party/goproxywss the owner of github.com/elazarl/
			// goproxy for the whole repo, and every real consumer of that package
			// would draw an arrow into a vendored copy.
			if e.Metadata["vendored"] == "true" {
				continue
			}
			if !seenPub[e.Target] {
				seenPub[e.Target] = true
				rm.Publishes = append(rm.Publishes, e.Target)
			}
		case "declares":
			if !seenDec[e.Target] {
				seenDec[e.Target] = true
				rm.Declares = append(rm.Declares, e.Target)
			}
		}
	}
	sort.Strings(rm.Publishes)
	sort.Strings(rm.Declares)
	return sc.Err() == nil
}

// portNode is the only part of a node this join reads.
type portNode struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// scanPorts pulls the module's port ids. Port ids are global, which is the one
// identity in the store that survives being gathered per module.
func scanPorts(path string, rm *scene.RepoModule) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	marker := []byte(`"port"`)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), scanBufMax)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, marker) {
			continue
		}
		var n portNode
		if json.Unmarshal(line, &n) != nil || n.Kind != "port" {
			continue
		}
		rm.Ports = append(rm.Ports, n.ID)
	}
	sort.Strings(rm.Ports)
	return sc.Err() == nil
}
