// dockerfile.go — Dockerfiles as build stages, per ADR
// 2026-07-25-docker-compose-recognizer. Line-anchored and literal:
//
//	FROM <ref> [AS <name>]   node kind `stage`, id <file>::stage:<name-or-index>
//	the <ref>                stage --uses_image--> image:<ref>  (the SAME shape
//	                         k8s.go and the compose lane emit — one node per ref)
//	COPY --from=<x>          stage --depends_on--> stage, ONLY when <x> names a
//	                         stage declared in this file; otherwise <x> is an
//	                         image reference and nothing is emitted
//	EXPOSE                   stage metadata
//
// NEVER read: RUN / CMD / ENTRYPOINT / ENV / ARG text — command text carries
// secrets and is not structure. `${VAR}` stays the literal text it is.
package manifests

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// dockerStage is one literal FROM block.
type dockerStage struct {
	name      string // AS <name>, empty when unnamed
	ref       string // the FROM image reference, verbatim
	index     int    // ordinal of the FROM in the file
	firstLine int
	expose    []string
	fromRefs  []string // COPY --from=<x> targets seen inside this stage
}

func (s dockerStage) id(rel string) string {
	if s.name != "" {
		return rel + "::stage:" + s.name
	}
	return rel + "::stage:" + strconv.Itoa(s.index)
}

// extractDockerfile scans one Dockerfile and emits its stages.
func extractDockerfile(c *collector, rel, content string) {
	lines := strings.Split(content, "\n")
	var stages []*dockerStage
	cur := (*dockerStage)(nil)
	for i, raw := range lines {
		t := strings.TrimSpace(raw)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		fields := strings.Fields(t)
		switch strings.ToUpper(fields[0]) {
		case "FROM":
			name, ref := parseFrom(fields[1:])
			if ref == "" {
				cur = nil // unreadable FROM — claim nothing for this block
				continue
			}
			cur = &dockerStage{name: name, ref: ref, index: len(stages), firstLine: i + 1}
			stages = append(stages, cur)
		case "EXPOSE":
			if cur != nil {
				cur.expose = append(cur.expose, fields[1:]...)
			}
		case "COPY":
			if cur == nil {
				continue
			}
			for _, f := range fields[1:] {
				if v, ok := strings.CutPrefix(f, "--from="); ok && v != "" {
					cur.fromRefs = append(cur.fromRefs, strings.Trim(v, `"'`))
				}
			}
		}
	}

	byName := map[string]*dockerStage{}
	for _, s := range stages {
		if s.name != "" {
			byName[strings.ToLower(s.name)] = s
		}
	}
	for _, s := range stages {
		id := s.id(rel)
		md := map[string]string{}
		if len(s.expose) > 0 {
			md["expose"] = strings.Join(s.expose, ",")
		}
		label := s.name
		if label == "" {
			label = s.ref
		}
		// Location is the FROM line only, never the stage span: `card` /
		// `query --include-content` hydrate a node's line range verbatim from
		// disk, and a stage span covers RUN/CMD/ENTRYPOINT text — which this
		// recognizer refuses to read precisely because it can embed secrets.
		// A single-line anchor keeps that text unreachable through the graph
		// (the k8s lane anchors its resources the same way).
		c.node(schema.Node{
			ID: id, Label: label, Kind: "stage", FileType: "manifest",
			Source: rel, Location: fmt.Sprintf("L%d", s.firstLine), Metadata: md,
		})
		c.edge(schema.Edge{Source: rel, Target: id, Relation: "contains", Confidence: schema.Extracted})
		imageEdge(c, id, s.ref)
		for _, ref := range s.fromRefs {
			// A --from that does NOT name a stage in this file is an image
			// reference; resolving it would be a guess, so nothing is emitted.
			target, ok := byName[strings.ToLower(ref)]
			if !ok || target == s {
				continue
			}
			c.edge(schema.Edge{
				Source: id, Target: target.id(rel), Relation: "depends_on",
				Confidence: schema.Extracted,
			})
		}
	}
}

// parseFrom reads `[--flag=…]… <ref> [AS <name>]`. `AS` casing is free-form.
func parseFrom(args []string) (name, ref string) {
	var rest []string
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			continue // --platform=… and friends: not part of the reference
		}
		rest = append(rest, a)
	}
	if len(rest) == 0 {
		return "", ""
	}
	ref = rest[0]
	if len(rest) == 3 && strings.EqualFold(rest[1], "as") {
		name = rest[2]
	}
	return name, ref
}
