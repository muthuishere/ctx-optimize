// The openapi connector — tier-1 native source for API specifications (ADR
// 2026-07-17-bundled-adapter-templates). Routes claim it for http:// and
// https:// values AND for no-scheme filesystem paths (Route hands both to
// "openapi"). One dial (or one read), one Batch, deterministic sorted output.
//
// JSON only in this build: go.mod carries no YAML library, and adding one is
// a reviewed dependency decision — YAML specs get a loud error naming the
// adapter-script lane. Stored ids come from Sanitize, so https ids strip
// userinfo and ALL query params (M2). Security-scheme nodes carry an
// allowlisted metadata set only, so example/secret values are stripped by
// construction (M6).
package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/sources"
)

const (
	openapiProducer = "openapi"
	openapiTimeout  = 10 * time.Second
	openapiMaxBody  = 20 << 20 // a 20MB "spec" is data, not an API description
)

// openapiMethods is the fixed operation order (determinism, not alphabet).
var openapiMethods = []string{"get", "put", "post", "delete", "patch", "head", "options", "trace"}

// securitySchemeAllowed is the metadata ALLOWLIST for security-scheme nodes:
// structural fields only. example/x-example/value and any vendor extension
// never reach the store — allowlist-in, not denylist-out (M6).
var securitySchemeAllowed = []string{"type", "scheme", "in", "name", "bearerFormat", "flow", "openIdConnectUrl", "authorizationUrl", "tokenUrl"}

type openapiConnector struct{}

func init() { sources.Register(openapiConnector{}) }

func (openapiConnector) Scheme() string { return "openapi" }

func (openapiConnector) Example() string {
	return "https://$API_USER:$API_TOKEN@api.internal/openapi.json"
}

func (openapiConnector) Params() []sources.Param {
	return []sources.Param{
		{Name: "user:pass userinfo", Desc: "sent as a Basic auth header on the fetch; stripped from stored ids", Cred: true},
		{Name: "(file path)", Desc: "a no-scheme value is a spec file read from disk, relative to the repo root"},
		{Name: "(format)", Desc: "OpenAPI 3.x or Swagger 2.0, JSON only — YAML specs need the adapter-script lane (.ctxoptimize/adapters/)"},
	}
}

// Capture fetches (http/https) or reads (no-scheme path) one spec and emits
// api → path → operation → component-schema nodes with contains + uses edges.
func (openapiConnector) Capture(ctx context.Context, value string) (*schema.Batch, error) {
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		id, ok := sources.Sanitize(value)
		if !ok {
			return nil, fmt.Errorf("openapi: source URL defies parsing — check the entry shape")
		}
		data, err := fetchOpenAPI(ctx, value, id)
		if err != nil {
			return nil, err
		}
		return buildOpenAPIBatch(id, data)
	}
	// The file lane: Route sends no-scheme values here. The value is a path
	// relative to the repo root (the CLI dials from the repo root, so a
	// relative path resolves against the process cwd).
	p := filepath.FromSlash(value)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("openapi %s: %w", filepath.ToSlash(value), err)
	}
	if len(data) > openapiMaxBody {
		return nil, fmt.Errorf("openapi %s: spec exceeds %dMB", filepath.ToSlash(value), openapiMaxBody>>20)
	}
	return buildOpenAPIBatch(filepath.ToSlash(value), data)
}

// fetchOpenAPI dials the raw (secret-bearing) URL and returns the body.
// Userinfo is split off TEXTUALLY (never url.Parse on a secret-bearing
// string) and rides a Basic auth header; every error names the sanitized id
// only — the raw URL never reaches an error string.
func fetchOpenAPI(ctx context.Context, raw, id string) ([]byte, error) {
	idx := strings.Index(raw, "://")
	scheme, rest := raw[:idx], raw[idx+3:]
	authority, tail := rest, ""
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		authority, tail = rest[:end], rest[end:]
	}
	user, pass, hasAuth := "", "", false
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		userinfo := authority[:at]
		authority = authority[at+1:]
		user, pass, _ = strings.Cut(userinfo, ":")
		hasAuth = true
		if u, err := url.PathUnescape(user); err == nil {
			user = u
		}
		if p, err := url.PathUnescape(pass); err == nil {
			pass = p
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+authority+tail, nil)
	if err != nil {
		return nil, fmt.Errorf("openapi %s: invalid URL", id) // never echo err — url errors embed the URL
	}
	if hasAuth {
		req.SetBasicAuth(user, pass)
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: openapiTimeout}
	resp, err := client.Do(req)
	if err != nil {
		// Unwrap *url.Error: its message embeds the dial URL (userinfo already
		// stripped above, but keep the shape scheme+host only regardless).
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("openapi %s: fetch failed: %v", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openapi %s: HTTP %d", id, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, openapiMaxBody+1))
	if err != nil {
		return nil, fmt.Errorf("openapi %s: read body: %v", id, err)
	}
	if len(data) > openapiMaxBody {
		return nil, fmt.Errorf("openapi %s: spec exceeds %dMB", id, openapiMaxBody>>20)
	}
	return data, nil
}

// buildOpenAPIBatch parses the spec (OpenAPI 3.x / Swagger 2.0, JSON) and
// emits the logical shape: api root → paths → operations → component schemas,
// contains edges plus operation→schema "uses" edges from $refs. Sorted,
// deterministic; non-spec content is a loud error naming the adapter lane.
func buildOpenAPIBatch(id string, data []byte) (*schema.Batch, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		hint := ""
		if looksLikeYAMLSpec(data) {
			hint = " (looks like a YAML spec — this build parses JSON specs only)"
		}
		return nil, fmt.Errorf("openapi %s: content is not JSON%s — non-JSON specs need the adapter-script lane (.ctxoptimize/adapters/)", id, hint)
	}
	var specVer, refPrefix string
	var schemas, secSchemes map[string]any
	if v, ok := doc["openapi"].(string); ok && strings.HasPrefix(v, "3") {
		specVer = "openapi " + v
		refPrefix = "#/components/schemas/"
		comps := asMap(doc["components"])
		schemas = asMap(comps["schemas"])
		secSchemes = asMap(comps["securitySchemes"])
	} else if v, ok := doc["swagger"].(string); ok && v == "2.0" {
		specVer = "swagger 2.0"
		refPrefix = "#/definitions/"
		schemas = asMap(doc["definitions"])
		secSchemes = asMap(doc["securityDefinitions"])
	} else {
		return nil, fmt.Errorf("openapi %s: JSON is not an OpenAPI 3.x or Swagger 2.0 spec — other formats need the adapter-script lane (.ctxoptimize/adapters/)", id)
	}

	info := asMap(doc["info"])
	title, _ := info["title"].(string)
	version, _ := info["version"].(string)
	if title == "" {
		title = "API"
	}
	label := title
	if version != "" {
		label += " v" + version
	}
	b := &schema.Batch{Producer: openapiProducer}
	rootMeta := map[string]string{"spec": specVer, "title": title}
	if version != "" {
		rootMeta["version"] = version
	}
	b.Nodes = append(b.Nodes, schema.Node{
		ID: id, Label: label, Kind: "api", FileType: "schema", Source: id, Metadata: rootMeta,
	})

	// Component schemas first (operation uses-edges point at their ids).
	schemaIDs := map[string]string{}
	for _, name := range sortedKeys(schemas) {
		sid := id + refPrefix + name
		schemaIDs[name] = sid
		meta := map[string]string{}
		if props := propertySummary(asMap(schemas[name])); props != "" {
			meta["properties"] = props
		}
		b.Nodes = append(b.Nodes, schema.Node{
			ID: sid, Label: name, Kind: "schema", FileType: "schema", Source: id, Metadata: meta,
		})
		b.Edges = append(b.Edges, containsEdge(id, sid))
	}

	// Security schemes: allowlisted structural metadata only — example/secret
	// values are stripped by construction.
	for _, name := range sortedKeys(secSchemes) {
		nid := id + "#/securitySchemes/" + name
		raw := asMap(secSchemes[name])
		meta := map[string]string{}
		for _, k := range securitySchemeAllowed {
			if v, ok := raw[k].(string); ok && v != "" {
				meta[k] = v
			}
		}
		b.Nodes = append(b.Nodes, schema.Node{
			ID: nid, Label: name, Kind: "securityScheme", FileType: "schema", Source: id, Metadata: meta,
		})
		b.Edges = append(b.Edges, containsEdge(id, nid))
	}

	paths := asMap(doc["paths"])
	for _, p := range sortedKeys(paths) {
		pathID := id + "#/paths" + p
		b.Nodes = append(b.Nodes, schema.Node{
			ID: pathID, Label: p, Kind: "path", FileType: "schema", Source: id,
		})
		b.Edges = append(b.Edges, containsEdge(id, pathID))
		item := asMap(paths[p])
		shared := paramNames(item["parameters"])
		for _, method := range openapiMethods {
			op, ok := item[method].(map[string]any)
			if !ok {
				continue
			}
			opID := pathID + "/" + method
			meta := map[string]string{}
			if s, ok := op["summary"].(string); ok && s != "" {
				meta["summary"] = s
			}
			if params := append(append([]string{}, shared...), paramNames(op["parameters"])...); len(params) > 0 {
				meta["params"] = strings.Join(params, ", ")
			}
			if codes := sortedKeys(asMap(op["responses"])); len(codes) > 0 {
				meta["responses"] = strings.Join(codes, ", ")
			}
			refs := map[string]bool{}
			collectRefs(op, refPrefix, refs)
			refNames := make([]string, 0, len(refs))
			for r := range refs {
				refNames = append(refNames, r)
			}
			sort.Strings(refNames)
			if len(refNames) > 0 {
				meta["schemas"] = strings.Join(refNames, ", ")
			}
			b.Nodes = append(b.Nodes, schema.Node{
				ID: opID, Label: strings.ToUpper(method) + " " + p, Kind: "operation",
				FileType: "schema", Source: id, Metadata: meta,
			})
			b.Edges = append(b.Edges, containsEdge(pathID, opID))
			for _, r := range refNames {
				if sid, ok := schemaIDs[r]; ok {
					b.Edges = append(b.Edges, schema.Edge{
						Source: opID, Target: sid, Relation: "uses", Confidence: schema.Extracted,
					})
				}
			}
		}
	}
	return b, nil
}

func containsEdge(from, to string) schema.Edge {
	return schema.Edge{Source: from, Target: to, Relation: "contains", Confidence: schema.Extracted}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// propertySummary renders a schema object's properties as "name:type, ..."
// (sorted). Type falls back to the $ref basename, then "object".
func propertySummary(s map[string]any) string {
	props := asMap(s["properties"])
	if len(props) == 0 {
		return ""
	}
	var parts []string
	for _, name := range sortedKeys(props) {
		p := asMap(props[name])
		t, _ := p["type"].(string)
		if t == "" {
			if ref, ok := p["$ref"].(string); ok {
				t = ref[strings.LastIndex(ref, "/")+1:]
			} else {
				t = "object"
			}
		}
		parts = append(parts, name+":"+t)
	}
	return strings.Join(parts, ", ")
}

// paramNames pulls the "name" of each parameter object, in declared order.
func paramNames(v any) []string {
	list, _ := v.([]any)
	var names []string
	for _, item := range list {
		if n, ok := asMap(item)["name"].(string); ok && n != "" {
			names = append(names, n)
		}
	}
	return names
}

// collectRefs walks any JSON value for local "$ref" strings under prefix and
// records the referenced component names.
func collectRefs(v any, prefix string, out map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, vv := range t {
			if k == "$ref" {
				if s, ok := vv.(string); ok && strings.HasPrefix(s, prefix) {
					out[strings.TrimPrefix(s, prefix)] = true
				}
				continue
			}
			collectRefs(vv, prefix, out)
		}
	case []any:
		for _, vv := range t {
			collectRefs(vv, prefix, out)
		}
	}
}

// looksLikeYAMLSpec sniffs non-JSON content for a YAML spec header so the
// error can say WHY it failed, not just that it did.
func looksLikeYAMLSpec(data []byte) bool {
	head := data
	if len(head) > 2048 {
		head = head[:2048]
	}
	for _, line := range strings.Split(string(head), "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "openapi:") || strings.HasPrefix(l, "swagger:") {
			return true
		}
	}
	return false
}
