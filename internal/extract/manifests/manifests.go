// Package manifests is the tier-1 build-manifest producer: build-tool
// dependency declarations, declared tasks, and k8s topology as first-class
// graph, per openspec/changes/2026-07-14-manifest-lane. Its own producer
// batch ("manifests") with its own Replace lifecycle — dependency churn
// prunes independently of code/docs.
//
// Core recognizers (embedded, code-backed): package.json, pom.xml,
// *.csproj/*.sln, go.mod, build.gradle(.kts) line shapes, and k8s manifests
// via the shared yaml walker. Everything declarative beyond that is a
// MANIFEST PACK (packs.go). Node id namespaces are disjoint from file paths
// by construction: dep:<ns>/<name>, <file>::task:<name>, k8s://…, image:…
// — the markdown config lane keeps its shallow document+key indexing; this
// producer adds the semantic layer on top.
//
// Provenance discipline: in-the-file facts are EXTRACTED; anything matched
// by computation (k8s selector → deployment) is INFERRED + synthesized_by.
// Lockfiles (package-lock.json, yarn.lock, go.sum, Cargo.lock, *.lock) are
// data, not intent — skipped. Secret-smelling filenames are refused outright,
// same discipline as the markdown config lane.
package manifests

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/ignore"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

const ProducerName = "manifests"

// maxManifestBytes: a multi-MB "manifest" is generated data, not intent
// (same cap as the markdown config lane).
const maxManifestBytes = 256 * 1024

// collector dedups nodes (first emission wins — the same dep declared by two
// files is ONE node) and edges within the batch, keeping output deterministic.
type collector struct {
	nodes    []schema.Node
	edges    []schema.Edge
	seenNode map[string]bool
	seenEdge map[string]bool
}

func newCollector() *collector {
	return &collector{seenNode: map[string]bool{}, seenEdge: map[string]bool{}}
}

func (c *collector) node(n schema.Node) {
	if c.seenNode[n.ID] {
		return
	}
	c.seenNode[n.ID] = true
	c.nodes = append(c.nodes, n)
}

func (c *collector) edge(e schema.Edge) {
	key := e.Source + "\x00" + e.Target + "\x00" + e.Relation + "\x00" +
		e.Metadata["version_spec"] + "\x00" + e.Metadata["scope"]
	if c.seenEdge[key] {
		return
	}
	c.seenEdge[key] = true
	c.edges = append(c.edges, e)
}

// depNode emits (idempotently) one shared dependency node. The version lives
// on the declares EDGE, never in the id — the same lib at two versions is one
// node with two edges carrying their own version metadata.
func (c *collector) depNode(namespace, name string) string {
	id := "dep:" + namespace + "/" + name
	c.node(schema.Node{
		ID: id, Label: name, Kind: "dependency", FileType: "manifest",
		Source:   "dep://" + namespace + "/" + name,
		Metadata: map[string]string{"ecosystem": namespace},
	})
	return id
}

// declares emits the manifest-file → dependency edge (EXTRACTED — it is in
// the file), carrying version_spec and scope.
func (c *collector) declares(rel, depID, versionSpec, scope string) {
	md := map[string]string{}
	if versionSpec != "" {
		md["version_spec"] = versionSpec
	}
	if scope != "" {
		md["scope"] = scope
	}
	c.edge(schema.Edge{
		Source: rel, Target: depID, Relation: "declares",
		Confidence: schema.Extracted, Metadata: md,
	})
}

// Extract walks root and emits one batch covering every recognized manifest.
func Extract(root string) (*schema.Batch, error) { return ExtractExcluding(root, nil) }

// ExtractExcluding is Extract with subtrees pruned — the multi-module root
// residual (same contract as the markdown/code producers).
func ExtractExcluding(root string, exclude []string) (*schema.Batch, error) {
	packs, err := LoadManifestPacks(root)
	if err != nil {
		return nil, err // malformed packs fail the add loudly
	}
	skip := map[string]bool{}
	for _, e := range exclude {
		if abs, err := filepath.Abs(e); err == nil {
			skip[abs] = true
		}
	}
	ignored := ignore.New(root) // .gitignore semantics via git itself; nil = no git
	c := newCollector()
	k8s := newK8sState()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if ignored != nil {
			if rel, rerr := filepath.Rel(root, path); rerr == nil && rel != "." && ignored(filepath.ToSlash(rel)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() {
			if len(skip) > 0 {
				if abs, aerr := filepath.Abs(path); aerr == nil && skip[abs] {
					return filepath.SkipDir
				}
			}
			// Same prune list as the markdown/code walks: hidden dirs, build
			// output, vendored trees — a manifest inside dist/ is generated.
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" ||
				name == "vendor" || name == "target" || name == "dist" || name == "build" ||
				strings.HasSuffix(name, "-out")) {
				return filepath.SkipDir
			}
			return nil
		}
		if secretName(name) || isLockfile(name) {
			return nil // refusal discipline gates core AND pack lanes
		}
		kind := manifestKind(name)
		packRules := matchPackRules(packs, name)
		if kind == "" && len(packRules) == 0 {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil && info.Size() > maxManifestBytes {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return fmt.Errorf("read %s: %w", rel, rerr)
		}
		content := string(data)
		switch kind {
		case "npm":
			extractPackageJSON(c, root, rel, data)
		case "taskfile":
			extractTaskfile(c, rel, data)
		case "makefile":
			extractMakefile(c, rel, data)
		case "justfile":
			extractJustfile(c, rel, data)
		case "pom":
			extractPomXML(c, rel, data)
		case "csproj":
			extractCsproj(c, rel, data)
		case "sln":
			extractSln(c, rel, content)
		case "gomod":
			extractGoMod(c, rel, content)
		case "gradle":
			extractGradle(c, rel, content)
		case "yaml":
			extractK8s(k8s, rel, content)
		}
		for _, pr := range packRules {
			applyPackRule(c, pr, rel, data)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	k8s.emit(c)
	b := &schema.Batch{Producer: ProducerName, Nodes: c.nodes, Edges: c.edges}
	sort.Slice(b.Nodes, func(i, j int) bool { return b.Nodes[i].ID < b.Nodes[j].ID })
	sort.Slice(b.Edges, func(i, j int) bool {
		a, z := b.Edges[i], b.Edges[j]
		if a.Source != z.Source {
			return a.Source < z.Source
		}
		if a.Target != z.Target {
			return a.Target < z.Target
		}
		return a.Relation < z.Relation
	})
	return b, nil
}

// lockfileNames are data, not intent — never parsed (most exceed the size
// cap anyway; the ones that don't still describe resolutions, not decisions).
var lockfileNames = map[string]bool{
	"package-lock.json": true, "npm-shrinkwrap.json": true, "yarn.lock": true,
	"pnpm-lock.yaml": true, "pnpm-lock.yml": true, "go.sum": true,
	"cargo.lock": true, "composer.lock": true, "gemfile.lock": true,
	"poetry.lock": true, "bun.lock": true,
}

// secretName refuses secret-smelling filenames outright — the knowledge
// graph must never become the place credentials leak from (markdown-lane
// discipline). Note: a k8s `kind: Secret` RESOURCE in a neutrally-named
// file still gets a node (identity only, data never read) — see k8s.go.
func secretName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "secret") || strings.Contains(lower, "credential") ||
		strings.HasPrefix(lower, ".env") || strings.Contains(lower, "password")
}

func isLockfile(name string) bool {
	lower := strings.ToLower(name)
	return lockfileNames[lower] || strings.HasSuffix(lower, ".lock")
}

// manifestKind classifies a basename into a core recognizer channel.
func manifestKind(name string) string {
	lower := strings.ToLower(name)
	switch lower {
	case "package.json":
		return "npm"
	case "makefile", "gnumakefile":
		return "makefile"
	case "justfile", ".justfile":
		return "justfile"
	case "pom.xml":
		return "pom"
	case "go.mod":
		return "gomod"
	case "build.gradle", "build.gradle.kts":
		return "gradle"
	}
	if strings.HasPrefix(lower, "taskfile.") &&
		(strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")) {
		return "taskfile"
	}
	switch filepath.Ext(lower) {
	case ".csproj":
		return "csproj"
	case ".sln":
		return "sln"
	case ".yaml", ".yml":
		return "yaml" // k8s candidate — extractK8s decides doc-by-doc
	}
	return ""
}
