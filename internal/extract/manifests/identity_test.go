package manifests

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// extractOne runs the real extractor over a one-file tree.
func extractOne(t *testing.T, rel, content string) ([]schema.Node, []schema.Edge) {
	t.Helper()
	b := extractFixture(t, map[string]string{rel: content})
	return b.Nodes, b.Edges
}

// publishedBy returns what a manifest declares this module to BE.
func publishedBy(nodes []schema.Node, edges []schema.Edge, rel string) []schema.Edge {
	var out []schema.Edge
	for _, e := range edges {
		if e.Relation == "publishes" && e.Source == rel {
			out = append(out, e)
		}
	}
	_ = nodes
	return out
}

// TestManifestRecordsWhatTheModuleIS is ADR 22 D0. Every cross-module join in
// a monorepo failed on the same missing half: the store recorded what a module
// CONSUMES and never its own identity, so `dep:npm/@acme/core` — a global node
// id present in every module that needs it — could not be resolved to the
// module that publishes it. Measured on 30 multi-module repos, recording this
// resolves 2,168 directed module→module links.
func TestManifestRecordsWhatTheModuleIS(t *testing.T) {
	for _, tc := range []struct {
		name, file, content, ns, want string
	}{
		{"npm", "package.json",
			`{"name":"@acme/core","dependencies":{"lodash":"^4"}}`, "npm", "@acme/core"},
		{"go", "go.mod",
			"module github.com/acme/core\n\ngo 1.22\n\nrequire (\n\tgithub.com/x/y v1.0.0\n)\n", "go", "github.com/acme/core"},
		{"cargo", "Cargo.toml",
			"[package]\nname = \"acme-core\"\nversion = \"0.1.0\"\n\n[dependencies]\nserde = \"1\"\n", "crates", "acme-core"},
		{"pyproject", "pyproject.toml",
			"[project]\nname = \"acme-core\"\ndependencies = [\"requests\"]\n", "pypi", "acme-core"},
		{"poetry", "pyproject.toml",
			"[tool.poetry]\nname = \"acme-core\"\n", "pypi", "acme-core"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes, edges := extractOne(t, tc.file, tc.content)
			pubs := publishedBy(nodes, edges, tc.file)
			if len(pubs) != 1 {
				t.Fatalf("%d publishes edges, want 1: %+v", len(pubs), pubs)
			}
			wantTarget := "dep:" + tc.ns + "/" + tc.want
			if pubs[0].Target != wantTarget {
				t.Errorf("publishes -> %q, want %q", pubs[0].Target, wantTarget)
			}
			// EXTRACTED, not INFERRED: it is written in the file. That is the
			// whole reason this beats the HTTP join it replaced.
			if pubs[0].Confidence != schema.Extracted {
				t.Errorf("confidence = %v, want EXTRACTED", pubs[0].Confidence)
			}
			// the node it points at is the SAME id a consumer declares, which
			// is what makes the cross-module join a lookup rather than a match
			var found bool
			for _, n := range nodes {
				if n.ID == wantTarget && n.Kind == "dependency" {
					found = true
				}
			}
			if !found {
				t.Errorf("no dependency node %q for a consumer to meet", wantTarget)
			}
			if pubs[0].Metadata["vendored"] != "" {
				t.Errorf("a manifest at the repo root is not vendored: %v", pubs[0].Metadata)
			}
		})
	}
}

// TestVendoredIdentityIsMarked — agent-proxy's four "cross-module" links are
// all a third_party copy declaring its upstream path. That is a true
// dependency on a real vendored copy and NOT a sibling product.
//
// Two mechanisms handle it, and the difference is worth pinning: `vendor/` and
// `node_modules/` are PRUNED by the walker, so nothing is recorded at all —
// which is stronger than marking. `third_party/` and `external/` are walked,
// which is exactly why agent-proxy's links exist, so those are recorded AND
// flagged rather than dropped: the dependency is real, it just is not a
// sibling product.
func TestVendoredIdentityIsMarked(t *testing.T) {
	for _, rel := range []string{
		"third_party/goproxy/go.mod",
		"external/agentwire/go.mod",
	} {
		nodes, edges := extractOne(t, rel, "module github.com/elazarl/goproxy\n")
		pubs := publishedBy(nodes, edges, rel)
		if len(pubs) != 1 {
			t.Fatalf("%s: %d publishes edges, want 1", rel, len(pubs))
		}
		if pubs[0].Metadata["vendored"] != "true" {
			t.Errorf("%s: not marked vendored (%v) — a vendored upstream would "+
				"read as a sibling product", rel, pubs[0].Metadata)
		}
	}

	// pruned trees never reach the extractor, which is the stronger guarantee
	for _, rel := range []string{
		"vendor/github.com/x/y/go.mod",
		"apps/cli/node_modules/left-pad/package.json",
	} {
		content := "module github.com/x/y\n"
		if strings.HasSuffix(rel, "package.json") {
			content = `{"name":"left-pad"}`
		}
		_, edges := extractOne(t, rel, content)
		if p := publishedBy(nil, edges, rel); len(p) != 0 {
			t.Errorf("%s: pruned tree produced %d publishes edges", rel, len(p))
		}
	}

	// and a normal module is NOT marked
	_, edges := extractOne(t, "apps/api/package.json", `{"name":"@acme/api"}`)
	p := publishedBy(nil, edges, "apps/api/package.json")
	if len(p) != 1 {
		t.Fatalf("apps/api: %d publishes edges, want 1", len(p))
	}
	if p[0].Metadata["vendored"] == "true" {
		t.Error("apps/api marked vendored — the marker is matching too widely")
	}
}

// TestIdentityIsNotConfusedWithADependency — a `name` key inside a dependency
// table is not the module's own name. This is the failure mode the
// table-anchored parsers exist to prevent, and it would attribute a sibling's
// identity to the wrong module.
func TestIdentityIsNotConfusedWithADependency(t *testing.T) {
	_, edges := extractOne(t, "Cargo.toml",
		"[package]\nname = \"real-crate\"\n\n[dependencies.serde]\nname = \"not-the-crate\"\nversion = \"1\"\n")
	pubs := publishedBy(nil, edges, "Cargo.toml")
	if len(pubs) != 1 || pubs[0].Target != "dep:crates/real-crate" {
		t.Errorf("identity from a dependency sub-table: %+v", pubs)
	}
}
