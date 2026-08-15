package dashboard

import (
	"net/http"
	"strconv"

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
	opt := scene.Options{}
	if v, err := strconv.Atoi(r.URL.Query().Get("cards")); err == nil && v > 0 {
		opt.Cards = min(v, 12)
	}
	if r.URL.Query().Get("tests") == "1" {
		opt.IncludeTests = true
	}
	jsonOK(w, scene.Derive(r.URL.Query().Get("module"), nodes, edges, opt))
}
