// yamlroutes.go — config-file route recognition (Phase 2 of the
// framework-routes spec): OpenAPI/Swagger specs, Drupal *.routing.yml,
// Kubernetes Ingress. Rides the SAME config lane as extractConfig — the
// secret-name refusals and the maxConfigBytes cap gate this walk too.
//
// No YAML library (stdlib-only rule): the shared indentation-based line
// walker (internal/extract/yamlwalk, also used by the manifests producer's
// k8s lane) — deterministic and good-enough for exactly these three shapes.
// Anything the walker can't confidently read is skipped silently — never
// guessed.
//
// What each channel matches:
//
//	openapi-route  a .yaml/.yml with a top-level openapi:/swagger: key —
//	               under paths:, keys starting with "/" are paths, their
//	               get/post/put/delete/patch/head/options children become one
//	               route node each (label `GET /users/{id}`). contains edge
//	               from the config doc (EXTRACTED — it IS in the file); an
//	               operationId: literal adds a handles edge to that bare name
//	               (INFERRED, resolved cross-batch, dangling is fine).
//	drupal-route   *.routing.yml — top-level entries with a path: child
//	               (`ROUTE /path`; a methods: list makes one node per
//	               method); _controller: 'Class::method' → handles edge to
//	               the LAST segment (method), INFERRED.
//	ingress-route  kind: Ingress documents — spec.rules[].http.paths[].path
//	               literals (`ROUTE /path`); backend service name → handles
//	               edge target by name (INFERRED, dangling OK). BEST-EFFORT:
//	               fields the walker can't pin down are skipped silently.
package markdown

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/yamlwalk"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// yamlRoute is one recognized route pending emission.
type yamlRoute struct {
	method, path string
	line         int
	handler      string // optional handles target (bare name, dangling OK)
	channel      string
}

// extractYAMLRoutes recognizes route shapes in a .yaml/.yml config file and
// appends route nodes + edges to the batch. Multi-document files (---) are
// processed per document.
func extractYAMLRoutes(b *schema.Batch, rel, content string) {
	var routes []yamlRoute
	isDrupal := strings.HasSuffix(filepath.Base(rel), ".routing.yml") ||
		strings.HasSuffix(filepath.Base(rel), ".routing.yaml")
	all := strings.Split(content, "\n")
	start := 0
	flush := func(end int) {
		if end <= start {
			return
		}
		ls := yamlwalk.Parse(all[start:end], start)
		routes = append(routes, openAPIRoutes(ls)...)
		if isDrupal {
			routes = append(routes, drupalRoutes(ls)...)
		}
		routes = append(routes, ingressRoutes(ls)...)
	}
	for i, line := range all {
		if strings.TrimSpace(line) == "---" {
			flush(i)
			start = i + 1
		}
	}
	flush(len(all))
	emitYAMLRoutes(b, rel, routes)
}

func emitYAMLRoutes(b *schema.Batch, rel string, routes []yamlRoute) {
	seenNode := map[string]bool{}
	seenEdge := map[string]bool{}
	for _, r := range routes {
		id := rel + "::route:" + r.method + " " + r.path
		if !seenNode[id] {
			seenNode[id] = true
			b.Nodes = append(b.Nodes, schema.Node{
				ID: id, Label: r.method + " " + r.path, Kind: "route",
				FileType: "config", Source: rel, Location: fmt.Sprintf("L%d", r.line),
				Metadata: map[string]string{"synthesized_by": r.channel},
			})
			b.Edges = append(b.Edges, schema.Edge{
				Source: rel, Target: id, Relation: "contains", Confidence: schema.Extracted,
			})
		}
		if r.handler == "" {
			continue
		}
		ek := id + "\x00" + r.handler
		if seenEdge[ek] {
			continue
		}
		seenEdge[ek] = true
		b.Edges = append(b.Edges, schema.Edge{
			Source: id, Target: r.handler, Relation: "handles", Confidence: schema.Inferred,
			Metadata: map[string]string{"synthesized_by": r.channel},
		})
	}
}

var openAPIMethods = map[string]string{
	"get": "GET", "post": "POST", "put": "PUT", "delete": "DELETE",
	"patch": "PATCH", "head": "HEAD", "options": "OPTIONS",
}

// openAPIRoutes: top-level openapi:/swagger: marks the document; paths:
// children starting with "/" are paths; their method children are routes.
func openAPIRoutes(ls []yamlwalk.Line) []yamlRoute {
	isAPI := false
	for _, l := range ls {
		if l.Indent == 0 && !l.List && (l.Key == "openapi" || l.Key == "swagger") {
			isAPI = true
			break
		}
	}
	if !isAPI {
		return nil
	}
	var out []yamlRoute
	for i := 0; i < len(ls); i++ {
		if ls[i].Indent != 0 || ls[i].List || ls[i].Key != "paths" {
			continue
		}
		end := yamlwalk.Span(ls, i)
		if i+1 >= end {
			continue
		}
		pathIndent := ls[i+1].Indent
		for j := i + 1; j < end; j++ {
			if ls[j].Indent != pathIndent || !strings.HasPrefix(ls[j].Key, "/") {
				continue
			}
			pend := yamlwalk.Span(ls, j)
			if j+1 >= pend {
				continue
			}
			mIndent := ls[j+1].Indent
			for k := j + 1; k < pend; k++ {
				if ls[k].Indent != mIndent {
					continue
				}
				verb, ok := openAPIMethods[strings.ToLower(ls[k].Key)]
				if !ok {
					continue
				}
				r := yamlRoute{method: verb, path: ls[j].Key, line: ls[k].Num, channel: "openapi-route"}
				mend := yamlwalk.Span(ls, k)
				for m := k + 1; m < mend; m++ {
					if ls[m].Key == "operationId" && ls[m].Val != "" {
						r.handler = ls[m].Val
						break
					}
				}
				out = append(out, r)
			}
		}
	}
	return out
}

// drupalRoutes: top-level entries with a path: child. methods: (inline flow
// list or block list) makes one node per method; absent methods = the
// method-less ROUTE token. _controller: 'Class::method' → handles to the
// last :: segment.
func drupalRoutes(ls []yamlwalk.Line) []yamlRoute {
	var out []yamlRoute
	for i := 0; i < len(ls); i++ {
		if ls[i].Indent != 0 || ls[i].List || ls[i].Key == "" {
			continue
		}
		end := yamlwalk.Span(ls, i)
		path, pathLine := "", 0
		var methods []string
		handler := ""
		for j := i + 1; j < end; j++ {
			switch ls[j].Key {
			case "path":
				if path == "" && strings.HasPrefix(ls[j].Val, "/") {
					path, pathLine = ls[j].Val, ls[j].Num
				}
			case "methods":
				if v := ls[j].Val; strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
					for _, m := range strings.Split(strings.Trim(v, "[]"), ",") {
						if m = strings.ToUpper(strings.Trim(strings.TrimSpace(m), `"'`)); m != "" {
							methods = append(methods, m)
						}
					}
				} else {
					for k := j + 1; k < end && ls[k].Indent > ls[j].Indent; k++ {
						if ls[k].List && ls[k].Key == "" && ls[k].Val != "" {
							methods = append(methods, strings.ToUpper(ls[k].Val))
						}
					}
				}
			case "_controller":
				if idx := strings.LastIndex(ls[j].Val, "::"); idx >= 0 {
					handler = ls[j].Val[idx+2:]
				}
			}
		}
		if path == "" {
			continue
		}
		if len(methods) == 0 {
			methods = []string{"ROUTE"}
		}
		for _, m := range methods {
			out = append(out, yamlRoute{method: m, path: path, line: pathLine, handler: handler, channel: "drupal-route"})
		}
	}
	return out
}

// ingressRoutes: kind: Ingress documents — path: literals under a top-level
// spec: block become routes; the item's backend service name is the handles
// target. Best-effort by design.
func ingressRoutes(ls []yamlwalk.Line) []yamlRoute {
	isIngress := false
	for _, l := range ls {
		if l.Indent == 0 && !l.List && l.Key == "kind" && l.Val == "Ingress" {
			isIngress = true
			break
		}
	}
	if !isIngress {
		return nil
	}
	var out []yamlRoute
	for i := 0; i < len(ls); i++ {
		if ls[i].Indent != 0 || ls[i].Key != "spec" {
			continue
		}
		end := yamlwalk.Span(ls, i)
		for j := i + 1; j < end; j++ {
			if ls[j].Key != "path" || !strings.HasPrefix(ls[j].Val, "/") {
				continue
			}
			r := yamlRoute{method: "ROUTE", path: ls[j].Val, line: ls[j].Num, channel: "ingress-route"}
			iend := yamlwalk.ItemSpan(ls, j)
			if iend > end {
				iend = end
			}
			for k := j + 1; k < iend; k++ {
				if ls[k].Key != "service" {
					continue
				}
				send := yamlwalk.Span(ls, k)
				for m := k + 1; m < send; m++ {
					if ls[m].Key == "name" && ls[m].Val != "" {
						r.handler = ls[m].Val
						break
					}
				}
				break
			}
			out = append(out, r)
		}
	}
	return out
}
