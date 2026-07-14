// routes.go — framework-aware ROUTE recognition (Phase 1 of the
// framework-routes spec): FastAPI, Flask, Express, NestJS. Recognition rides
// the SAME preorder visit as declarations/calls — no second parse, no extra
// full-tree walk; each recognizer does a bounded local scan around the node
// the main loop is already standing on.
//
// The deterministic contract holds: a route is emitted only when both the
// HTTP method and the path are string literals in the source. Anything
// dynamic (f-strings, variables, template strings, computed members) is
// skipped silently — never guessed.
//
// What each channel matches (documented boundaries, not type-checked truth):
//
//	fastapi-route  python  @X.get|post|put|delete|patch("<lit>") — any
//	               attribute receiver (app, router, blueprint, self.api…).
//	               Flask 2.x verb shortcuts (@app.get) match here too.
//	flask-route    python  @X.route("<lit>"[, methods=["GET",…]]) — methods
//	               must be all string literals; absent methods = GET.
//	express-route  js/ts   app|router|*Router . get|post|put|delete|patch
//	               ("<lit>", …args) with ≥1 arg after the path. Handler =
//	               LAST argument: an identifier resolves module-wide like
//	               call edges (ambiguous dropped); an inline function has no
//	               decl node, so the route node stands without a handles edge.
//	nestjs-route   ts/tsx  @Controller("<base>"?) class + @Get|Post|Put|
//	               Delete|Patch("<sub>"?) method; paths compose. A verb
//	               decorator outside a literal @Controller class is ignored.
package code

import (
	"fmt"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// routeSite is one recognized route, pending handler resolution in Extract.
type routeSite struct {
	node        schema.Node
	handlerID   string // direct link: the decorated function / method decl
	handlerName string // deferred: express identifier handler, resolved like calls
	channel     string // synthesized_by value, stable per framework
	file        string
}

var httpVerbs = map[string]string{
	"get": "GET", "post": "POST", "put": "PUT", "delete": "DELETE", "patch": "PATCH",
}

// nestVerbs: NestJS method decorators are TitleCase identifiers.
var nestVerbs = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE", "Patch": "PATCH",
}

func routeID(rel, method, path string) string {
	return rel + "::route:" + method + " " + path
}

func makeRouteNode(rel, lang, channel, method, path string, startRow, endRow uint32) schema.Node {
	label := method + " " + path
	return schema.Node{
		ID: routeID(rel, method, path), Label: label, Kind: "route",
		FileType: "code", Source: rel,
		Location: fmt.Sprintf("L%d-L%d", startRow+1, endRow+1),
		Metadata: map[string]string{"lang": lang, "synthesized_by": channel},
	}
}

// decoratorIndices returns the decorator records attached to the decl at i,
// in source order. Decorators are preceding SIBLINGS at the decl's depth
// (python decorated_definition, TS export_statement / class_body members) or
// leading CHILDREN before the name (non-exported TS class).
func decoratorIndices(raw []RawNode, i int, typeOf func(RawNode) string) []int {
	d := raw[i].Depth
	var back []int
	for j := i - 1; j >= 0; j-- {
		if raw[j].Depth > d || !raw[j].Named {
			continue // inside a sibling's subtree / punctuation
		}
		if raw[j].Depth < d {
			break // left the parent
		}
		if typeOf(raw[j]) != "decorator" {
			break // a real previous sibling ends the chain
		}
		back = append([]int{j}, back...)
	}
	out := back
	for j := i + 1; j < len(raw) && raw[j].Depth > d; j++ {
		if raw[j].Depth != d+1 || !raw[j].Named {
			continue
		}
		if typeOf(raw[j]) != "decorator" {
			break // decorator children only lead the subtree
		}
		out = append(out, j)
	}
	return out
}

// childAt finds the first named child of raw[i] (direct children only) with
// the given type; -1 if absent.
func childAt(raw []RawNode, i int, typ string, typeOf func(RawNode) string) int {
	d := raw[i].Depth
	for j := i + 1; j < len(raw) && raw[j].Depth > d; j++ {
		if raw[j].Depth == d+1 && raw[j].Named && typeOf(raw[j]) == typ {
			return j
		}
	}
	return -1
}

// namedChildren lists direct named children of raw[i].
func namedChildren(raw []RawNode, i int, _ func(RawNode) string) []int {
	d := raw[i].Depth
	var out []int
	for j := i + 1; j < len(raw) && raw[j].Depth > d; j++ {
		if raw[j].Depth == d+1 && raw[j].Named {
			out = append(out, j)
		}
	}
	return out
}

// pyStringLit unquotes a python "string" node. false for anything that is not
// a plain quoted literal (f-strings, raw/byte prefixes, concatenations).
func pyStringLit(txt string) (string, bool) {
	if txt == "" || (txt[0] != '"' && txt[0] != '\'') {
		return "", false
	}
	return strings.Trim(txt, `"'`), true
}

// jsStringLit unquotes a js/ts "string" node (template strings are a
// different node type and never reach here).
func jsStringLit(txt string) (string, bool) {
	if txt == "" || (txt[0] != '"' && txt[0] != '\'') {
		return "", false
	}
	return strings.Trim(txt, `"'`), true
}

// pyDecoratorRoutes recognizes FastAPI / Flask route decorators on the
// function_definition at index i and returns finished route sites pointing at
// handlerID. Called from the main visit only for python decls.
func pyDecoratorRoutes(raw []RawNode, i int, typeOf, text func(RawNode) string, rel, lang, handlerID string) []routeSite {
	var out []routeSite
	endRow := raw[i].EndRow
	for _, di := range decoratorIndices(raw, i, typeOf) {
		ci := childAt(raw, di, "call", typeOf)
		if ci < 0 {
			continue // bare decorator (@staticmethod)
		}
		ai := childAt(raw, ci, "attribute", typeOf)
		if ai < 0 {
			continue // @cached(...) — identifier callee, not X.verb
		}
		attr := text(raw[ai])
		name := attr[strings.LastIndex(attr, ".")+1:]
		args := childAt(raw, ci, "argument_list", typeOf)
		if args < 0 {
			continue
		}
		kids := namedChildren(raw, args, typeOf)
		if verb, ok := httpVerbs[name]; ok {
			// FastAPI-style: @X.get("/path", ...)
			if len(kids) == 0 || typeOf(raw[kids[0]]) != "string" {
				continue
			}
			path, ok := pyStringLit(text(raw[kids[0]]))
			if !ok {
				continue
			}
			out = append(out, routeSite{
				node:      makeRouteNode(rel, lang, "fastapi-route", verb, path, raw[di].StartRow, endRow),
				handlerID: handlerID, channel: "fastapi-route", file: rel,
			})
			continue
		}
		if name != "route" {
			continue
		}
		// Flask-style: @X.route("/path", methods=["GET","POST"])
		if len(kids) == 0 || typeOf(raw[kids[0]]) != "string" {
			continue
		}
		path, ok := pyStringLit(text(raw[kids[0]]))
		if !ok {
			continue
		}
		methods := []string{"GET"}
		literal := true
		for _, k := range kids[1:] {
			if typeOf(raw[k]) != "keyword_argument" {
				continue
			}
			kk := namedChildren(raw, k, typeOf)
			if len(kk) < 2 || typeOf(raw[kk[0]]) != "identifier" || text(raw[kk[0]]) != "methods" {
				continue
			}
			if typeOf(raw[kk[1]]) != "list" {
				literal = false
				break
			}
			methods = methods[:0]
			for _, m := range namedChildren(raw, kk[1], typeOf) {
				s, ok := "", false
				if typeOf(raw[m]) == "string" {
					s, ok = pyStringLit(text(raw[m]))
				}
				if !ok {
					literal = false
					break
				}
				methods = append(methods, strings.ToUpper(s))
			}
		}
		if !literal || len(methods) == 0 {
			continue // a non-literal method list poisons the whole decorator
		}
		for _, m := range methods {
			out = append(out, routeSite{
				node:      makeRouteNode(rel, lang, "flask-route", m, path, raw[di].StartRow, endRow),
				handlerID: handlerID, channel: "flask-route", file: rel,
			})
		}
	}
	return out
}

// nestDecoratorCall decodes one TS decorator: @Name("arg") → (Name, arg,
// argIsLiteral). A decorator without a call or with a non-identifier callee
// returns ok=false. hasArg distinguishes @Get() from @Get(nonLiteral).
func nestDecoratorCall(raw []RawNode, di int, typeOf, text func(RawNode) string) (name, arg string, hasArg, argLiteral, ok bool) {
	ci := childAt(raw, di, "call_expression", typeOf)
	if ci < 0 {
		return "", "", false, false, false // bare @Injectable
	}
	id := childAt(raw, ci, "identifier", typeOf)
	if id < 0 {
		return "", "", false, false, false // @nest.Get(...) — member callee, skip
	}
	name = text(raw[id])
	args := childAt(raw, ci, "arguments", typeOf)
	if args < 0 {
		return name, "", false, false, true
	}
	kids := namedChildren(raw, args, typeOf)
	if len(kids) == 0 {
		return name, "", false, false, true
	}
	hasArg = true
	if typeOf(raw[kids[0]]) == "string" {
		if s, lit := jsStringLit(text(raw[kids[0]])); lit {
			return name, s, true, true, true
		}
	}
	return name, "", true, false, true
}

// nestControllerBase inspects the decorators of the class decl at i. Returns
// (base, true) when a literal (or bare) @Controller is present; false when
// there is no usable controller — including @Controller(nonLiteral), which
// silently disables the whole class per the deterministic contract.
func nestControllerBase(raw []RawNode, i int, typeOf, text func(RawNode) string) (string, bool) {
	for _, di := range decoratorIndices(raw, i, typeOf) {
		name, arg, hasArg, lit, ok := nestDecoratorCall(raw, di, typeOf, text)
		if !ok || name != "Controller" {
			continue
		}
		if hasArg && !lit {
			return "", false
		}
		return arg, true
	}
	return "", false
}

// nestMethodRoutes recognizes @Get/@Post/... on the method_definition at i,
// composing paths with the enclosing controller's base.
func nestMethodRoutes(raw []RawNode, i int, typeOf, text func(RawNode) string, rel, lang, base, handlerID string) []routeSite {
	var out []routeSite
	endRow := raw[i].EndRow
	for _, di := range decoratorIndices(raw, i, typeOf) {
		name, arg, hasArg, lit, ok := nestDecoratorCall(raw, di, typeOf, text)
		if !ok {
			continue
		}
		verb, isVerb := nestVerbs[name]
		if !isVerb {
			continue
		}
		if hasArg && !lit {
			continue // @Get(SOME_CONST) — not a literal, skip silently
		}
		path := joinRoutePath(base, arg)
		out = append(out, routeSite{
			node:      makeRouteNode(rel, lang, "nestjs-route", verb, path, raw[di].StartRow, endRow),
			handlerID: handlerID, channel: "nestjs-route", file: rel,
		})
	}
	return out
}

// joinRoutePath composes NestJS controller base + method sub-path with a
// single leading slash: ("users", ":id") → "/users/:id"; ("", "") → "/".
func joinRoutePath(base, sub string) string {
	b := strings.Trim(base, "/")
	s := strings.Trim(sub, "/")
	switch {
	case b == "" && s == "":
		return "/"
	case b == "":
		return "/" + s
	case s == "":
		return "/" + b
	}
	return "/" + b + "/" + s
}

// expressRoute recognizes app.get('/path', …) / router.post(…) at the
// call_expression index i. Receiver must be a bare identifier named app,
// router, or *Router; the first argument must be a string literal; at least
// one more argument must follow (this is what separates a route from a
// Map.get lookalike). The handler is the LAST argument: identifier →
// deferred module-wide resolution; inline function → no decl node exists, so
// the route stands without a handles edge; anything else → no edge.
func expressRoute(raw []RawNode, i int, typeOf, text func(RawNode) string, rel, lang string) (routeSite, bool) {
	mi := childAt(raw, i, "member_expression", typeOf)
	if mi < 0 || raw[mi].Start != raw[i].Start {
		return routeSite{}, false // callee is not a member expression
	}
	mk := namedChildren(raw, mi, typeOf)
	if len(mk) != 2 || typeOf(raw[mk[0]]) != "identifier" || typeOf(raw[mk[1]]) != "property_identifier" {
		return routeSite{}, false // this.app.get / computed member — skip
	}
	recv, prop := text(raw[mk[0]]), text(raw[mk[1]])
	if recv != "app" && recv != "router" && !strings.HasSuffix(recv, "Router") {
		return routeSite{}, false
	}
	verb, ok := httpVerbs[prop]
	if !ok {
		return routeSite{}, false
	}
	args := childAt(raw, i, "arguments", typeOf)
	if args < 0 {
		return routeSite{}, false
	}
	kids := namedChildren(raw, args, typeOf)
	if len(kids) < 2 || typeOf(raw[kids[0]]) != "string" {
		return routeSite{}, false
	}
	path, lit := jsStringLit(text(raw[kids[0]]))
	if !lit {
		return routeSite{}, false
	}
	site := routeSite{
		node:    makeRouteNode(rel, lang, "express-route", verb, path, raw[i].StartRow, raw[i].EndRow),
		channel: "express-route", file: rel,
	}
	if last := kids[len(kids)-1]; typeOf(raw[last]) == "identifier" {
		site.handlerName = text(raw[last])
	}
	return site, true
}
