// identity.go — what a module IS, not only what it needs.
//
// ADR 22, D0. Every join between two modules of a monorepo fails on the same
// missing half: the store records the CONSUMES side of everything and never
// the module's own identity. `dep:npm/@mastra/core` is a global node id and
// appears in every module that depends on it — but nothing says which module
// PUBLISHES it, so the edge that would connect them cannot be built.
//
// Measured before writing this (scripts/spikes/monorepo-links.py, 30
// multi-module repos): recording identity would resolve 2,168 directed
// module→module dependency links. The same spike found ZERO observable
// http/ws/process links, because those joins are missing this same half.
//
// The emission is deliberately minimal: the SAME `dep:<ns>/<name>` node the
// consumer already declares, plus a `publishes` edge from the manifest that
// names it. So "who publishes X" is `edges --relation publishes`, "who needs
// X" is `--relation declares`, and the cross-module join is those two meeting
// on one node id. No new node kind, no new producer, nothing for `deps` to
// misread.
package manifests

import (
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// vendorMarkers are path segments that mean "this manifest describes somebody
// else's package that happens to live in our tree". Without this, a vendored
// copy of an upstream library reads as an intra-product module: agent-proxy's
// four cross-module links are all `third_party/goproxywss` declaring
// `github.com/elazarl/goproxy`, which is a real dependency on a real vendored
// copy but is not a sibling product.
var vendorMarkers = []string{
	"vendor/", "third_party/", "thirdparty/", "node_modules/",
	"external/", "_vendor/", ".yarn/",
}

func isVendored(rel string) bool {
	p := strings.ToLower(strings.ReplaceAll(rel, "\\", "/"))
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	for _, m := range vendorMarkers {
		if strings.HasPrefix(p, m) || strings.Contains(p, "/"+m) {
			return true
		}
	}
	return false
}

// publishes records that the manifest at `rel` declares this module to BE the
// package `name` in `namespace`. Emitted as EXTRACTED because it is written in
// the file — it is a declaration, not a name match, which is exactly why this
// beats the HTTP join it replaces at the top of ADR 22.
func (c *collector) publishes(namespace, name, rel string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	id := c.depNode(namespace, name)
	md := map[string]string{"ecosystem": namespace, "package": name}
	if isVendored(rel) {
		// still recorded — the fact is true — but marked, so a count of
		// intra-product links can exclude it instead of flattering itself
		md["vendored"] = "true"
	}
	c.edge(schema.Edge{
		Source: rel, Target: id, Relation: "publishes",
		Confidence: schema.Extracted, Metadata: md,
	})
}
