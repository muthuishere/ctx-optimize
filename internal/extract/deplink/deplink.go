// Package deplink is the cross-lane linker (ADR 2026-07-23-code-dependency-
// edges): it bridges the code lane's module:// import targets to the manifest
// lane's dep: nodes with resolves_to edges, so the graph answers "which files
// use package X" and `affected dep:npm/react` crosses the dependency
// boundary. Its own producer with its own Replace lifecycle — link churn
// prunes independently of both source lanes, neither of which it touches.
//
// Resolution is exact where the specifier IS the package name (npm, go) and
// unambiguous-prefix elsewhere (maven, nuget); ambiguous candidates are
// dropped, not guessed — the calls discipline. All links are INFERRED +
// synthesized_by (matched by computation across two files).
package deplink

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

const ProducerName = "deplink"

const modulePrefix = "module://"

// nodeBuiltins: specifiers the js/ts lanes import from the node runtime, not
// from a package. The node: scheme is authoritative; the bare names cover the
// legacy spelling of the common ones.
var nodeBuiltins = map[string]bool{
	"assert": true, "buffer": true, "child_process": true, "crypto": true,
	"events": true, "fs": true, "http": true, "https": true, "net": true,
	"os": true, "path": true, "readline": true, "stream": true, "tls": true,
	"url": true, "util": true, "worker_threads": true, "zlib": true,
}

// Link computes the resolves_to batch from one gather's code and manifests
// batches. goSelf holds the repo's own go.mod module paths — self-imports are
// internal edges already (spike finding 1), never dependency links.
func Link(codeB, manifestB *schema.Batch, goSelf []string) *schema.Batch {
	b := &schema.Batch{Producer: ProducerName}
	if codeB == nil || manifestB == nil {
		return b
	}

	npm := map[string]string{}   // package name -> dep node id
	var goMods []string          // module paths, longest first
	goIDs := map[string]string{} // module path -> dep node id
	var mavenGroups []mavenDep   // groupId prefixes
	var nugetIDs []nugetDep      // package ids
	for _, n := range manifestB.Nodes {
		rest, ok := strings.CutPrefix(n.ID, "dep:")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(rest, "npm/"):
			npm[rest[len("npm/"):]] = n.ID
		case strings.HasPrefix(rest, "go/"):
			mod := rest[len("go/"):]
			goMods = append(goMods, mod)
			goIDs[mod] = n.ID
		case strings.HasPrefix(rest, "maven/"):
			// dep:maven/<groupId>:<artifactId> — java imports match on groupId.
			if g, _, ok := strings.Cut(rest[len("maven/"):], ":"); ok {
				mavenGroups = append(mavenGroups, mavenDep{group: g, id: n.ID})
			}
		case strings.HasPrefix(rest, "nuget/"):
			nugetIDs = append(nugetIDs, nugetDep{pkg: rest[len("nuget/"):], id: n.ID})
		}
	}
	sort.Slice(goMods, func(i, j int) bool { return len(goMods[i]) > len(goMods[j]) })

	seen := map[string]bool{}
	resolved := map[string]bool{} // module:// id -> did it resolve to a dep?
	for _, n := range codeB.Nodes {
		spec, ok := strings.CutPrefix(n.ID, modulePrefix)
		if !ok || spec == "" {
			continue
		}
		depID, ecosystem := resolve(spec, npm, goMods, goIDs, goSelf, mavenGroups, nugetIDs)
		if depID == "" || seen[n.ID+"\x00"+depID] {
			continue
		}
		resolved[n.ID] = true
		seen[n.ID+"\x00"+depID] = true
		b.Edges = append(b.Edges, schema.Edge{
			Source: n.ID, Target: depID, Relation: "resolves_to",
			Confidence: schema.Inferred,
			Metadata:   map[string]string{"ecosystem": ecosystem, "synthesized_by": ProducerName},
		})
	}

	// F2 — undeclared-dependency drift (ADR 2026-07-23 follow-up): a SCOPED
	// npm import (@scope/pkg) that resolves to no declared dep is almost always
	// a workspace-internal package imported without being declared in this
	// module's manifest — the reporter's "undeclared integration" signal.
	// Scoped-only keeps false positives near zero. Emitted as a queryable
	// node + file edges (nodes --kind undeclared_dependency / edges --relation
	// undeclared_dependency). npm context only (len(npm) > 0).
	if len(npm) > 0 {
		linkUndeclared(b, codeB, resolved)
	}
	sort.Slice(b.Edges, func(i, j int) bool {
		if b.Edges[i].Source != b.Edges[j].Source {
			return b.Edges[i].Source < b.Edges[j].Source
		}
		return b.Edges[i].Target < b.Edges[j].Target
	})
	return b
}

// linkUndeclared flags scoped-npm imports with no resolved dependency and
// wires each importing file to a shared undeclared_dependency node.
func linkUndeclared(b *schema.Batch, codeB *schema.Batch, resolved map[string]bool) {
	// Which scoped module specs are undeclared?
	undeclared := map[string]string{} // module:// id -> undeclared node id
	for _, n := range codeB.Nodes {
		spec, ok := strings.CutPrefix(n.ID, modulePrefix)
		if !ok || !strings.HasPrefix(spec, "@") || resolved[n.ID] {
			continue
		}
		name := npmName(spec) // @scope/pkg
		id := "undeclared:npm/" + name
		if _, done := undeclared[n.ID]; !done {
			undeclared[n.ID] = id
		}
	}
	if len(undeclared) == 0 {
		return
	}
	// One node per undeclared package (dedup by id).
	nodeSeen := map[string]bool{}
	for _, id := range undeclared {
		if nodeSeen[id] {
			continue
		}
		nodeSeen[id] = true
		name := strings.TrimPrefix(id, "undeclared:npm/")
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: name, Kind: "undeclared_dependency", FileType: "manifest",
			Source:   "deplink://undeclared/npm/" + name,
			Metadata: map[string]string{"ecosystem": "npm", "producer": ProducerName, "synthesized_by": ProducerName},
		})
	}
	// file --undeclared_dependency--> undeclared node, from the imports edges.
	edgeSeen := map[string]bool{}
	for _, e := range codeB.Edges {
		if e.Relation != "imports" {
			continue
		}
		id, ok := undeclared[e.Target]
		if !ok {
			continue
		}
		key := e.Source + "\x00" + id
		if edgeSeen[key] {
			continue
		}
		edgeSeen[key] = true
		b.Edges = append(b.Edges, schema.Edge{
			Source: e.Source, Target: id, Relation: "undeclared_dependency",
			Confidence: schema.Inferred,
			Metadata:   map[string]string{"ecosystem": "npm", "synthesized_by": ProducerName},
		})
	}
	sortNodes(b.Nodes)
}

func sortNodes(ns []schema.Node) {
	sort.Slice(ns, func(i, j int) bool { return ns[i].ID < ns[j].ID })
}

type mavenDep struct{ group, id string }
type nugetDep struct{ pkg, id string }

func resolve(spec string, npm map[string]string, goMods []string, goIDs map[string]string,
	goSelf []string, maven []mavenDep, nuget []nugetDep) (depID, ecosystem string) {
	if strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") {
		return "", "" // relative — internal by definition
	}
	if strings.HasPrefix(spec, "node:") || nodeBuiltins[spec] {
		return "", ""
	}
	// npm: strip the subpath; the remainder IS the package name. Exact match.
	if len(npm) > 0 {
		if id := npm[npmName(spec)]; id != "" {
			return id, "npm"
		}
	}
	// go: self-module first (skip), then longest-prefix against declared mods.
	for _, self := range goSelf {
		if spec == self || strings.HasPrefix(spec, self+"/") {
			return "", ""
		}
	}
	for _, mod := range goMods {
		if spec == mod || strings.HasPrefix(spec, mod+"/") {
			return goIDs[mod], "go"
		}
	}
	// maven (java imports, dot-separated): groupId prefix, unambiguous only.
	if id := unambiguous(spec, maven, func(d mavenDep) string { return d.group }, func(d mavenDep) string { return d.id }); id != "" {
		return id, "maven"
	}
	// nuget (c# usings, dot-separated): package-id prefix, unambiguous only.
	if id := unambiguous(spec, nuget, func(d nugetDep) string { return d.pkg }, func(d nugetDep) string { return d.id }); id != "" {
		return id, "nuget"
	}
	return "", ""
}

// npmName strips the subpath: react-dom/client → react-dom,
// @scope/pkg/sub → @scope/pkg.
func npmName(spec string) string {
	parts := strings.SplitN(spec, "/", 3)
	if strings.HasPrefix(spec, "@") && len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// unambiguous links a dot-separated specifier to a prefix-matching dep ONLY
// when exactly one matches — two candidates mean we drop, never guess.
func unambiguous[T any](spec string, deps []T, key func(T) string, id func(T) string) string {
	var found string
	for _, d := range deps {
		k := key(d)
		if k == "" {
			continue
		}
		if spec == k || strings.HasPrefix(spec, k+".") {
			if found != "" && found != id(d) {
				return "" // ambiguous
			}
			found = id(d)
		}
	}
	return found
}

// GoModulePaths reads the `module` directive from base's and each dir's
// go.mod — the self-set the linker must never link against.
func GoModulePaths(base string, dirs []string) []string {
	seen := map[string]bool{}
	var out []string
	paths := []string{filepath.Join(base, "go.mod")}
	for _, d := range dirs {
		paths = append(paths, filepath.Join(base, d, "go.mod"))
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if mod, ok := strings.CutPrefix(line, "module "); ok {
				mod = strings.TrimSpace(mod)
				if mod != "" && !seen[mod] {
					seen[mod] = true
					out = append(out, mod)
				}
				break
			}
		}
	}
	sort.Strings(out)
	return out
}
