// ast.go — the boundary lane's matcher, as DATA over the AST (ADR
// 2026-08-14-boundaries-on-the-ast). A rule names a node SHAPE, not a line of
// text, and it is evaluated inside the code extractor's existing walk: the
// file is already read and already parsed, so a rule costs a map probe rather
// than another pass over the bytes.
//
// Six shapes, chosen by enumerating what the shipped rules actually need —
// not by inventing a pattern language:
//
//	call        os.Getenv("X")          {"shape":"call","name":"Getenv","receiver":"os","arg":0}
//	member      process.env.X           {"shape":"member","path":["process","env"]}
//	subscript   os.environ["X"]         {"shape":"subscript","path":["os","environ"]}
//	annotation  @GetMapping("/x")       {"shape":"annotation","name":"GetMapping","arg":0}
//	new         new WebSocket(url)      {"shape":"new","name":"WebSocket","arg":0}
//	literal     "https://api.example"   {"shape":"literal","url_scheme":["http","https"],"take":"host"}
//
// The honesty rule and the recall fix are ONE mechanism: an argument that is
// not a string literal does not vanish, it emits with `resolved: dynamic` at
// AMBIGUOUS. `os.Getenv(name)` is a certain env read with an uncertain value,
// and that is exactly what the graph then says.
package boundaries

import (
	"fmt"
	"sort"
	"strings"
)

// Shape names. A rule declares exactly one.
const (
	ShapeCall       = "call"
	ShapeMember     = "member"
	ShapeSubscript  = "subscript"
	ShapeAnnotation = "annotation"
	ShapeNew        = "new"
	ShapeLiteral    = "literal"
)

// ASTMatch is one declarative node-shape pattern. Fields are per-shape; the
// loader rejects a field that the shape cannot use, so a typo fails at add
// time instead of silently matching nothing.
type ASTMatch struct {
	Shape string `json:"shape"`

	// call / annotation / new: the name to match. For `call` this is the
	// callee's own name (`Getenv` in `os.Getenv`) — never the receiver.
	Name string `json:"name,omitempty"`

	// call: constrain the receiver. routepacks matches the callee's LAST
	// identifier only, so a rule for os.Getenv also fires on a bare
	// Getenv(); the AST has the receiver, so we can be stricter.
	Receiver       string   `json:"receiver,omitempty"`
	ReceiverAnyOf  []string `json:"receiver_any_of,omitempty"`
	ReceiverSuffix string   `json:"receiver_suffix,omitempty"`

	// member / subscript: the object chain, outermost last —
	// ["process","env"] matches process.env.X and process.env["X"].
	Path []string `json:"path,omitempty"`

	// call / annotation / new: which named argument carries the identifier.
	Arg *int `json:"arg,omitempty"`
	// ArgInList reads the FIRST element of a list/array argument:
	// subprocess.run(["git","log"]) names git.
	ArgInList bool `json:"arg_in_list,omitempty"`
	// NamedArg accepts `name=value` argument forms, in order of preference —
	// @RequestMapping(path="/k") needs it and a regex never got it.
	NamedArg []string `json:"named_arg,omitempty"`

	// literal: which schemes count, and what to take from the URL.
	URLScheme []string `json:"url_scheme,omitempty"`
	Take      string   `json:"take,omitempty"` // "host"

	// InDecorator restricts a call match to decorator position: @app.get("/x")
	// is a route, app.get("/x") at runtime is a client call.
	InDecorator bool `json:"in_decorator,omitempty"`
	// IdentifierPrefix rejects identifiers that do not start with it — routes
	// require "/" so a method name is never mistaken for a path.
	IdentifierPrefix string `json:"identifier_prefix,omitempty"`
}

// Hit is one boundary hit found on the AST. It carries the RAW identifier;
// normalization, the sensitive flag and metadata interpolation happen at
// assembly, so the matcher stays a pure finder.
type Hit struct {
	Rule       string
	File       string
	Line       int
	Identifier string
	Dynamic    bool
	// SvcID/Sym are set instead of Rule when the hit is an SDK call site.
	SvcID string
	Sym   string
}

// validateAST checks one match against its shape. Fail-closed: a field the
// shape cannot use is an error, because a silently ignored constraint reads
// as "covered" when it is not.
func validateAST(m *ASTMatch) error {
	needsName := map[string]bool{ShapeCall: true, ShapeAnnotation: true, ShapeNew: true}
	needsPath := map[string]bool{ShapeMember: true, ShapeSubscript: true}
	switch m.Shape {
	case ShapeCall, ShapeAnnotation, ShapeNew, ShapeMember, ShapeSubscript, ShapeLiteral:
	default:
		return fmt.Errorf("shape %q is not call|member|subscript|annotation|new|literal", m.Shape)
	}
	if needsName[m.Shape] && strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("shape %s requires name", m.Shape)
	}
	if !needsName[m.Shape] && m.Name != "" {
		return fmt.Errorf("shape %s does not take name", m.Shape)
	}
	if needsPath[m.Shape] && len(m.Path) == 0 {
		return fmt.Errorf("shape %s requires path", m.Shape)
	}
	if !needsPath[m.Shape] && len(m.Path) > 0 {
		return fmt.Errorf("shape %s does not take path", m.Shape)
	}
	if m.Shape != ShapeCall && (m.Receiver != "" || len(m.ReceiverAnyOf) > 0 || m.ReceiverSuffix != "") {
		return fmt.Errorf("shape %s does not take a receiver constraint", m.Shape)
	}
	if m.Shape == ShapeLiteral {
		if len(m.URLScheme) == 0 {
			return fmt.Errorf("shape literal requires url_scheme")
		}
		if m.Take != "host" {
			return fmt.Errorf("shape literal: take must be \"host\", got %q", m.Take)
		}
		if m.Arg != nil {
			return fmt.Errorf("shape literal does not take arg")
		}
	}
	if m.Shape == ShapeMember || m.Shape == ShapeSubscript {
		if m.Arg != nil {
			return fmt.Errorf("shape %s does not take arg", m.Shape)
		}
	}
	if m.Arg != nil && *m.Arg < 0 {
		return fmt.Errorf("arg must be >= 0")
	}
	return nil
}

// ReceiverOK reports whether recv satisfies the match's receiver constraint.
// No constraint accepts anything — routes-go matches .HandleFunc on any
// receiver by design.
func (m *ASTMatch) ReceiverOK(recv string) bool {
	if m.Receiver != "" {
		return recv == m.Receiver
	}
	if len(m.ReceiverAnyOf) > 0 || m.ReceiverSuffix != "" {
		for _, r := range m.ReceiverAnyOf {
			if recv == r {
				return true
			}
		}
		return m.ReceiverSuffix != "" && strings.HasSuffix(recv, m.ReceiverSuffix)
	}
	return true
}

// IdentifierOK applies the prefix gate.
func (m *ASTMatch) IdentifierOK(id string) bool {
	return m.IdentifierPrefix == "" || strings.HasPrefix(id, m.IdentifierPrefix)
}

// URLHost parses a URL literal WITHOUT regex and without net/url (whose
// permissiveness is the wrong tool here): scheme must be declared, the host
// is what precedes the first '/', ':', '?' or '#'. A host that is empty or
// carries a template marker is reported dynamic by the caller.
func (m *ASTMatch) URLHost(lit string) (string, bool) {
	rest := ""
	for _, s := range m.URLScheme {
		p := s + "://"
		if len(lit) > len(p) && strings.EqualFold(lit[:len(p)], p) {
			rest = lit[len(p):]
			break
		}
	}
	if rest == "" {
		return "", false
	}
	// RFC 3986 userinfo: everything through the LAST '@' before the path is
	// credentials, not the host. Without this, `https://user:pw@host/x` reported
	// `user` as the host — a FABRICATED name, which is worse than a miss and is
	// exactly what the confidence tiers exist to prevent — and
	// `https://user@host/x` dropped the real host entirely. The password is
	// never captured either way (the ':' terminator below stops before it), so
	// this was not a leak; it was a lie. Last '@', because a userinfo field may
	// legally contain a percent-encoded one.
	if at := strings.LastIndexByte(rest[:hostEnd(rest)], '@'); at >= 0 {
		rest = rest[at+1:]
	}
	end := len(rest)
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '/', ':', '?', '#', '"', '\'', '`', ' ':
			end = i
			i = len(rest)
		}
	}
	host := rest[:end]
	if host == "" {
		return "", false
	}
	// A host must look like a host: letters/digits to start, then the DNS
	// alphabet. Anything else (a template head, a format verb) is not a
	// hostname and is dropped rather than guessed.
	c := host[0]
	if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
		return "", false
	}
	for i := 0; i < len(host); i++ {
		c := host[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-'
		if !ok {
			return "", false
		}
	}
	return host, true
}

// Index groups the merged rules by the key each shape probes, so the walk
// pays one map lookup per candidate node instead of one scan per rule.
type Index struct {
	Call       map[string][]Bound // key: callee name
	Member     map[string][]Bound // key: the object chain, dotted ("process.env")
	Subscript  map[string][]Bound // key: the object chain, dotted ("os.environ")
	Annotation map[string][]Bound // key: annotation name
	New        map[string][]Bound // key: constructed type
	Literal    []Bound
	// SDK indexes service call symbols (chat.completions.create) by their
	// LAST dotted component, so a call node costs one probe on the callee
	// name the extractor already resolved. Same technique as routepacks.
	SDK map[string][]SDKBound
	// Exts is the union of extensions any AST rule declares; "" means a rule
	// applies to every language.
	Exts map[string]bool
	All  bool
	// Mask records, per extension, WHICH shapes any rule wants. Without it a
	// Go tree probes the member index once per selector_expression — every
	// `foo.Bar` in the repo — for rules that only exist for TypeScript.
	Mask map[string]uint8
	// AllMask is the mask contributed by rules that declare no extension.
	AllMask uint8
}

// Shape bits for Index.Mask.
const (
	MaskCall uint8 = 1 << iota
	MaskMember
	MaskSubscript
	MaskAnnotation
	MaskNew
	MaskLiteral
)

// MaskFor returns the shapes worth probing in a file with this extension.
func (ix *Index) MaskFor(ext string) uint8 {
	if ix == nil {
		return 0
	}
	return ix.AllMask | ix.Mask[strings.ToLower(ext)]
}

// Bound pairs a match with the rule that owns it.
type Bound struct {
	Rule  *Rule
	Match *ASTMatch
}

// SDKBound is one service SDK symbol: the dependency IS the boundary, so a
// call to `chat.completions.create` names api.openai.com with no URL in
// sight (ADR 2026-08-13 D5).
type SDKBound struct {
	SvcID string
	Sym   string // the full dotted symbol, e.g. chat.completions.create
	Last  string // its last component — the index key
}

// Empty reports an index with nothing to probe — the caller then skips the
// boundary hook entirely.
func (ix *Index) Empty() bool {
	return ix == nil || (len(ix.Call) == 0 && len(ix.Member) == 0 && len(ix.Subscript) == 0 &&
		len(ix.Annotation) == 0 && len(ix.New) == 0 && len(ix.Literal) == 0 && len(ix.SDK) == 0)
}

// Applies reports whether any AST rule wants this file extension.
func (ix *Index) Applies(ext string) bool {
	if ix == nil {
		return false
	}
	return ix.All || ix.Exts[strings.ToLower(ext)]
}

// BuildIndex compiles rules into the probe index. Rules are visited in sorted
// id order (Load already guarantees it) so the per-key slices are stable.
func BuildIndex(rules []Rule) *Index {
	ix := &Index{
		Call: map[string][]Bound{}, Member: map[string][]Bound{},
		Subscript: map[string][]Bound{}, Annotation: map[string][]Bound{},
		New: map[string][]Bound{}, SDK: map[string][]SDKBound{},
		Exts: map[string]bool{}, Mask: map[string]uint8{},
	}
	for i := range rules {
		r := &rules[i]
		if len(r.AST) == 0 {
			continue
		}
		if len(r.When.Ext) == 0 {
			ix.All = true
		}
		for _, e := range r.When.Ext {
			ix.Exts[strings.ToLower(e)] = true
		}
		for j := range r.AST {
			m := &r.AST[j]
			b := Bound{Rule: r, Match: m}
			var bit uint8
			switch m.Shape {
			case ShapeCall:
				bit = MaskCall
			case ShapeMember:
				bit = MaskMember
			case ShapeSubscript:
				bit = MaskSubscript
			case ShapeAnnotation:
				bit = MaskAnnotation
			case ShapeNew:
				bit = MaskNew
			case ShapeLiteral:
				bit = MaskLiteral
			}
			if len(r.When.Ext) == 0 {
				ix.AllMask |= bit
			}
			for _, e := range r.When.Ext {
				ix.Mask[strings.ToLower(e)] |= bit
			}
			switch m.Shape {
			case ShapeCall:
				ix.Call[m.Name] = append(ix.Call[m.Name], b)
			case ShapeMember:
				k := strings.Join(m.Path, ".")
				ix.Member[k] = append(ix.Member[k], b)
			case ShapeSubscript:
				k := strings.Join(m.Path, ".")
				ix.Subscript[k] = append(ix.Subscript[k], b)
			case ShapeAnnotation:
				ix.Annotation[m.Name] = append(ix.Annotation[m.Name], b)
			case ShapeNew:
				ix.New[m.Name] = append(ix.New[m.Name], b)
			case ShapeLiteral:
				ix.Literal = append(ix.Literal, b)
			}
		}
	}
	return ix
}

// RuleAllowsFile applies the rule's own ext + exclude gates to one file. The
// index bounds the walk coarsely; this is the per-rule decision.
func RuleAllowsFile(r *Rule, rel, ext string) bool {
	if len(r.When.Ext) > 0 {
		ok := false
		for _, e := range r.When.Ext {
			if strings.EqualFold(e, ext) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return !excludedPath(rel, r.Exclude.Path)
}

// SortHits orders sites deterministically. Extraction runs on parallel
// workers, so arrival order varies; node creation is first-writer-wins, which
// would make the winning spelling depend on scheduling. Sorting first makes
// the output a function of the source alone.
func SortHits(s []Hit) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].File != s[j].File {
			return s[i].File < s[j].File
		}
		if s[i].Line != s[j].Line {
			return s[i].Line < s[j].Line
		}
		if s[i].Rule != s[j].Rule {
			return s[i].Rule < s[j].Rule
		}
		return s[i].Identifier < s[j].Identifier
	})
}

// IndexServices folds the service registry's SDK symbols into the index and
// widens the extension bound to the languages those SDKs live in. Called
// after BuildIndex so one probe covers rules and services alike.
func (ix *Index) IndexServices(services map[string]Service) {
	ids := make([]string, 0, len(services))
	for id := range services {
		ids = append(ids, id)
	}
	sort.Strings(ids) // deterministic slices per key
	for _, id := range ids {
		for _, ep := range services[id].Endpoints {
			for _, sym := range ep.SDK {
				last := sym
				if i := strings.LastIndexByte(sym, '.'); i >= 0 {
					last = sym[i+1:]
				}
				ix.SDK[last] = append(ix.SDK[last], SDKBound{SvcID: id, Sym: sym, Last: last})
			}
		}
	}
	if len(ix.SDK) > 0 {
		for e := range SDKSourceExts() {
			ix.Exts[e] = true
			ix.Mask[e] |= MaskCall
		}
	}
}

// SDKMatches reports the service symbols satisfied by a call whose callee is
// `last` and whose full callee expression is `expr`. A dotted symbol must
// match the tail of the expression on a component boundary, so
// `client.chat.completions.create` matches `chat.completions.create` and a
// bare `create()` does not.
func (ix *Index) SDKMatches(last, expr string) []SDKBound {
	cands := ix.SDK[last]
	if len(cands) == 0 {
		return nil
	}
	var out []SDKBound
	for _, c := range cands {
		if c.Sym == last {
			out = append(out, c)
			continue
		}
		if strings.HasSuffix(expr, c.Sym) &&
			(len(expr) == len(c.Sym) || expr[len(expr)-len(c.Sym)-1] == '.') {
			out = append(out, c)
		}
	}
	return out
}

// hostEnd returns where the authority component ends — the first delimiter that
// can only appear after it. Used to bound the userinfo search so an '@' in a
// query string or fragment is never mistaken for credentials.
func hostEnd(rest string) int {
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '/', '?', '#', '"', '\'', '`', ' ':
			return i
		}
	}
	return len(rest)
}
