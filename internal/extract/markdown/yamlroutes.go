// yamlroutes.go — config-file route recognition (Phase 2 of the
// framework-routes spec): OpenAPI/Swagger specs, Drupal *.routing.yml,
// Kubernetes Ingress. Rides the SAME config lane as extractConfig — the
// secret-name refusals and the maxConfigBytes cap gate this walk too.
//
// No YAML library (stdlib-only rule): a small indentation-based line walker,
// deterministic and good-enough for exactly these three shapes. Anything the
// walker can't confidently read is skipped silently — never guessed.
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

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// yline is one meaningful YAML line: indentation (a `- ` list marker counts
// as two columns of indent, so a list item's keys align with its siblings),
// an optional key, an unquoted scalar value, and the 1-based line number.
type yline struct {
	indent int
	key    string
	val    string
	list   bool
	num    int
}

// parseYAMLLines flattens one YAML document into ylines. Blank lines,
// comments, and tab-indented lines (YAML forbids tabs; we refuse rather than
// misread) are dropped.
func parseYAMLLines(lines []string, offset int) []yline {
	var out []yline
	for idx, line := range lines {
		num := offset + idx + 1
		t := strings.TrimRight(line, " \t\r")
		if t == "" {
			continue
		}
		ind := 0
		for ind < len(t) && t[ind] == ' ' {
			ind++
		}
		if ind < len(t) && t[ind] == '\t' {
			continue
		}
		content := t[ind:]
		if strings.HasPrefix(content, "#") || content == "-" {
			continue
		}
		isList := false
		if strings.HasPrefix(content, "- ") {
			isList = true
			ind += 2
			content = strings.TrimLeft(content[2:], " ")
		}
		key, val := splitKeyVal(content)
		out = append(out, yline{indent: ind, key: key, val: val, list: isList, num: num})
	}
	return out
}

// splitKeyVal splits `key: value` at the first colon followed by a space (or
// end of line) — URLs (`http://…`) stay whole. A colon-less line is a scalar.
// Surrounding quotes and trailing comments are stripped from the value.
func splitKeyVal(s string) (key, val string) {
	ci := -1
	for j := 0; j < len(s); j++ {
		if s[j] == ':' && (j == len(s)-1 || s[j+1] == ' ') {
			ci = j
			break
		}
	}
	if ci < 0 {
		return "", cleanScalar(s)
	}
	key = strings.Trim(strings.TrimSpace(s[:ci]), `"'`)
	return key, cleanScalar(strings.TrimSpace(s[ci+1:]))
}

// cleanScalar unquotes a scalar or cuts an unquoted trailing comment.
func cleanScalar(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		if end := strings.IndexByte(v[1:], v[0]); end >= 0 {
			return v[1 : 1+end]
		}
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// yspan returns the index just past the block owned by ls[i]: every following
// line indented deeper than ls[i].
func yspan(ls []yline, i int) int {
	j := i + 1
	for j < len(ls) && ls[j].indent > ls[i].indent {
		j++
	}
	return j
}

// yitemSpan is yspan for a LIST ITEM: sibling keys of the item share its
// indent (the dash counted), so the item ends at a dedent or the next dash.
func yitemSpan(ls []yline, i int) int {
	j := i + 1
	for j < len(ls) && (ls[j].indent > ls[i].indent || (ls[j].indent == ls[i].indent && !ls[j].list)) {
		j++
	}
	return j
}

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
		ls := parseYAMLLines(all[start:end], start)
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
func openAPIRoutes(ls []yline) []yamlRoute {
	isAPI := false
	for _, l := range ls {
		if l.indent == 0 && !l.list && (l.key == "openapi" || l.key == "swagger") {
			isAPI = true
			break
		}
	}
	if !isAPI {
		return nil
	}
	var out []yamlRoute
	for i := 0; i < len(ls); i++ {
		if ls[i].indent != 0 || ls[i].list || ls[i].key != "paths" {
			continue
		}
		end := yspan(ls, i)
		if i+1 >= end {
			continue
		}
		pathIndent := ls[i+1].indent
		for j := i + 1; j < end; j++ {
			if ls[j].indent != pathIndent || !strings.HasPrefix(ls[j].key, "/") {
				continue
			}
			pend := yspan(ls, j)
			if j+1 >= pend {
				continue
			}
			mIndent := ls[j+1].indent
			for k := j + 1; k < pend; k++ {
				if ls[k].indent != mIndent {
					continue
				}
				verb, ok := openAPIMethods[strings.ToLower(ls[k].key)]
				if !ok {
					continue
				}
				r := yamlRoute{method: verb, path: ls[j].key, line: ls[k].num, channel: "openapi-route"}
				mend := yspan(ls, k)
				for m := k + 1; m < mend; m++ {
					if ls[m].key == "operationId" && ls[m].val != "" {
						r.handler = ls[m].val
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
func drupalRoutes(ls []yline) []yamlRoute {
	var out []yamlRoute
	for i := 0; i < len(ls); i++ {
		if ls[i].indent != 0 || ls[i].list || ls[i].key == "" {
			continue
		}
		end := yspan(ls, i)
		path, pathLine := "", 0
		var methods []string
		handler := ""
		for j := i + 1; j < end; j++ {
			switch ls[j].key {
			case "path":
				if path == "" && strings.HasPrefix(ls[j].val, "/") {
					path, pathLine = ls[j].val, ls[j].num
				}
			case "methods":
				if v := ls[j].val; strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
					for _, m := range strings.Split(strings.Trim(v, "[]"), ",") {
						if m = strings.ToUpper(strings.Trim(strings.TrimSpace(m), `"'`)); m != "" {
							methods = append(methods, m)
						}
					}
				} else {
					for k := j + 1; k < end && ls[k].indent > ls[j].indent; k++ {
						if ls[k].list && ls[k].key == "" && ls[k].val != "" {
							methods = append(methods, strings.ToUpper(ls[k].val))
						}
					}
				}
			case "_controller":
				if idx := strings.LastIndex(ls[j].val, "::"); idx >= 0 {
					handler = ls[j].val[idx+2:]
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
func ingressRoutes(ls []yline) []yamlRoute {
	isIngress := false
	for _, l := range ls {
		if l.indent == 0 && !l.list && l.key == "kind" && l.val == "Ingress" {
			isIngress = true
			break
		}
	}
	if !isIngress {
		return nil
	}
	var out []yamlRoute
	for i := 0; i < len(ls); i++ {
		if ls[i].indent != 0 || ls[i].key != "spec" {
			continue
		}
		end := yspan(ls, i)
		for j := i + 1; j < end; j++ {
			if ls[j].key != "path" || !strings.HasPrefix(ls[j].val, "/") {
				continue
			}
			r := yamlRoute{method: "ROUTE", path: ls[j].val, line: ls[j].num, channel: "ingress-route"}
			iend := yitemSpan(ls, j)
			if iend > end {
				iend = end
			}
			for k := j + 1; k < iend; k++ {
				if ls[k].key != "service" {
					continue
				}
				send := yspan(ls, k)
				for m := k + 1; m < send; m++ {
					if ls[m].key == "name" && ls[m].val != "" {
						r.handler = ls[m].val
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
