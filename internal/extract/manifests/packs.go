// packs.go — drop-in MANIFEST PACKS, the declarative counterpart of the core
// recognizers, mirroring route/grammar packs. Unlike routes (real frameworks
// need visitor state), custom manifests are usually "a path in a structured
// file" — genuinely declarative-friendly. A pack is one <name>.json
// discovered at add time from two locations (repo wins on name collision):
//
//  1. <store-root>/manifests (default ~/ctxoptimize/manifests;
//     CTX_OPTIMIZE_STORE relocates it — hermetic tests) — machine-wide
//  2. <repo>/.ctxoptimize/manifests — travels with the repo, committable
//
// Pack shape:
//
//	{"name": "internal-deps",
//	 "rules": [
//	   {"file": "*.deps.json", "format": "json", "path": "libraries.*",
//	    "emit": "dependency", "namespace": "internal"},
//	   {"file": "*.build.xml", "format": "xml", "path": "target/@name",
//	    "emit": "task"}]}
//
// The selector language is deliberately TINY (see design.md): dot path with
// a `*` wildcard for json/yaml, element path with a trailing /@attr for xml.
// If it can't express something, the answer is an adapter script (the
// universal door), not a bigger language. Malformed packs fail the add
// LOUDLY (grammar/route-pack precedent); malformed USER files a valid rule
// happens to match are skipped silently, same as the core recognizers.
package manifests

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/yamlwalk"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

// ManifestRule is one declarative extraction rule.
type ManifestRule struct {
	File      string `json:"file"`      // glob matched against the BASENAME
	Format    string `json:"format"`    // json | xml | yaml
	Path      string `json:"path"`      // tiny selector (see package comment)
	Emit      string `json:"emit"`      // dependency | task
	Namespace string `json:"namespace"` // dep namespace / task label prefix (default: pack name)
}

// ManifestPack is one discovered pack; File records where it was loaded from.
type ManifestPack struct {
	Name  string         `json:"name"`
	Rules []ManifestRule `json:"rules"`
	File  string         `json:"-"`
}

var validFormats = map[string]bool{"json": true, "xml": true, "yaml": true}
var validEmits = map[string]bool{"dependency": true, "task": true}

// ParseManifestPack decodes and validates one pack. origin names the source
// in errors (a file path or URL) so a bad pack is a one-step fix.
func ParseManifestPack(data []byte, origin string) (*ManifestPack, error) {
	var p ManifestPack
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("manifest pack %s: %w", origin, err)
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, fmt.Errorf("manifest pack %s: name is required", origin)
	}
	if len(p.Rules) == 0 {
		return nil, fmt.Errorf("manifest pack %s: at least one rule is required", origin)
	}
	for i, r := range p.Rules {
		if strings.TrimSpace(r.File) == "" {
			return nil, fmt.Errorf("manifest pack %s: rules[%d]: file glob is required", origin, i)
		}
		if !validFormats[r.Format] {
			return nil, fmt.Errorf("manifest pack %s: rules[%d]: format %q not in {json,xml,yaml}", origin, i, r.Format)
		}
		if strings.TrimSpace(r.Path) == "" {
			return nil, fmt.Errorf("manifest pack %s: rules[%d]: path selector is required", origin, i)
		}
		if !validEmits[r.Emit] {
			return nil, fmt.Errorf("manifest pack %s: rules[%d]: emit %q not in {dependency,task}", origin, i, r.Emit)
		}
		if _, err := path.Match(r.File, "probe"); err != nil {
			return nil, fmt.Errorf("manifest pack %s: rules[%d]: bad file glob %q", origin, i, r.File)
		}
	}
	p.File = origin
	return &p, nil
}

// MachineManifestsDir is where machine-wide packs live: <store-root>/manifests.
func MachineManifestsDir() (string, error) {
	root, err := store.Root("")
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "manifests"), nil
}

// RepoManifestsDir is the repo-level pack dir (committable).
func RepoManifestsDir(repo string) string {
	return filepath.Join(repo, ".ctxoptimize", "manifests")
}

// LoadManifestPacks discovers packs for a repo. Search order: machine dir
// first, repo dir second — later wins on name collision, so repo packs beat
// machine packs (grammar/route-pack precedence). Malformed packs fail loudly.
func LoadManifestPacks(repo string) ([]ManifestPack, error) {
	machine, err := MachineManifestsDir()
	if err != nil {
		return nil, err
	}
	byName := map[string]ManifestPack{}
	var order []string
	for _, dir := range []string{machine, RepoManifestsDir(repo)} {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names) // deterministic within a dir
		for _, n := range names {
			p := filepath.Join(dir, n)
			data, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			pack, err := ParseManifestPack(data, p)
			if err != nil {
				return nil, err
			}
			if _, seen := byName[pack.Name]; !seen {
				order = append(order, pack.Name)
			}
			byName[pack.Name] = *pack
		}
	}
	packs := make([]ManifestPack, 0, len(order))
	for _, n := range order {
		packs = append(packs, byName[n])
	}
	return packs, nil
}

// boundRule is a rule with its pack's identity attached for provenance.
type boundRule struct {
	rule      ManifestRule
	packName  string
	namespace string
}

// matchPackRules returns every pack rule whose file glob matches basename.
func matchPackRules(packs []ManifestPack, basename string) []boundRule {
	var out []boundRule
	for _, p := range packs {
		for _, r := range p.Rules {
			if ok, _ := path.Match(r.File, basename); !ok {
				continue
			}
			ns := r.Namespace
			if ns == "" {
				ns = p.Name
			}
			out = append(out, boundRule{rule: r, packName: p.Name, namespace: ns})
		}
	}
	return out
}

// pair is one selector hit: a name and an optional version spec.
type pair struct{ name, version string }

// applyPackRule runs one bound rule against one file's content. Files the
// rule matched but the parser can't read are skipped silently — the pack was
// validated; the user's data file is not ours to fail an add over.
func applyPackRule(c *collector, br boundRule, rel string, data []byte) {
	var hits []pair
	switch br.rule.Format {
	case "json":
		hits = jsonSelect(data, br.rule.Path)
	case "yaml":
		hits = yamlSelect(string(data), br.rule.Path)
	case "xml":
		hits = xmlSelect(data, br.rule.Path)
	}
	channel := "manifest-pack:" + br.packName
	for _, h := range hits {
		if h.name == "" {
			continue
		}
		switch br.rule.Emit {
		case "dependency":
			id := c.depNode(br.namespace, h.name)
			md := map[string]string{"synthesized_by": channel}
			if h.version != "" {
				md["version_spec"] = h.version
			}
			c.edge(schema.Edge{
				Source: rel, Target: id, Relation: "declares",
				Confidence: schema.Extracted, Metadata: md,
			})
		case "task":
			id := rel + "::task:" + h.name
			c.node(schema.Node{
				ID: id, Label: br.namespace + ":" + h.name, Kind: "task",
				FileType: "manifest", Source: rel,
				Metadata: map[string]string{"synthesized_by": channel},
			})
			c.edge(schema.Edge{Source: rel, Target: id, Relation: "contains", Confidence: schema.Extracted})
		}
	}
}

// jsonSelect walks a dot path (`*` = every map key / array element) through
// decoded JSON. Yield semantics (the whole selector contract):
//   - a trailing `*` over an OBJECT yields (key, value-if-string) pairs
//   - a string value yields itself as a name
//   - an array of strings yields one name per element
//   - anything else is skipped silently
func jsonSelect(data []byte, selector string) []pair {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil
	}
	return walkAny(root, strings.Split(selector, "."))
}

func walkAny(v any, segs []string) []pair {
	if len(segs) == 0 {
		return yieldAny(v)
	}
	seg, rest := segs[0], segs[1:]
	switch t := v.(type) {
	case map[string]any:
		if seg == "*" {
			if len(rest) == 0 {
				return yieldMapEntries(t)
			}
			var out []pair
			for _, k := range sortedKeys(t) {
				out = append(out, walkAny(t[k], rest)...)
			}
			return out
		}
		child, ok := t[seg]
		if !ok {
			return nil
		}
		return walkAny(child, rest)
	case []any:
		if seg != "*" {
			return nil
		}
		var out []pair
		for _, e := range t {
			out = append(out, walkAny(e, rest)...)
		}
		return out
	}
	return nil
}

func yieldAny(v any) []pair {
	switch t := v.(type) {
	case string:
		return []pair{{name: t}}
	case []any:
		var out []pair
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, pair{name: s})
			}
		}
		return out
	}
	return nil
}

func yieldMapEntries(m map[string]any) []pair {
	var out []pair
	for _, k := range sortedKeys(m) {
		p := pair{name: k}
		if s, ok := m[k].(string); ok {
			p.version = s
		}
		out = append(out, p)
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// yamlSelect is jsonSelect over the shared yaml indent walker: dot path,
// `*` wildcard. A trailing `*` yields (key, scalar-value) mapping entries;
// a concrete final segment yields its scalar value (or its list items).
func yamlSelect(content string, selector string) []pair {
	var out []pair
	all := strings.Split(content, "\n")
	start := 0
	flush := func(end int) {
		if end <= start {
			return
		}
		ls := yamlwalk.Parse(all[start:end], start)
		out = append(out, yamlWalkPath(ls, 0, len(ls), -1, strings.Split(selector, "."))...)
	}
	for i, line := range all {
		if strings.TrimSpace(line) == "---" {
			flush(i)
			start = i + 1
		}
	}
	flush(len(all))
	return out
}

// yamlWalkPath resolves selector segments inside ls[from:to], where matched
// lines must sit at the block's own child indent (parentIndent < indent, and
// the FIRST line's indent inside the block defines the level).
func yamlWalkPath(ls []yamlwalk.Line, from, to, parentIndent int, segs []string) []pair {
	if from >= to {
		return nil
	}
	level := -1
	for i := from; i < to; i++ {
		if ls[i].Indent > parentIndent {
			level = ls[i].Indent
			break
		}
	}
	if level < 0 {
		return nil
	}
	seg := segs[0]
	rest := segs[1:]
	var out []pair
	for i := from; i < to; i++ {
		if ls[i].Indent != level || ls[i].Key == "" {
			continue
		}
		if seg != "*" && ls[i].Key != seg {
			continue
		}
		if len(rest) == 0 {
			if seg == "*" {
				out = append(out, pair{name: ls[i].Key, version: ls[i].Val})
			} else if ls[i].Val != "" {
				out = append(out, pair{name: ls[i].Val})
			} else {
				// list items under the key: `- name` scalars
				end := yamlwalk.Span(ls, i)
				for j := i + 1; j < end; j++ {
					if ls[j].List && ls[j].Key == "" && ls[j].Val != "" {
						out = append(out, pair{name: ls[j].Val})
					}
				}
			}
			continue
		}
		out = append(out, yamlWalkPath(ls, i+1, yamlwalk.Span(ls, i), ls[i].Indent, rest)...)
	}
	return out
}

// xmlSelect walks an element path (`a/b/c`, `*` = any element) with an
// optional trailing `/@attr`. A match yields the attribute value (with
// @attr) or the element's trimmed character content. No version channel —
// xml rules yield names only.
func xmlSelect(data []byte, selector string) []pair {
	segs := strings.Split(selector, "/")
	attr := ""
	if last := segs[len(segs)-1]; strings.HasPrefix(last, "@") {
		attr = strings.TrimPrefix(last, "@")
		segs = segs[:len(segs)-1]
	}
	if len(segs) == 0 {
		return nil
	}
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	var stack []string
	var out []pair
	var textDepth int = -1
	var text strings.Builder
	matches := func() bool {
		if len(stack) != len(segs) {
			return false
		}
		for i, s := range segs {
			if s != "*" && s != stack[i] {
				return false
			}
		}
		return true
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			break // malformed or EOF — yield what parsed cleanly
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			if matches() {
				if attr != "" {
					for _, a := range t.Attr {
						if a.Name.Local == attr && a.Value != "" {
							out = append(out, pair{name: a.Value})
						}
					}
				} else {
					textDepth = len(stack)
					text.Reset()
				}
			}
		case xml.CharData:
			if textDepth == len(stack) {
				text.Write(t)
			}
		case xml.EndElement:
			if textDepth == len(stack) {
				if v := strings.TrimSpace(text.String()); v != "" {
					out = append(out, pair{name: v})
				}
				textDepth = -1
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return out
}
