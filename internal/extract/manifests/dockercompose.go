// dockercompose.go — Docker Compose files as service topology, via the shared
// yaml indent walker (internal/extract/yamlwalk — same engine as the k8s
// lane). Per ADR 2026-07-25-docker-compose-recognizer.
//
// Recognition is by FILENAME ONLY (compose.y{a,}ml / docker-compose.y{a,}ml):
// an arbitrary yaml that happens to carry a `services:` key is NOT compose, and
// the k8s lane keeps every real k8s manifest (both lanes see `.yaml`).
//
// Emitted (all EXTRACTED — every one is a literal read of the file):
//
//	<file> --contains--> <file>::service:<name>     each key under services:
//	service --uses_image--> image:<ref>             `image:` scalar, shape
//	                                                IDENTICAL to k8s.go so a
//	                                                repo with both lanes
//	                                                converges on ONE image node
//	service --depends_on--> service                 depends_on list AND map form
//	service --depends_on--> <dockerfile path>       build: context/dockerfile,
//	                                                ONLY if it exists on disk
//
// Ports ride as service metadata, never as nodes.
//
// NEVER read (the secret surface): `environment:` and `env_file:` — neither
// values NOR keys. `${VAR}` is emitted as the literal text it is; compose
// `extends`/`include`/profiles/override-file merging is resolution, i.e.
// inference, so each file is read literally and independently.
package manifests

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/yamlwalk"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// composeNames is the exact basename set that makes a yaml file compose.
var composeNames = map[string]bool{
	"compose.yaml": true, "compose.yml": true,
	"docker-compose.yaml": true, "docker-compose.yml": true,
}

// composeService is one literal service declaration.
type composeService struct {
	name      string
	firstLine int
	image     string
	ports     []string
	dependsOn []string // service names, as written
	buildCtx  string   // build: string form, or build.context
	buildFile string   // build.dockerfile (empty = default "Dockerfile")
	hasBuild  bool
}

// extractCompose reads one compose file. root is needed to answer the only
// on-disk question we ask: does the build's Dockerfile actually exist?
func extractCompose(c *collector, root, rel, content string) {
	ls := yamlwalk.Parse(strings.Split(content, "\n"), 0)
	for i := 0; i < len(ls); i++ {
		if ls[i].Indent != 0 || ls[i].List || ls[i].Key != "services" || ls[i].Val != "" {
			continue
		}
		end := yamlwalk.Span(ls, i)
		if i+1 >= end {
			continue
		}
		svcIndent := ls[i+1].Indent
		for j := i + 1; j < end; j++ {
			if ls[j].Indent != svcIndent || ls[j].List || ls[j].Key == "" || ls[j].Val != "" {
				continue // only block-mapping service keys; flow style is not read
			}
			emitComposeService(c, root, rel, parseComposeService(ls, j, yamlwalk.Span(ls, j)))
		}
		return // one top-level services: block per file
	}
}

// parseComposeService reads the direct children of one service block. Keys
// outside the recognized set — environment, env_file, volumes, networks,
// command, … — are not read at all.
func parseComposeService(ls []yamlwalk.Line, i, end int) composeService {
	s := composeService{name: ls[i].Key, firstLine: ls[i].Num}
	if i+1 >= end {
		return s
	}
	keyIndent := ls[i+1].Indent
	for j := i + 1; j < end; j++ {
		if ls[j].Indent != keyIndent || ls[j].List {
			continue
		}
		switch ls[j].Key {
		case "image":
			if s.image == "" {
				s.image = ls[j].Val
			}
		case "ports":
			s.ports = append(s.ports, blockScalars(ls, j, ls[j].Val)...)
		case "depends_on":
			s.dependsOn = append(s.dependsOn, dependsOnNames(ls, j)...)
		case "build":
			s.hasBuild = true
			if ls[j].Val != "" {
				if m, ok := flowMap(ls[j].Val); ok {
					s.buildCtx, s.buildFile = m["context"], m["dockerfile"]
				} else {
					s.buildCtx = ls[j].Val // build: ./api
				}
				continue
			}
			bend := yamlwalk.Span(ls, j)
			for k := j + 1; k < bend; k++ {
				switch ls[k].Key {
				case "context":
					s.buildCtx = ls[k].Val
				case "dockerfile":
					s.buildFile = ls[k].Val
				}
			}
		}
	}
	return s
}

// dependsOnNames reads BOTH literal forms:
//
//	depends_on: [db, cache]     inline sequence
//	depends_on:                 block sequence
//	  - db
//	depends_on:                 block mapping (long form)
//	  db:
//	    condition: service_healthy
func dependsOnNames(ls []yamlwalk.Line, i int) []string {
	if ls[i].Val != "" {
		return flowList(ls[i].Val)
	}
	end := yamlwalk.Span(ls, i)
	if i+1 >= end {
		return nil
	}
	ind := ls[i+1].Indent
	var out []string
	for j := i + 1; j < end; j++ {
		if ls[j].Indent != ind {
			continue
		}
		switch {
		case ls[j].List && ls[j].Key == "" && ls[j].Val != "":
			out = append(out, ls[j].Val) // - db
		case !ls[j].List && ls[j].Key != "" && ls[j].Val == "":
			out = append(out, ls[j].Key) // db: {condition: …}
		}
	}
	return out
}

// blockScalars collects a key's list values: an inline `[a, b]` when val is
// set, otherwise the block-sequence scalars directly under it.
func blockScalars(ls []yamlwalk.Line, i int, val string) []string {
	if val != "" {
		if l := flowList(val); l != nil {
			return l
		}
		return []string{val}
	}
	end := yamlwalk.Span(ls, i)
	if i+1 >= end {
		return nil
	}
	ind := ls[i+1].Indent
	var out []string
	for j := i + 1; j < end; j++ {
		if ls[j].Indent == ind && ls[j].List && ls[j].Key == "" && ls[j].Val != "" {
			out = append(out, ls[j].Val)
		}
	}
	return out
}

// flowList reads `[a, "b", c]` literally. Anything that is not a plain
// bracketed list of scalars yields nil (literal-or-silent).
func flowList(v string) []string {
	if !strings.HasPrefix(v, "[") || !strings.HasSuffix(v, "]") {
		return nil
	}
	inner := strings.TrimSpace(v[1 : len(v)-1])
	if inner == "" || strings.ContainsAny(inner, "[]{}") {
		return nil
	}
	var out []string
	for _, part := range strings.Split(inner, ",") {
		item := strings.Trim(strings.TrimSpace(part), `"'`)
		if item == "" {
			return nil
		}
		out = append(out, item)
	}
	return out
}

// flowMap reads `{context: ., dockerfile: Dockerfile.dev}` literally. Nested
// or malformed flow yields ok=false and nothing is claimed.
func flowMap(v string) (map[string]string, bool) {
	if !strings.HasPrefix(v, "{") || !strings.HasSuffix(v, "}") {
		return nil, false
	}
	inner := strings.TrimSpace(v[1 : len(v)-1])
	if inner == "" || strings.ContainsAny(inner, "{}[]") {
		return nil, false
	}
	m := map[string]string{}
	for _, part := range strings.Split(inner, ",") {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			return nil, false
		}
		key := strings.Trim(strings.TrimSpace(kv[0]), `"'`)
		val := strings.Trim(strings.TrimSpace(kv[1]), `"'`)
		if key == "" {
			return nil, false
		}
		m[key] = val
	}
	return m, true
}

// emitComposeService turns one parsed service into graph.
func emitComposeService(c *collector, root, rel string, s composeService) {
	if s.name == "" {
		return
	}
	id := composeServiceID(rel, s.name)
	md := map[string]string{}
	if len(s.ports) > 0 {
		md["ports"] = strings.Join(s.ports, ",")
	}
	// Location is the service KEY line only, never the block span — same as the
	// k8s lane's resource nodes, and deliberately so: `card`/`query
	// --include-content` hydrate a node's line range verbatim from disk, so a
	// span over a service block would read `environment:` back out of the file
	// and hand it to the caller. The env surface stays unreachable through the
	// graph by construction. The span is available in metadata as line counts
	// only if ever needed; today nothing needs it.
	c.node(schema.Node{
		ID: id, Label: s.name, Kind: "service", FileType: "manifest",
		Source: rel, Location: fmt.Sprintf("L%d", s.firstLine), Metadata: md,
	})
	c.edge(schema.Edge{Source: rel, Target: id, Relation: "contains", Confidence: schema.Extracted})

	if s.image != "" {
		imageEdge(c, id, s.image)
	}
	for _, dep := range s.dependsOn {
		c.edge(schema.Edge{
			Source: id, Target: composeServiceID(rel, dep),
			Relation: "depends_on", Confidence: schema.Extracted,
		})
	}
	if s.hasBuild {
		if df := resolveDockerfile(root, rel, s.buildCtx, s.buildFile); df != "" {
			c.edge(schema.Edge{
				Source: id, Target: df, Relation: "depends_on",
				Confidence: schema.Extracted,
			})
		}
	}
}

func composeServiceID(rel, name string) string { return rel + "::service:" + name }

// imageEdge emits the shared image node + uses_image edge in EXACTLY the shape
// k8s.go:344-351 established, so both lanes converge on one node per ref.
func imageEdge(c *collector, from, img string) {
	imgID := "image:" + img
	c.node(schema.Node{
		ID: imgID, Label: img, Kind: "image", FileType: "manifest",
		Source: imgID,
	})
	c.edge(schema.Edge{
		Source: from, Target: imgID, Relation: "uses_image",
		Confidence: schema.Extracted,
	})
}

// resolveDockerfile joins the build context and dockerfile name against the
// compose file's directory and returns the repo-relative path ONLY when that
// file exists. Anything unresolvable — a variable, a git/url context, an
// absolute or escaping path — returns "" and nothing is emitted.
func resolveDockerfile(root, rel, ctx, file string) string {
	if ctx == "" {
		ctx = "."
	}
	if file == "" {
		file = "Dockerfile"
	}
	if strings.Contains(ctx, "$") || strings.Contains(file, "$") {
		return "" // ${VAR} is never resolved
	}
	if path.IsAbs(ctx) || path.IsAbs(file) || strings.Contains(ctx, "://") ||
		strings.HasPrefix(ctx, "git@") {
		return ""
	}
	p := path.Join(path.Dir(rel), ctx, file)
	if p == "" || p == "." || strings.HasPrefix(p, "..") {
		return ""
	}
	if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil || info.IsDir() {
		return ""
	}
	return p
}
