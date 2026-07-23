// scopeclass.go — one scope vocabulary across ecosystems (ADR 2026-07-23-
// code-dependency-edges, move 2): the raw manifest section name stays the
// authority on the declares edge; scope_class adds the normalized
// runtime|dev|peer|optional|test|build|indirect a consumer can filter on
// without knowing five ecosystems' section names. Unknown scopes get no
// class — absent beats wrong.
package manifests

import (
	"sort"
	"strings"
)

var scopeClasses = map[string]string{
	// npm sections
	"dependencies":     "runtime",
	"devDependencies":  "dev",
	"peerDependencies": "peer",
	// go.mod
	"require":  "runtime",
	"indirect": "indirect",
	// maven scopes (+ our synthetic parent/plugin)
	"compile":  "runtime",
	"provided": "build",
	"system":   "build",
	"import":   "build",
	"parent":   "build",
	"plugin":   "build",
	// gradle configurations (test* handled by prefix below)
	"implementation":      "runtime",
	"api":                 "runtime",
	"runtimeOnly":         "runtime",
	"compileOnly":         "build",
	"annotationProcessor": "build",
	"kapt":                "build",
	"developmentOnly":     "dev",
	// nuget
	"package": "runtime",
	// shared literals
	"runtime":  "runtime",
	"test":     "test",
	"optional": "optional",
}

func scopeClass(scope string) string {
	if c, ok := scopeClasses[scope]; ok {
		return c
	}
	// gradle testImplementation/testRuntimeOnly/…, maven test — one family.
	if strings.HasPrefix(strings.ToLower(scope), "test") {
		return "test"
	}
	return ""
}

// applyScopeAggregates mirrors the per-declaration classes onto the dep node
// as metadata "scopes" — the sorted, comma-joined union (move 3). The EDGE
// stays the authority; the node field is the one-look convenience for
// consumers filtering dep nodes without walking edges.
func applyScopeAggregates(c *collector) {
	union := map[string]map[string]bool{}
	for _, e := range c.edges {
		if e.Relation != "declares" {
			continue
		}
		cls := e.Metadata["scope_class"]
		if cls == "" {
			continue
		}
		if union[e.Target] == nil {
			union[e.Target] = map[string]bool{}
		}
		union[e.Target][cls] = true
	}
	for i, n := range c.nodes {
		set := union[n.ID]
		if n.Kind != "dependency" || len(set) == 0 {
			continue
		}
		classes := make([]string, 0, len(set))
		for cls := range set {
			classes = append(classes, cls)
		}
		sort.Strings(classes)
		if c.nodes[i].Metadata == nil {
			c.nodes[i].Metadata = map[string]string{}
		}
		c.nodes[i].Metadata["scopes"] = strings.Join(classes, ",")
	}
}
