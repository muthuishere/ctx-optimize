// maven.go — pom.xml recognizer (stdlib encoding/xml): dependencies →
// dep:maven/<groupId>:<artifactId> (version verbatim, ${properties} included
// — no property resolution, spec text only), <modules> → depends_on edges to
// member poms, <parent> as a dependency with scope "parent", build plugins as
// dependencies with scope "plugin".
package manifests

import (
	"encoding/xml"
	"path/filepath"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

type pomCoord struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
}

type pomXMLDoc struct {
	Parent       pomCoord   `xml:"parent"`
	Modules      []string   `xml:"modules>module"`
	Dependencies []pomCoord `xml:"dependencies>dependency"`
	Plugins      []pomCoord `xml:"build>plugins>plugin"`
}

func extractPomXML(c *collector, rel string, data []byte) {
	var pom pomXMLDoc
	if err := xml.Unmarshal(data, &pom); err != nil {
		return // malformed xml: skip silently, the markdown lane still indexed it
	}
	emit := func(d pomCoord, defaultScope string) {
		if d.GroupID == "" || d.ArtifactID == "" {
			return
		}
		scope := d.Scope
		if scope == "" {
			scope = defaultScope
		}
		id := c.depNode("maven", d.GroupID+":"+d.ArtifactID)
		c.declares(rel, id, d.Version, scope)
	}
	for _, d := range pom.Dependencies {
		emit(d, "compile")
	}
	for _, p := range pom.Plugins {
		p.Scope = "" // a plugin's <scope> is not a thing; force ours
		emit(p, "plugin")
	}
	if pom.Parent.GroupID != "" && pom.Parent.ArtifactID != "" {
		emit(pomCoord{GroupID: pom.Parent.GroupID, ArtifactID: pom.Parent.ArtifactID,
			Version: pom.Parent.Version}, "parent")
	}
	// Aggregator modules: pom --depends_on--> <module-dir>/pom.xml (the maven
	// module graph). Repo-relative resolution from this pom's directory.
	dir := filepath.ToSlash(filepath.Dir(rel))
	for _, m := range pom.Modules {
		if m == "" {
			continue
		}
		target := filepath.ToSlash(filepath.Join(dir, filepath.FromSlash(m), "pom.xml"))
		c.edge(schema.Edge{
			Source: rel, Target: target, Relation: "depends_on",
			Confidence: schema.Extracted, Metadata: map[string]string{"via": "maven-module"},
		})
	}
}
