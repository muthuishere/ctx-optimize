// boundarysites.go — the boundary lane's matcher over the AST (ADR
// 2026-08-14-boundaries-on-the-ast). The rules are DATA (internal/boundaries);
// this file is the engine that probes them against the node stream the code
// extractor already produced. No second walk, no second read, no second parse:
// a rule costs one map lookup at a node we were visiting anyway.
//
// The ONE non-obvious rule, and it is the whole quality argument: an argument
// that is not a string literal is not a miss. `os.Getenv(name)` is a certain
// env read with an uncertain value, so it emits with the expression wrapped as
// ${name} — which `emit` recognizes and downgrades to AMBIGUOUS with
// `resolved: dynamic`. A regex could only see text, so it saw nothing there;
// that is why process-py measured 0.00 and reported "this repo spawns
// nothing", which was a lie.
package code

import (
	"bytes"
	"strings"
	"sync"

	"github.com/muthuishere/ctx-optimize/internal/boundaries"
)

// bndCtx is the per-file matcher state. Nil when no AST rule wants this file,
// which keeps the hook free for repos the rules do not cover.
type bndCtx struct {
	ix     *boundaries.Index
	rel    string
	ext    string
	hits   *[]boundaries.Hit
	raw    []RawNode
	src    []byte
	class  []uint8 // symbol id → shape class; see shapeClasses
	mask   uint8   // which shapes any rule wants for THIS file's extension
	typeOf func(RawNode) string
	text   func(RawNode) string
}

// Shape classes. The dispatch runs on EVERY node of EVERY file, so it must be
// a slice index, not a string comparison: `strings.Contains(t, "string")` per
// node cost more than the whole lane on a TypeScript tree.
const (
	clsNone uint8 = iota
	clsString
	clsMember
	clsSubscript
	clsAnnotation
	clsNew
)

// shapeClasses builds the symbol→class table for one language. Computed once
// per language (symbol ids are stable within a grammar), not per file.
func shapeClasses(syms []string) []uint8 {
	out := make([]uint8, len(syms))
	for i, t := range syms {
		switch {
		case isStringLiteralType(t):
			out[i] = clsString
		case isMemberType(t):
			out[i] = clsMember
		case isSubscriptType(t):
			out[i] = clsSubscript
		case isAnnotationType(t):
			out[i] = clsAnnotation
		case isNewType(t):
			out[i] = clsNew
		}
	}
	return out
}

// classAt is the per-node dispatch: one bounds check, one slice read, and a
// mask test so a Go file never probes a TypeScript-only member rule.
func (c *bndCtx) classAt(i int) uint8 {
	s := int(c.raw[i].Symbol)
	if s >= len(c.class) {
		return clsNone
	}
	cl := c.class[s]
	if cl == clsNone {
		return clsNone
	}
	var bit uint8
	switch cl {
	case clsString:
		bit = boundaries.MaskLiteral
	case clsMember:
		bit = boundaries.MaskMember
	case clsSubscript:
		bit = boundaries.MaskSubscript
	case clsAnnotation:
		bit = boundaries.MaskAnnotation
	case clsNew:
		bit = boundaries.MaskNew
	}
	if c.mask&bit == 0 {
		return clsNone
	}
	return cl
}

// key returns the node's source text for a MAP LOOKUP without allocating —
// Go optimizes m[string(b)] into a no-copy probe. Never keep the result.
func (c *bndCtx) key(i int) []byte {
	n := c.raw[i]
	if n.Start < n.End && int(n.End) <= len(c.src) {
		return c.src[n.Start:n.End]
	}
	return nil
}

// maxDynExpr caps the expression text kept for a dynamic identifier. A whole
// nested call as a port label helps nobody; the site is what matters.
const maxDynExpr = 48

// litText unquotes a string-literal node by TEXT, language-agnostically —
// same discipline as routepacks' anyStringLit, plus backtick templates. A
// template carrying ${…} is returned as dynamic rather than dropped.
func litText(txt string) (val string, dynamic bool, ok bool) {
	if len(txt) < 2 {
		return "", false, false
	}
	q := txt[0]
	if q != '"' && q != '\'' && q != '`' {
		return "", false, false // f-strings, raw/byte prefixes, identifiers
	}
	if txt[len(txt)-1] != q {
		return "", false, false
	}
	inner := txt[1 : len(txt)-1]
	if strings.ContainsAny(inner, "\n") {
		return "", false, false
	}
	if strings.Contains(inner, "${") {
		return inner, true, true // template — keep the shape, flag it
	}
	if q != '`' && strings.ContainsAny(inner, "\"'") {
		return "", false, false // concatenation / embedded quotes
	}
	return inner, false, true
}

// dynExpr wraps a non-literal expression so it reads as what it is.
func dynExpr(txt string) string {
	txt = strings.Join(strings.Fields(txt), " ")
	if txt == "" {
		return ""
	}
	if len(txt) > maxDynExpr {
		txt = txt[:maxDynExpr] + "…"
	}
	return "${" + txt + "}"
}

// argContainer finds the argument list of the node at i: the first direct
// named child whose type mentions "argument" — true across every embedded
// grammar (arguments, argument_list, annotation_argument_list,
// attribute_argument_list).
func (c *bndCtx) argContainer(i int) int {
	d := c.raw[i].Depth
	for j := i + 1; j < len(c.raw) && c.raw[j].Depth > d; j++ {
		if c.raw[j].Depth == d+1 && c.raw[j].Named && strings.Contains(c.typeOf(c.raw[j]), "argument") {
			return j
		}
	}
	return -1
}

// unwrap descends one level through argument wrappers (C#'s
// attribute_argument, Java's element_value_pair) so the literal beneath is
// reachable without teaching the schema about each grammar's spelling.
func (c *bndCtx) unwrap(idx int) int {
	if _, _, ok := litText(c.text(c.raw[idx])); ok {
		return idx
	}
	kids := namedChildren(c.raw, idx, c.typeOf)
	if len(kids) == 1 {
		return kids[0]
	}
	if len(kids) == 2 && strings.Contains(c.typeOf(c.raw[idx]), "pair") {
		return kids[1] // name=value: the value
	}
	return idx
}

// namedArgValue finds a `name = value` argument by name.
func (c *bndCtx) namedArgValue(args []int, want string) (int, bool) {
	for _, a := range args {
		if !strings.Contains(c.typeOf(c.raw[a]), "pair") {
			continue
		}
		kids := namedChildren(c.raw, a, c.typeOf)
		if len(kids) == 2 && c.text(c.raw[kids[0]]) == want {
			return kids[1], true
		}
	}
	return 0, false
}

// identFromArgs resolves a match's identifier out of an argument list.
// Returns ok=false only when the argument is absent — a present-but-computed
// argument returns dynamic, never nothing.
func (c *bndCtx) identFromArgs(m *boundaries.ASTMatch, args []int) (string, bool, bool) {
	idx := -1
	for _, n := range m.NamedArg {
		if v, ok := c.namedArgValue(args, n); ok {
			idx = v
			break
		}
	}
	if idx < 0 {
		pos := 0
		if m.Arg != nil {
			pos = *m.Arg
		}
		if pos >= len(args) {
			return "", false, false
		}
		idx = c.unwrap(args[pos])
	} else {
		idx = c.unwrap(idx)
	}
	if m.ArgInList {
		// subprocess.run(["git", "log"]) names git; run(argv) does not.
		if _, _, ok := litText(c.text(c.raw[idx])); !ok {
			kids := namedChildren(c.raw, idx, c.typeOf)
			if len(kids) == 0 {
				return dynExpr(c.text(c.raw[idx])), true, true
			}
			idx = c.unwrap(kids[0])
		}
	}
	txt := c.text(c.raw[idx])
	if v, dyn, ok := litText(txt); ok {
		return v, dyn, true
	}
	return dynExpr(txt), true, true
}

// add records a hit after the rule's own file gates and identifier gate.
func (c *bndCtx) add(b boundaries.Bound, ident string, dynamic bool, row uint32) {
	if ident == "" || !b.Match.IdentifierOK(ident) {
		return
	}
	if !boundaries.RuleAllowsFile(b.Rule, c.rel, c.ext) {
		return
	}
	*c.hits = append(*c.hits, boundaries.Hit{
		Rule: b.Rule.ID, File: c.rel, Line: int(row) + 1,
		Identifier: ident, Dynamic: dynamic,
	})
}

// parentTypeIs reports whether the immediate parent of raw[i] has type typ.
// The stream is preorder, so the first strictly-shallower node walking
// backward is the parent.
func (c *bndCtx) parentTypeIs(i int, typ string) bool {
	d := c.raw[i].Depth
	for j := i - 1; j >= 0; j-- {
		if c.raw[j].Depth < d {
			return c.typeOf(c.raw[j]) == typ
		}
	}
	return false
}

// probeSDK handles service SDK call sites: the dependency IS the boundary, so
// `chat.completions.create(...)` names api.openai.com with no URL present.
// Keyed on the callee name the extractor already resolved, so a file with no
// SDK call pays one map probe per call node.
func (c *bndCtx) probeSDK(i int, callee string) {
	if len(c.ix.SDK) == 0 || len(c.ix.SDK[callee]) == 0 {
		return
	}
	// Only now is the callee expression worth materializing.
	expr := ""
	d := c.raw[i].Depth
	for j := i + 1; j < len(c.raw) && c.raw[j].Depth > d; j++ {
		if c.raw[j].Depth == d+1 && c.raw[j].Named && !strings.Contains(c.typeOf(c.raw[j]), "argument") {
			expr = c.text(c.raw[j])
			break
		}
	}
	if boundaries.SDKExcluded(c.rel) {
		return
	}
	for _, b := range c.ix.SDKMatches(callee, expr) {
		*c.hits = append(*c.hits, boundaries.Hit{
			File: c.rel, Line: int(c.raw[i].StartRow) + 1,
			SvcID: b.SvcID, Sym: b.Sym,
		})
	}
}

// probeCall handles the call and annotation-as-call shapes.
func (c *bndCtx) probeCall(i int, recv, callee string) {
	c.probeSDK(i, callee)
	bs := c.ix.Call[callee]
	if len(bs) == 0 {
		return
	}
	argsIdx := c.argContainer(i)
	var args []int
	if argsIdx >= 0 {
		args = namedChildren(c.raw, argsIdx, c.typeOf)
	}
	for _, b := range bs {
		if !b.Match.ReceiverOK(recv) {
			continue
		}
		if b.Match.InDecorator && !c.inDecorator(i) {
			continue
		}
		ident, dyn, ok := c.identFromArgs(b.Match, args)
		if !ok {
			continue
		}
		c.add(b, ident, dyn, c.raw[i].StartRow)
	}
}

// inDecorator reports whether the call sits in decorator position. Python
// wraps it in a `decorator` node; that is the only grammar in the embedded
// set that needs this, and a route rule must not fire on a runtime call.
func (c *bndCtx) inDecorator(i int) bool {
	return c.parentTypeIs(i, "decorator")
}

// probeAnnotation handles Java annotations and C# attributes: the name is the
// first named child, the arguments hang off the *_argument_list sibling.
func (c *bndCtx) probeAnnotation(i int) {
	kids := namedChildren(c.raw, i, c.typeOf)
	if len(kids) == 0 {
		return
	}
	bs := c.ix.Annotation[string(c.key(kids[0]))]
	if len(bs) == 0 {
		return
	}
	argsIdx := c.argContainer(i)
	var args []int
	if argsIdx >= 0 {
		args = namedChildren(c.raw, argsIdx, c.typeOf)
	}
	for _, b := range bs {
		ident, dyn, ok := c.identFromArgs(b.Match, args)
		if !ok {
			continue
		}
		c.add(b, ident, dyn, c.raw[i].StartRow)
	}
}

// probeNew handles `new WebSocket(url)`.
func (c *bndCtx) probeNew(i int) {
	kids := namedChildren(c.raw, i, c.typeOf)
	if len(kids) == 0 {
		return
	}
	bs := c.ix.New[string(c.key(kids[0]))]
	if len(bs) == 0 {
		return
	}
	argsIdx := c.argContainer(i)
	var args []int
	if argsIdx >= 0 {
		args = namedChildren(c.raw, argsIdx, c.typeOf)
	}
	for _, b := range bs {
		ident, dyn, ok := c.identFromArgs(b.Match, args)
		if !ok {
			continue
		}
		c.add(b, ident, dyn, c.raw[i].StartRow)
	}
}

// probeMember handles `process.env.FOO`: the object's source text is the
// path, the trailing property is the identifier.
func (c *bndCtx) probeMember(i int) {
	kids := namedChildren(c.raw, i, c.typeOf)
	if len(kids) != 2 {
		return
	}
	bs := c.ix.Member[string(c.key(kids[0]))]
	if len(bs) == 0 {
		return
	}
	prop := c.text(c.raw[kids[1]])
	for _, b := range bs {
		c.add(b, prop, false, c.raw[i].StartRow)
	}
}

// probeSubscript handles `os.environ["X"]` and `process.env[expr]`.
func (c *bndCtx) probeSubscript(i int) {
	kids := namedChildren(c.raw, i, c.typeOf)
	if len(kids) < 2 {
		return
	}
	bs := c.ix.Subscript[string(c.key(kids[0]))]
	if len(bs) == 0 {
		return
	}
	key := c.unwrap(kids[1])
	txt := c.text(c.raw[key])
	ident, dyn, ok := litText(txt)
	if !ok {
		ident, dyn = dynExpr(txt), true
	}
	for _, b := range bs {
		c.add(b, ident, dyn, c.raw[i].StartRow)
	}
}

// probeLiteral handles bare URL literals — the one shape with no surrounding
// structure to key on.
func (c *bndCtx) probeLiteral(i int) {
	if len(c.ix.Literal) == 0 {
		return
	}
	// Cheap reject first: a URL literal must contain "://" somewhere. Most
	// string literals in a codebase are not URLs, and this skips them without
	// materializing the text.
	if !bytes.Contains(c.key(i), []byte("://")) {
		return
	}
	val, dyn, ok := litText(c.text(c.raw[i]))
	if !ok {
		return
	}
	for _, b := range c.ix.Literal {
		host, hok := b.Match.URLHost(val)
		if !hok {
			continue
		}
		c.add(b, host, dyn, c.raw[i].StartRow)
	}
}

// isStringLiteralType reports whether a node type names a whole string
// literal (not its pieces: string_content, string_fragment, string_start).
func isStringLiteralType(t string) bool {
	if !strings.Contains(t, "string") {
		return false
	}
	return t == "string" || strings.HasSuffix(t, "string_literal") || t == "template_string"
}

// isMemberType / isSubscriptType name the access shapes across the embedded
// grammars: TS member_expression / subscript_expression, Python attribute /
// subscript.
func isMemberType(t string) bool {
	return t == "member_expression" || t == "attribute" || t == "field_expression" ||
		t == "selector_expression"
}

func isSubscriptType(t string) bool {
	return t == "subscript_expression" || t == "subscript" || t == "index_expression" ||
		t == "element_access_expression"
}

func isAnnotationType(t string) bool {
	return t == "annotation" || t == "marker_annotation" || t == "attribute"
}

func isNewType(t string) bool {
	return t == "new_expression" || t == "object_creation_expression"
}

// classCache memoizes the symbol→class table per language id. Grammars are
// fixed for the process, so this is computed at most once per language.
var classCache sync.Map // langID -> []uint8

func shapeClassesFor(langID int, syms []string) []uint8 {
	if v, ok := classCache.Load(langID); ok {
		return v.([]uint8)
	}
	t := shapeClasses(syms)
	classCache.Store(langID, t)
	return t
}

// BoundaryHits returns the boundary sites a gather of root would emit. It is
// the HitSource `boundaries verify` injects: an AST rule cannot be re-run as a
// regex, so verify measures it against the sites it actually produced.
func BoundaryHits(root string) ([]boundaries.Hit, error) {
	var out []boundaries.Hit
	if err := extractHits(root, &out); err != nil {
		return nil, err
	}
	return out, nil
}
