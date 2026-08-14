// Services registry (ADR 2026-08-13 boundary-model-and-defaults, D5): when
// the DEPENDENCY is the boundary. firebase / gcloud / boto3 / stripe reach
// their vendors with no URL literal in code — the manifest is the evidence.
// The registry is DATA like boundaries.json: a validated JSON file mapping
// service ids to dependency names, hosts, a config-key hint, and (optionally)
// SDK call symbols that resolve straight to endpoints.
//
// Tiers, exactly as the ADR states them:
//
//	dep declared in a manifest → port(consumes) INFERRED (dep is real; the
//	                             call may be dead)
//	SDK symbol at a call site  → EXTRACTED, resolved to the endpoint when
//	                             named (chat.completions.create ⇒
//	                             POST /v1/chat/completions, no URL ever seen)
//	config_hint                → attaches the service's config.env ports
//
// Ladder, mirroring boundaries.json:
//
//	.ctxoptimize/services.json       repo   (committed, reviewed)
//	  ~/ctxoptimize/services/*.json  machine (CTX_OPTIMIZE_SERVICES overrides)
//	    embedded services.json       this package
package boundaries

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/manifests"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

//go:embed services.json
var servicesJSON []byte

// ServiceFile is the on-disk shape of services.json.
type ServiceFile struct {
	Version  int                `json:"version"`
	Services map[string]Service `json:"services"`
}

// Service is one SaaS / platform entry. Deps are ecosystem-prefixed names
// matching the manifests producer's dep ids (`npm:firebase` ⇔ dep:npm/firebase);
// a trailing `*` is a prefix match (`npm:@aws-sdk/*`). Hosts document the
// vendor's addresses; the first concrete (non-wildcard) host becomes the port
// identifier so a URL-literal port and a dep-tier port for the same vendor
// land on the SAME node.
type Service struct {
	Transport  string       `json:"transport,omitempty"` // default network.http
	Match      ServiceMatch `json:"match"`
	ConfigHint string       `json:"config_hint,omitempty"`
	Endpoints  []Endpoint   `json:"endpoints,omitempty"`
}

type ServiceMatch struct {
	Deps  []string `json:"deps,omitempty"`
	Hosts []string `json:"hosts,omitempty"`
}

// Endpoint names one API operation and the SDK symbols that mean it.
type Endpoint struct {
	Method string   `json:"method"`
	Path   string   `json:"path"`
	SDK    []string `json:"sdk,omitempty"`
}

// LoadServices resolves the ladder for root: embedded → machine dir →
// repo file, merged by service id (later wins). Malformed files fail loudly.
func LoadServices(root string) (map[string]Service, error) {
	merged := map[string]Service{}
	apply := func(data []byte, origin string) error {
		var f ServiceFile
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&f); err != nil {
			return fmt.Errorf("%s: %w", origin, err)
		}
		for id, s := range f.Services {
			if err := validateService(id, s); err != nil {
				return fmt.Errorf("%s: %w", origin, err)
			}
			merged[id] = s
		}
		return nil
	}
	if err := apply(servicesJSON, "embedded services"); err != nil {
		return nil, err // a broken embedded file is a build defect
	}
	if dir := servicesMachineDir(); dir != "" {
		ents, _ := os.ReadDir(dir)
		var names []string
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names) // deterministic merge order
		for _, n := range names {
			data, err := os.ReadFile(filepath.Join(dir, n))
			if err != nil {
				return nil, err
			}
			if err := apply(data, filepath.Join(dir, n)); err != nil {
				return nil, err
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, ".ctxoptimize", "services.json")); err == nil {
		if err := apply(data, ".ctxoptimize/services.json"); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

// ServicesMachineDir is where `services add` installs registry files.
func ServicesMachineDir() string { return servicesMachineDir() }

func servicesMachineDir() string {
	if v := os.Getenv("CTX_OPTIMIZE_SERVICES"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "ctxoptimize", "services")
}

func validateService(id string, s Service) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("service with empty id")
	}
	if s.Transport != "" && strings.ToLower(s.Transport) != s.Transport {
		return fmt.Errorf("service %q: transport must be lowercase dotted, got %q", id, s.Transport)
	}
	if len(s.Match.Deps) == 0 && len(s.Match.Hosts) == 0 {
		return fmt.Errorf("service %q: match needs deps or hosts", id)
	}
	for _, d := range s.Match.Deps {
		if !strings.Contains(d, ":") {
			return fmt.Errorf("service %q: dep %q must be ecosystem-prefixed (npm:name, pypi:name, go:module, maven:group:artifact, nuget:pkg)", id, d)
		}
	}
	for _, ep := range s.Endpoints {
		if ep.Method == "" || ep.Path == "" {
			return fmt.Errorf("service %q: endpoint needs method and path", id)
		}
		for _, sym := range ep.SDK {
			if strings.TrimSpace(sym) == "" {
				return fmt.Errorf("service %q: empty sdk symbol", id)
			}
		}
	}
	return nil
}

// ValidateServiceFile parses and validates one registry file — the check
// `services add` runs before installing anything.
func ValidateServiceFile(data []byte) (*ServiceFile, error) {
	var f ServiceFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, err
	}
	if len(f.Services) == 0 {
		return nil, fmt.Errorf("no services in file")
	}
	ids := make([]string, 0, len(f.Services))
	for id := range f.Services {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := validateService(id, f.Services[id]); err != nil {
			return nil, err
		}
	}
	return &f, nil
}

// --- extraction-time wiring -------------------------------------------------

// serviceIdentifier: the first concrete (non-wildcard) host, else the id.
// A concrete host makes the dep-tier port and any URL-literal port for the
// same vendor collapse into one node.
func serviceIdentifier(id string, s Service) string {
	for _, h := range s.Match.Hosts {
		if !strings.Contains(h, "*") {
			return h
		}
	}
	return id
}

func serviceTransport(s Service) string {
	if s.Transport != "" {
		return s.Transport
	}
	return "network.http"
}

// sdkMatcher is one compiled SDK-symbol pattern: `\bchat\.completions\.create\s*\(`.
type sdkMatcher struct {
	svcID string
	svc   Service
	ep    Endpoint
	re    *regexp.Regexp
}

// sdkExcludes keeps the call-site scan out of vendored corpora — the spike's
// precision failure, same list the default rules carry.
var sdkExcludes = []string{"testdata/", "benchmarks/"}

// sdkSourceExts bounds the call-site scan — same idea as rule ext-bounding.
var sdkSourceExts = map[string]bool{
	".go": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".mjs": true, ".cjs": true, ".py": true, ".java": true, ".kt": true,
	".cs": true, ".rb": true, ".php": true, ".swift": true,
}

func compileSDKMatchers(services map[string]Service) []sdkMatcher {
	ids := make([]string, 0, len(services))
	for id := range services {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []sdkMatcher
	for _, id := range ids {
		s := services[id]
		for _, ep := range s.Endpoints {
			for _, sym := range ep.SDK {
				re, err := regexp.Compile(`\b` + regexp.QuoteMeta(sym) + `\s*\(`)
				if err != nil {
					continue // QuoteMeta makes this unreachable; belt-and-braces
				}
				out = append(out, sdkMatcher{svcID: id, svc: s, ep: ep, re: re})
			}
		}
	}
	return out
}

// ensureServicePort creates (or reuses) the service's port node and returns
// its id. Reuse matters: the URL-literal rule may already have made this node.
func ensureServicePort(nodes map[string]schema.Node, svcID string, s Service) string {
	transport := serviceTransport(s)
	ident := Normalize(transport, serviceIdentifier(svcID, s))
	id := "port:" + transport + ":>" + ident
	n, ok := nodes[id]
	if !ok {
		n = schema.Node{
			ID: id, Label: ident, Kind: "port", FileType: "boundary",
			Source: "port://" + transport + "/" + ident,
			Metadata: map[string]string{
				"direction": "consumes", "transport": transport, "identifier": ident,
			},
		}
	}
	if n.Metadata["svc.id"] == "" {
		n.Metadata["svc.id"] = svcID
	}
	if n.Metadata["otel.server.address"] == "" && strings.Contains(ident, ".") {
		n.Metadata["otel.server.address"] = ident
	}
	nodes[id] = n
	return id
}

// applySDKSite records one EXTRACTED call-site hit during the walk.
func applySDKSite(m sdkMatcher, rel string, content []byte,
	nodes map[string]schema.Node, edges map[edgeKey]schema.Edge) {
	locs := m.re.FindAllIndex(content, -1)
	if len(locs) == 0 {
		return
	}
	pid := ensureServicePort(nodes, m.svcID, m.svc)
	k := edgeKey{rel, pid}
	if e, exists := edges[k]; exists {
		// A call site outranks a dep declaration or an ambiguous literal.
		if e.Confidence != schema.Extracted {
			e.Confidence = schema.Extracted
			edges[k] = e
		}
		return
	}
	line := 1 + bytes.Count(content[:locs[0][0]], []byte{'\n'})
	edges[k] = schema.Edge{
		Source: rel, Target: pid, Relation: "consumes", Confidence: schema.Extracted,
		Metadata: map[string]string{
			"synthesized_by": "boundaries", "rule": "service:" + m.svcID,
			"site":                     fmt.Sprintf("%s:L%d", rel, line),
			"otel.http.request.method": m.ep.Method,
			"otel.url.path":            m.ep.Path,
		},
	}
}

// depMatches reports whether pattern (ns:name, trailing `*` = prefix) matches
// the dep node key `ns/name`.
func depMatches(pattern, key string) bool {
	p := strings.Replace(pattern, ":", "/", 1)
	if strings.HasSuffix(p, "*") {
		return strings.HasPrefix(key, strings.TrimSuffix(p, "*"))
	}
	return p == key
}

// joinServices runs after the walk: manifests give the declared deps, the
// registry names which of them ARE a boundary, and config_hint stitches the
// service's env keys to it. Everything lands in the same nodes/edges maps so
// determinism and scope-by-join are inherited.
func joinServices(root string, exclude []string, services map[string]Service,
	nodes map[string]schema.Node, edges map[edgeKey]schema.Edge) error {
	if len(services) == 0 {
		return nil
	}
	mb, err := manifests.ExtractExcluding(root, exclude)
	if err != nil {
		return fmt.Errorf("services join: %w", err)
	}
	// dep key `ns/name` → sorted manifest files that declare it.
	declared := map[string][]string{}
	for _, e := range mb.Edges {
		if e.Relation != "declares" || !strings.HasPrefix(e.Target, "dep:") {
			continue
		}
		key := strings.TrimPrefix(e.Target, "dep:")
		declared[key] = append(declared[key], e.Source)
	}
	depKeys := make([]string, 0, len(declared))
	for k := range declared {
		sort.Strings(declared[k])
		depKeys = append(depKeys, k)
	}
	sort.Strings(depKeys)

	ids := make([]string, 0, len(services))
	for id := range services {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, svcID := range ids {
		s := services[svcID]
		for _, pat := range s.Match.Deps {
			for _, key := range depKeys {
				if !depMatches(pat, key) {
					continue
				}
				pid := ensureServicePort(nodes, svcID, s)
				for _, mf := range declared[key] {
					k := edgeKey{mf, pid}
					if _, exists := edges[k]; exists {
						continue // an EXTRACTED call site already outranks this
					}
					edges[k] = schema.Edge{
						Source: mf, Target: pid, Relation: "consumes",
						Confidence: schema.Inferred, // dep is real; the call may be dead
						Metadata: map[string]string{
							"synthesized_by": "boundaries", "rule": "service:" + svcID,
							"site": mf, "svc.dep": key,
						},
					}
				}
			}
		}
	}

	// config_hint: attach the service's config.env ports — only to services
	// this repo actually surfaced (a hint alone asserts nothing).
	for _, svcID := range ids {
		s := services[svcID]
		if s.ConfigHint == "" {
			continue
		}
		pid := "port:" + serviceTransport(s) + ":>" + serviceIdentifier(svcID, s)
		if n, ok := nodes[pid]; !ok || n.Metadata["svc.id"] != svcID {
			continue
		}
		envIDs := make([]string, 0)
		for id, n := range nodes {
			if n.Metadata["transport"] == "config.env" && n.Metadata["direction"] == "consumes" &&
				strings.Contains(n.Metadata["identifier"], s.ConfigHint) {
				envIDs = append(envIDs, id)
			}
		}
		sort.Strings(envIDs)
		for _, envID := range envIDs {
			k := edgeKey{envID, pid}
			if _, exists := edges[k]; exists {
				continue
			}
			edges[k] = schema.Edge{
				Source: envID, Target: pid, Relation: "references",
				Confidence: schema.Inferred,
				Metadata: map[string]string{
					"synthesized_by": "boundaries", "rule": "service:" + svcID,
					"site": "config_hint:" + s.ConfigHint,
				},
			}
		}
	}
	return nil
}
