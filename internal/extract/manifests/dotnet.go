// dotnet.go — *.csproj (stdlib encoding/xml) and *.sln (line-parsed)
// recognizers. PackageReference → dep:nuget/<Include> (Version attribute or
// child element); ProjectReference → depends_on edges between project files
// with repo-relative resolved paths (backslashes normalized — the .NET module
// graph for free). Since no other producer walks these files, the csproj/sln
// file itself gets a config node here so its edges have a real anchor.
package manifests

import (
	"encoding/xml"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

type csprojDoc struct {
	ItemGroups []struct {
		PackageReferences []struct {
			Include     string `xml:"Include,attr"`
			VersionAttr string `xml:"Version,attr"`
			VersionElem string `xml:"Version"`
		} `xml:"PackageReference"`
		ProjectReferences []struct {
			Include string `xml:"Include,attr"`
		} `xml:"ProjectReference"`
	} `xml:"ItemGroup"`
}

// projectFileNode anchors a csproj/sln in the graph (nothing else emits it).
func projectFileNode(c *collector, rel string) {
	c.node(schema.Node{
		ID: rel, Label: filepath.Base(rel), Kind: "config", FileType: "manifest",
		Source: rel, Location: "L1",
	})
}

// resolveProjectPath normalizes a ProjectReference/sln path (Windows
// separators, ..) into a repo-relative slash path from the referencing file.
func resolveProjectPath(rel, ref string) string {
	ref = strings.ReplaceAll(ref, `\`, "/")
	dir := filepath.ToSlash(filepath.Dir(rel))
	return filepath.ToSlash(filepath.Join(dir, ref))
}

func extractCsproj(c *collector, rel string, data []byte) {
	var doc csprojDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return
	}
	projectFileNode(c, rel)
	for _, ig := range doc.ItemGroups {
		for _, pr := range ig.PackageReferences {
			if pr.Include == "" {
				continue
			}
			version := pr.VersionAttr
			if version == "" {
				version = strings.TrimSpace(pr.VersionElem)
			}
			id := c.depNode("nuget", pr.Include)
			c.declares(rel, id, version, "package")
		}
		for _, pj := range ig.ProjectReferences {
			if pj.Include == "" {
				continue
			}
			c.edge(schema.Edge{
				Source: rel, Target: resolveProjectPath(rel, pj.Include),
				Relation: "depends_on", Confidence: schema.Extracted,
				Metadata: map[string]string{"via": "project-reference"},
			})
		}
	}
}

// slnProjectRe matches the sln project lines:
//
//	Project("{GUID}") = "Name", "rel\path\Name.csproj", "{GUID}"
var slnProjectRe = regexp.MustCompile(`^Project\("\{[^}]+\}"\)\s*=\s*"[^"]*",\s*"([^"]+)"`)

func extractSln(c *collector, rel, content string) {
	projectFileNode(c, rel)
	for _, line := range strings.Split(content, "\n") {
		m := slnProjectRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		ref := m[1]
		// Solution folders and non-project entries carry no project file ext.
		if !strings.Contains(strings.ToLower(filepath.Ext(ref)), "proj") {
			continue
		}
		c.edge(schema.Edge{
			Source: rel, Target: resolveProjectPath(rel, ref),
			Relation: "depends_on", Confidence: schema.Extracted,
			Metadata: map[string]string{"via": "sln"},
		})
	}
}
