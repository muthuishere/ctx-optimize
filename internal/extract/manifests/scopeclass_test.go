package manifests

import "testing"

func TestScopeClassVocabulary(t *testing.T) {
	cases := map[string]string{
		"dependencies":       "runtime",
		"devDependencies":    "dev",
		"peerDependencies":   "peer",
		"require":            "runtime",
		"indirect":           "indirect",
		"compile":            "runtime",
		"provided":           "build",
		"plugin":             "build",
		"parent":             "build",
		"implementation":     "runtime",
		"testImplementation": "test",
		"testRuntimeOnly":    "test",
		"test":               "test",
		"package":            "runtime",
		"weirdCustomScope":   "",
		// ADR 2026-07-25-structured-formats S7 requirement 5.
		"dev-dependencies":             "dev", // poetry/cargo/uv spelling
		"build-dependencies":           "build",
		"build-system":                 "build",
		"requirements":                 "runtime",
		"requirements-dev":             "dev",
		"requirements-test":            "test",
		"dependency-groups":            "dev",
		"dependency-groups:dev":        "dev",
		"dependency-groups:tests":      "test", // the fixed HasPrefix("test") hole
		"dependency-groups:test":       "test",
		"dependency-groups:docs":       "dev",
		"group:docs":                   "dev",
		"group:tests":                  "test",
		"optional-dependencies":        "optional",
		"optional-dependencies:async":  "optional",
		"target-dependencies":          "runtime",
		"target-dev-dependencies":      "dev",
		"workspace-build-dependencies": "build",
		"unknown-prefix:tests":         "", // unknown family stays unclassified
	}
	for scope, want := range cases {
		if got := scopeClass(scope); got != want {
			t.Errorf("scopeClass(%q) = %q, want %q", scope, got, want)
		}
	}
}

func TestDeclaresCarriesScopeClass(t *testing.T) {
	c := newCollector()
	id := c.depNode("npm", "typescript")
	c.declares("package.json", id, "^5", "devDependencies")
	if len(c.edges) != 1 {
		t.Fatalf("edges = %d", len(c.edges))
	}
	md := c.edges[0].Metadata
	if md["scope"] != "devDependencies" || md["scope_class"] != "dev" {
		t.Fatalf("metadata = %v", md)
	}
}

func TestApplyScopeAggregatesUnion(t *testing.T) {
	c := newCollector()
	ts := c.depNode("npm", "typescript")
	c.declares("a/package.json", ts, "^5", "devDependencies")
	react := c.depNode("npm", "react")
	c.declares("a/package.json", react, "^19", "dependencies")
	c.declares("b/package.json", react, "^19", "devDependencies")
	applyScopeAggregates(c)
	got := map[string]string{}
	for _, n := range c.nodes {
		got[n.ID] = n.Metadata["scopes"]
	}
	if got["dep:npm/typescript"] != "dev" {
		t.Errorf("typescript scopes = %q, want dev", got["dep:npm/typescript"])
	}
	if got["dep:npm/react"] != "dev,runtime" {
		t.Errorf("react scopes = %q, want dev,runtime (sorted union)", got["dep:npm/react"])
	}
}
