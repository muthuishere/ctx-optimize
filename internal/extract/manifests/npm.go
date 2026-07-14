// npm.go — package.json recognizer (stdlib encoding/json):
// dependencies/devDependencies/peerDependencies → dep:npm/<name> nodes with
// file --declares--> dep edges (version_spec + scope in edge metadata),
// scripts → <file>::task:<name> nodes (label npm:<name>, line-anchored
// best-effort), workspaces globs → depends_on edges to member manifests.
package manifests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

type packageJSON struct {
	Dependencies     map[string]string `json:"dependencies"`
	DevDependencies  map[string]string `json:"devDependencies"`
	PeerDependencies map[string]string `json:"peerDependencies"`
	Scripts          map[string]string `json:"scripts"`
	Workspaces       json.RawMessage   `json:"workspaces"`
}

func extractPackageJSON(c *collector, root, rel string, data []byte) {
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return // malformed user json: not our error to raise — skip silently
	}
	lines := strings.Split(string(data), "\n")
	for _, scope := range []struct {
		name string
		deps map[string]string
	}{
		{"dependencies", pkg.Dependencies},
		{"devDependencies", pkg.DevDependencies},
		{"peerDependencies", pkg.PeerDependencies},
	} {
		names := make([]string, 0, len(scope.deps))
		for n := range scope.deps {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			id := c.depNode("npm", n)
			c.declares(rel, id, scope.deps[n], scope.name)
		}
	}

	// Scripts: one task node each, line-anchored best-effort (the first line
	// carrying the quoted key — good enough for a hand-written manifest).
	scriptNames := make([]string, 0, len(pkg.Scripts))
	for n := range pkg.Scripts {
		scriptNames = append(scriptNames, n)
	}
	sort.Strings(scriptNames)
	for _, n := range scriptNames {
		id := rel + "::task:" + n
		c.node(schema.Node{
			ID: id, Label: "npm:" + n, Kind: "task", FileType: "manifest",
			Source: rel, Location: lineOfQuotedKey(lines, n),
			Metadata: map[string]string{"command": pkg.Scripts[n]},
		})
		c.edge(schema.Edge{Source: rel, Target: id, Relation: "contains", Confidence: schema.Extracted})
	}

	// Workspaces: ["packages/*"] or {"packages": ["packages/*"]} — expand
	// against the filesystem; a member is a dir carrying its own package.json.
	for _, member := range expandWorkspaces(root, rel, pkg.Workspaces) {
		c.edge(schema.Edge{
			Source: rel, Target: member, Relation: "depends_on",
			Confidence: schema.Extracted, Metadata: map[string]string{"via": "npm-workspace"},
		})
	}
}

// lineOfQuotedKey finds the first line containing `"key"` followed by a
// colon; "L1" when not found (best-effort anchoring, never wrong-file).
func lineOfQuotedKey(lines []string, key string) string {
	needle := `"` + key + `"`
	for i, l := range lines {
		if idx := strings.Index(l, needle); idx >= 0 &&
			strings.HasPrefix(strings.TrimSpace(l[idx+len(needle):]), ":") {
			return fmt.Sprintf("L%d", i+1)
		}
	}
	return "L1"
}

func expandWorkspaces(root, rel string, raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var globs []string
	if err := json.Unmarshal(raw, &globs); err != nil {
		var obj struct {
			Packages []string `json:"packages"`
		}
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil
		}
		globs = obj.Packages
	}
	baseDir := filepath.Dir(filepath.Join(root, filepath.FromSlash(rel)))
	var members []string
	for _, g := range globs {
		matches, err := filepath.Glob(filepath.Join(baseDir, filepath.FromSlash(g)))
		if err != nil {
			continue
		}
		for _, m := range matches {
			manifest := filepath.Join(m, "package.json")
			if _, err := os.Stat(manifest); err != nil {
				continue
			}
			if r, err := filepath.Rel(root, manifest); err == nil {
				members = append(members, filepath.ToSlash(r))
			}
		}
	}
	sort.Strings(members)
	return members
}
