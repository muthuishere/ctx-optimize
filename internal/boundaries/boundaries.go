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
	// AST is the matcher (ADR 2026-08-14): node shapes evaluated inside the
	// code extractor's existing walk. This is how every shipped rule works.
	AST []ASTMatch `json:"ast,omitempty"`
	// Match is the RAW-BYTE escape hatch, and it is opt-in: a rule that uses
	// it must also declare `"scan": "raw"`. It exists for boundaries in files
	// no grammar parses (.env, .txt, plain config) — the honest limit of an
	// AST lane. Regex also still owns `search` and verify's ground truth,
	// which must stay independent of the capture mechanism.
	Match []Matcher `json:"match,omitempty"`
	Scan  string    `json:"scan,omitempty"` // "" (ast) | "raw"
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
	if len(r.AST) == 0 && len(r.Match) == 0 {
		return fmt.Errorf("no matcher: declare `ast` (node shapes) or `match` with `scan:\"raw\"`")
	}
	if len(r.AST) > 0 && len(r.Match) > 0 {
		return fmt.Errorf("declare either `ast` or `match`, not both")
	}
	if len(r.Match) > 0 && r.Scan != "raw" {
		return fmt.Errorf("`match` is the raw-byte escape hatch and must declare `\"scan\": \"raw\"` (ADR 2026-08-14 D3)")
	}
	if len(r.AST) > 0 && r.Scan != "" {
		return fmt.Errorf("`scan` applies to `match` rules only, got %q", r.Scan)
	}
	for i := range r.AST {
		if err := validateAST(&r.AST[i]); err != nil {
			return fmt.Errorf("ast[%d]: %w", i, err)
		}
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
	// A rule's own metadata is applied to the port node AFTER the engine has
	// filled the reserved fields, so an engine-owned key there does not add a
	// fact — it REPLACES one. `metadata: {"direction": "provides"}` on a
	// `consumes` rule shipped the port under PROVIDES, inverting the headline
	// split of the `boundaries` verb and the input to the scope join. Reject at
	// LOAD, with the other malformed shapes, rather than skipping silently: the
	// schema door already fails closed on a bare un-namespaced key, and the
	// engine-owned keys were the hole in that fence (ADR
	// 2026-08-15-authoring-loop-unenforced, D1). `flag.set` writes into the same
	// map and had the same hole. Namespaced metadata (`otel.*`/`pack.*`/`org.*`)
	// and the author-owned `sensitive` are untouched.
	for _, k := range sortedKeys(r.Metadata) {
		if schema.PortMetadataEngineOwned(k) {
			return fmt.Errorf("metadata key %q is set by the engine and a rule may not overwrite it (namespaced keys — otel.*/pack.*/org.* — are open)", k)
		}
	}
	if r.Flag != nil {
		for _, k := range sortedKeys(r.Flag.Set) {
			if schema.PortMetadataEngineOwned(k) {
				return fmt.Errorf("flag.set key %q is set by the engine and a rule may not overwrite it", k)
			}
		}
		re, err := regexp.Compile(r.Flag.WhenIdentifierMatches)
		if err != nil {
			return fmt.Errorf("flag pattern: %w", err)
		}
		r.flag = re
	}
	return nil
}

// sortedKeys keeps the rejection message deterministic when a rule carries more
// than one bad key — otherwise the error text changes between runs.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Extract runs the raw-scan rules and the services join over root. It is the
// STANDALONE entry (tests, tooling); the gather uses the code extractor's
// ExtractPathsWithBoundaries for AST rules and ExtractRaw for this pass, so
// the services join happens exactly once.
func Extract(root string) (*schema.Batch, error) { return extractRaw(root, nil, true) }

// ExtractExcluding is the gather's raw-scan pass: escape-hatch rules only,
// NO services join (Assemble already did it — joining twice would emit the
// same port node from two batches).
func ExtractExcluding(root string, exclude []string) (*schema.Batch, error) {
	return extractRaw(root, exclude, false)
}

func extractRaw(root string, exclude []string, withServices bool) (*schema.Batch, error) {
	batch := &schema.Batch{Producer: Producer}
	rules, err := Load(root)
	if err != nil {
		return nil, err
	}
	// Only RAW-SCAN rules walk here (ADR 2026-08-14 D3). Every shipped rule
	// is an `ast` rule and rides the code extractor's parse instead — that is
	// the whole point, and it is why this walk normally visits nothing.
	var raw []Rule
	for _, r := range rules {
		if r.Scan == "raw" {
			raw = append(raw, r)
		}
	}
	rules = raw
	services, err := LoadServices(root)
	if err != nil {
		return nil, err
	}
	// SDK call sites are found on the AST now (probeSDK); this pass keeps only
	// the raw-byte escape hatch, so a repo with no such rule walks nothing.
	var sdk []sdkMatcher
	if withServices {
		sdk = compileSDKMatchers(services)
	}
	if len(rules) == 0 && len(sdk) == 0 {
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
	if err := finish(root, exclude, withServices, batch, nodes, edges); err != nil {
		return nil, err
	}
	return batch, nil
}

// finish is the shared tail: the services join, deterministic ordering, and
// scope-by-join. Both lanes end here so their output cannot drift apart.
func finish(root string, exclude []string, withServices bool, batch *schema.Batch, nodes map[string]schema.Node, edges map[edgeKey]schema.Edge) error {
	if withServices {
		services, err := LoadServices(root)
		if err != nil {
			return err
		}
		if err := joinServices(root, exclude, services, nodes, edges); err != nil {
			return err
		}
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
	return nil
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
			line := 1 + bytes.Count(content[:m[0]], []byte{'\n'})
			emit(r, Hit{Rule: r.ID, File: rel, Line: line, Identifier: ident}, nodes, edges)
		}
	}
}

// emit turns one site into its port node and provides/consumes edge. Every
// lane funnels through here — the AST matcher and the raw escape hatch emit
// byte-identical facts because they share this one function.
func emit(r *Rule, s Hit, nodes map[string]schema.Node, edges map[edgeKey]schema.Edge) {
	ident := s.Identifier
	if ident == "" {
		return
	}
	tier := r.Tier
	if tier == "" {
		tier = schema.Extracted
	}
	meta := map[string]string{}
	// A computed identifier is an admission, not a fact: the graph reports it
	// AMBIGUOUS and keeps the template shape. The matcher marks a non-literal
	// argument dynamic; a literal that still carries a template marker is
	// caught here.
	if s.Dynamic || strings.Contains(ident, "${") || strings.Contains(ident, "%s") || strings.Contains(ident, "%d") {
		tier = schema.Ambiguous
		meta["resolved"] = "dynamic"
	}
	raw := ident
	ident = Normalize(r.Transport, ident)
	if ident == "" {
		return
	}
	sig := ">"
	if r.Direction == "provides" {
		sig = "<"
	}
	id := "port:" + r.Transport + ":" + sig + ident
	if _, ok := nodes[id]; !ok {
		nm := map[string]string{
			"direction": r.Direction, "transport": r.Transport, "identifier": ident,
		}
		if raw != ident {
			nm["raw"] = raw // the pre-fold spelling stays recoverable
		}
		for k, v := range meta {
			nm[k] = v
		}
		for k, v := range r.Metadata {
			// $identifier keeps the SOURCE spelling — otel.http.route is the
			// framework's template, not our join key.
			nm[k] = strings.ReplaceAll(v, "$identifier", raw)
		}
		if r.flag != nil && r.flag.MatchString(raw) {
			for k, v := range r.Flag.Set {
				nm[k] = v
			}
		}
		nodes[id] = schema.Node{
			ID: id, Label: raw, Kind: "port", FileType: "boundary",
			Source: "port://" + r.Transport + "/" + ident, Metadata: nm,
		}
	}
	k := edgeKey{s.File, id}
	if e, exists := edges[k]; exists {
		// Keep the strongest tier seen for this file→port pair.
		if e.Confidence == schema.Ambiguous && tier != schema.Ambiguous {
			e.Confidence = tier
			edges[k] = e
		}
		return
	}
	edges[k] = schema.Edge{
		Source: s.File, Target: id, Relation: r.Direction, Confidence: tier,
		Metadata: map[string]string{
			"synthesized_by": "boundaries", "rule": r.ID,
			"site": fmt.Sprintf("%s:L%d", s.File, s.Line),
		},
	}
}

// Assemble turns AST sites (found during the code extractor's walk) into the
// boundary batch: port nodes, provides/consumes edges, the services join and
// scope-by-join. Sites are sorted first so the first-writer-wins node spelling
// is a function of the source, not of worker scheduling.
func Assemble(root string, exclude []string, rules []Rule, sites []Hit) (*schema.Batch, error) {
	batch := &schema.Batch{Producer: Producer}
	byID := make(map[string]*Rule, len(rules))
	for i := range rules {
		byID[rules[i].ID] = &rules[i]
	}
	nodes := map[string]schema.Node{}
	edges := map[edgeKey]schema.Edge{}
	SortHits(sites)
	services, serr := LoadServices(root)
	if serr != nil {
		return nil, serr
	}
	for _, s := range sites {
		if s.SvcID != "" {
			emitSDK(services, s, nodes, edges)
			continue
		}
		if r := byID[s.Rule]; r != nil {
			emit(r, s, nodes, edges)
		}
	}
	if err := finish(root, exclude, true, batch, nodes, edges); err != nil {
		return nil, err
	}
	return batch, nil
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
