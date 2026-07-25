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
	// pypi (pyproject + requirements) and crates — ADR 2026-07-25 S7. Note
	// `dev-dependencies` (poetry/cargo/uv) is spelled differently from npm's
	// `devDependencies`; both are needed.
	"dev-dependencies":      "dev",
	"build-dependencies":    "build",
	"build-system":          "build",
	"optional-dependencies": "optional",
	"dependency-groups":     "dev",
	"group":                 "dev",
	"requirements":          "runtime",
	"requirements-dev":      "dev",
	"requirements-test":     "test",
	// shared literals
	"runtime":  "runtime",
	"test":     "test",
	"optional": "optional",
}

func scopeClass(scope string) string {
	if c, ok := scopeClasses[scope]; ok {
		return c
	}
	// Qualified scopes carry the group/extra after a colon:
	// `dependency-groups:tests`, `optional-dependencies:async`, poetry
	// `group:docs`. The prefix decides the family; a dev family whose group is
	// a test group is a TEST scope. Without this, `dependency-groups:tests` fell
	// through to the HasPrefix("test") rule below — which never fires, because
	// the prefix is "dependency" — and silently got no class at all.
	if i := strings.IndexByte(scope, ':'); i > 0 {
		base := scopeClass(scope[:i])
		if base == "dev" && strings.HasPrefix(strings.ToLower(scope[i+1:]), "test") {
			return "test"
		}
		return base
	}
	// Cargo's `[workspace.…]` / `[target."cfg(…)".…]` sections declare the same
	// kinds of dependency as the bare section — same class, different location.
	for _, p := range []string{"workspace-", "target-"} {
		if strings.HasPrefix(scope, p) {
			return scopeClass(strings.TrimPrefix(scope, p))
		}
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
		joined := strings.Join(classes, ",")
		if c.nodes[i].Metadata == nil {
			c.nodes[i].Metadata = map[string]string{}
		}
		c.nodes[i].Metadata["scopes"] = joined // back-compat
		c.nodes[i].Scope = joined              // F1: top-level, the field consumers reach for first
	}
}
