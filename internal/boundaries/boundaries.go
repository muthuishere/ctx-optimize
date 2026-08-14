// Package boundaries is the tier-1 boundary producer (ADR 2026-08-13
// boundary-model-and-defaults, D1–D3): declarative rules in
// `boundaries.json` turn boundary crossings — env reads, spawned processes,
// outbound URLs, storage keys — into `port` nodes with provides/consumes
// edges. The rules are DATA, never code: a JSON diff is reviewable, nothing
// executes at gather time, and a repo rule that proves general graduates into
// the embedded defaults verbatim.
//
// Precedence ladder, matching grammar packs:
//
//	.ctxoptimize/boundaries.json      repo   (committed, reviewed)
//	  ~/ctxoptimize/boundaries/*.json machine (CTX_OPTIMIZE_BOUNDARIES overrides, tests)
//	    embedded defaults             this package
//
// Merged by rule ID — repo beats machine beats embedded, so a team can
// correct ONE rule without forking the set. Rules use Go RE2 — the same
// engine as `ctx-optimize search`, so a rule and its ground-truth count can
// never disagree about regex semantics.
package boundaries

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// Producer is the provenance tag; store.Replace scopes truth per producer.
const Producer = "boundaries"

//go:embed defaults.json
var defaultsJSON []byte

// maxFileBytes mirrors the code producer's cap — same file set discipline.
const maxFileBytes = 2 << 20

// File is the on-disk shape of boundaries.json.
type File struct {
	Version    int    `json:"version"`
	Boundaries []Rule `json:"boundaries"`
}

// Rule is one declarative boundary. Verified is carried opaquely — the
// authoring loop (ADR 2026-08-13-boundary-authoring) owns its shape; the
// engine only executes.
type Rule struct {
	ID        string `json:"id"`
	Transport string `json:"transport"`
	Direction string `json:"direction"`
	When      struct {
		Ext []string `json:"ext"`
	} `json:"when"`
	Exclude struct {
		Path []string `json:"path"`
	} `json:"exclude"`
	Match []Matcher `json:"match"`
	Flag  *struct {
		WhenIdentifierMatches string            `json:"when_identifier_matches"`
		Set                   map[string]string `json:"set"`
	} `json:"flag,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Tier     string            `json:"tier,omitempty"` // default EXTRACTED; authoring derives it
	Verified json.RawMessage   `json:"verified,omitempty"`

	res  []*regexp.Regexp
	flag *regexp.Regexp
}

// Matcher is one regex with the capture group that holds the identifier.
type Matcher struct {
	Re         string `json:"re"`
	Identifier int    `json:"identifier"`
}

// Load resolves the ladder for root and returns compiled, validated rules.
// A malformed file fails loudly (fail-closed, like the schema door) — a
// silently dropped rule would make every later count a lie.
func Load(root string) ([]Rule, error) {
	merged := map[string]Rule{}
	apply := func(data []byte, origin string) error {
		var f File
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&f); err != nil {
			return fmt.Errorf("%s: %w", origin, err)
		}
		for _, r := range f.Boundaries {
			merged[r.ID] = r
		}
		return nil
	}
	if err := apply(defaultsJSON, "embedded defaults"); err != nil {
		return nil, err // a broken embedded file is a build defect
	}
	if dir := machineDir(); dir != "" {
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
	if data, err := os.ReadFile(filepath.Join(root, ".ctxoptimize", "boundaries.json")); err == nil {
		if err := apply(data, ".ctxoptimize/boundaries.json"); err != nil {
			return nil, err
		}
	}

	ids := make([]string, 0, len(merged))
	for id := range merged {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rules := make([]Rule, 0, len(ids))
	for _, id := range ids {
		r := merged[id]
		if err := compile(&r); err != nil {
			return nil, fmt.Errorf("rule %q: %w", id, err)
		}
		rules = append(rules, r)
	}
	return rules, nil
}

func machineDir() string {
	if v := os.Getenv("CTX_OPTIMIZE_BOUNDARIES"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "ctxoptimize", "boundaries")
}

func compile(r *Rule) error {
	if r.ID == "" {
		return fmt.Errorf("missing id")
	}
	if r.Direction != "provides" && r.Direction != "consumes" {
		return fmt.Errorf("direction must be provides|consumes, got %q", r.Direction)
	}
	if r.Transport == "" || strings.ToLower(r.Transport) != r.Transport {
		return fmt.Errorf("transport must be lowercase dotted, got %q", r.Transport)
	}
	switch r.Tier {
	case "", schema.Extracted, schema.Inferred, schema.Ambiguous:
	default:
		return fmt.Errorf("tier must be EXTRACTED|INFERRED|AMBIGUOUS, got %q", r.Tier)
	}
	if len(r.Match) == 0 {
		return fmt.Errorf("no match patterns")
	}
	r.res = r.res[:0]
	for _, m := range r.Match {
		re, err := regexp.Compile(m.Re)
		if err != nil {
			return fmt.Errorf("pattern %q: %w", m.Re, err)
		}
		if m.Identifier < 0 || m.Identifier > re.NumSubexp() {
			return fmt.Errorf("pattern %q: identifier group %d out of range", m.Re, m.Identifier)
		}
		r.res = append(r.res, re)
	}
	if r.Flag != nil {
		re, err := regexp.Compile(r.Flag.WhenIdentifierMatches)
		if err != nil {
			return fmt.Errorf("flag pattern: %w", err)
		}
		r.flag = re
	}
	return nil
}

// Extract runs the merged rule set over root.
func Extract(root string) (*schema.Batch, error) { return ExtractExcluding(root, nil) }

// ExtractExcluding is Extract with subtrees dropped — the multi-module fan-out
// passes child-module dirs, same contract as every other producer.
func ExtractExcluding(root string, exclude []string) (*schema.Batch, error) {
	batch := &schema.Batch{Producer: Producer}
	rules, err := Load(root)
	if err != nil {
		return nil, err
	}
	services, err := LoadServices(root)
	if err != nil {
		return nil, err
	}
	sdk := compileSDKMatchers(services)
	if len(rules) == 0 && len(services) == 0 {
		return batch, nil
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	exAbs := map[string]bool{}
	for _, ex := range exclude {
		if a, aerr := filepath.Abs(ex); aerr == nil {
			exAbs[a] = true
		}
	}
	// The union of rule extensions bounds the walk: a repo of .c files pays
	// nothing for a webstorage rule.
	extRules := map[string][]*Rule{}
	for i := range rules {
		r := &rules[i]
		if len(r.When.Ext) == 0 {
			extRules["*"] = append(extRules["*"], r)
			continue
		}
		for _, e := range r.When.Ext {
			extRules[e] = append(extRules[e], r)
		}
	}
	rulesFor := func(name string) []*Rule {
		out := extRules["*"]
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			out = append(out, extRules[name[i:]]...)
		}
		return out
	}

	nodes := map[string]schema.Node{}
	edges := map[edgeKey]schema.Edge{}

	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		name := d.Name()
		if d.IsDir() {
			if exAbs[path] {
				return filepath.SkipDir
			}
			if path != absRoot && (strings.HasPrefix(name, ".") || name == "node_modules" ||
				name == "vendor" || name == "target" || name == "dist" || name == "build" ||
				strings.HasSuffix(name, "-out")) {
				return filepath.SkipDir
			}
			return nil
		}
		rs := rulesFor(name)
		var sdkRS []sdkMatcher
		if len(sdk) > 0 {
			if i := strings.LastIndexByte(name, '.'); i >= 0 && sdkSourceExts[name[i:]] {
				sdkRS = sdk
			}
		}
		if len(rs) == 0 && len(sdkRS) == 0 {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.Size() > maxFileBytes || !info.Mode().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(absRoot, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		content, cerr := os.ReadFile(path)
		if cerr != nil || bytes.IndexByte(content[:min(len(content), 8192)], 0) >= 0 {
			return nil // unreadable or binary
		}
		for _, r := range rs {
			if excludedPath(rel, r.Exclude.Path) {
				continue
			}
			applyRule(r, rel, content, nodes, edges)
		}
		if len(sdkRS) > 0 && !excludedPath(rel, sdkExcludes) {
			for _, m := range sdkRS {
				applySDKSite(m, rel, content, nodes, edges)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := joinServices(root, exclude, services, nodes, edges); err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids) // deterministic batch (store artifacts stay git-diffable)
	for _, id := range ids {
		batch.Nodes = append(batch.Nodes, nodes[id])
	}
	eks := make([]edgeKey, 0, len(edges))
	for k := range edges {
		eks = append(eks, k)
	}
	sort.Slice(eks, func(i, j int) bool {
		if eks[i].src != eks[j].src {
			return eks[i].src < eks[j].src
		}
		return eks[i].tgt < eks[j].tgt
	})
	for _, k := range eks {
		batch.Edges = append(batch.Edges, edges[k])
	}
	markScope(batch)
	return batch, nil
}

func excludedPath(rel string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(rel, p) || strings.Contains(rel, "/"+p) {
			return true
		}
	}
	return false
}

type edgeKey struct{ src, tgt string }

func applyRule(r *Rule, rel string, content []byte, nodes map[string]schema.Node, edges map[edgeKey]schema.Edge) {
	for ri, re := range r.res {
		gi := r.Match[ri].Identifier
		for _, m := range re.FindAllSubmatchIndex(content, -1) {
			var ident string
			if gi > 0 && 2*gi+1 < len(m) && m[2*gi] >= 0 {
				ident = string(content[m[2*gi]:m[2*gi+1]])
			}
			if ident == "" {
				continue
			}
			tier := r.Tier
			if tier == "" {
				tier = schema.Extracted
			}
			meta := map[string]string{}
			// A computed identifier is an admission, not a fact: the graph
			// reports it AMBIGUOUS and keeps the template shape.
			if strings.Contains(ident, "${") || strings.Contains(ident, "%s") || strings.Contains(ident, "%d") {
				tier = schema.Ambiguous
				meta["resolved"] = "dynamic"
			}
			sig := ">"
			if r.Direction == "provides" {
				sig = "<"
			}
			id := "port:" + r.Transport + ":" + sig + ident
			n, ok := nodes[id]
			if !ok {
				nm := map[string]string{
					"direction": r.Direction, "transport": r.Transport, "identifier": ident,
				}
				for k, v := range meta {
					nm[k] = v
				}
				for k, v := range r.Metadata {
					nm[k] = strings.ReplaceAll(v, "$identifier", ident)
				}
				if r.flag != nil && r.flag.MatchString(ident) {
					for k, v := range r.Flag.Set {
						nm[k] = v
					}
				}
				n = schema.Node{
					ID: id, Label: ident, Kind: "port", FileType: "boundary",
					Source: "port://" + r.Transport + "/" + ident, Metadata: nm,
				}
				nodes[id] = n
			}
			k := edgeKey{rel, id}
			if e, exists := edges[k]; exists {
				// Keep the strongest tier seen for this file→port pair.
				if e.Confidence == schema.Ambiguous && tier != schema.Ambiguous {
					e.Confidence = tier
					edges[k] = e
				}
				continue
			}
			line := 1 + bytes.Count(content[:m[0]], []byte{'\n'})
			edges[k] = schema.Edge{
				Source: rel, Target: id, Relation: r.Direction, Confidence: tier,
				Metadata: map[string]string{
					"synthesized_by": "boundaries", "rule": r.ID,
					"site": fmt.Sprintf("%s:L%d", rel, line),
				},
			}
		}
	}
}

// markScope computes scope BY JOIN (ADR D1): a consumes port is internal iff
// its identifier is provided somewhere in this same gather; external
// otherwise. Recomputed every gather, so moving a service in or out of the
// repo flips it without touching any rule.
func markScope(b *schema.Batch) {
	provided := map[string]bool{}
	for _, n := range b.Nodes {
		if n.Metadata["direction"] == "provides" {
			provided[n.Metadata["transport"]+"\x00"+n.Metadata["identifier"]] = true
		}
	}
	for i, n := range b.Nodes {
		if n.Metadata["direction"] != "consumes" {
			continue
		}
		if provided[n.Metadata["transport"]+"\x00"+n.Metadata["identifier"]] {
			b.Nodes[i].Metadata["scope"] = "internal"
		} else {
			b.Nodes[i].Metadata["scope"] = "external"
		}
	}
}
