// frontend_routes.go — frontend router recognition (Angular, React Router,
// Vue Router), riding the SAME preorder visit as routes.go. Frontend routes
// have no HTTP method: the method token is ROUTE (label `ROUTE /admin/users`),
// keeping the id shape `<file>::route:<METHOD> <path>` uniform.
//
// The deterministic contract holds: paths must be string literals, handler
// components must be bare identifiers (or JSX tags) — anything dynamic is
// skipped silently, and a non-literal path skips the object AND its children
// (a composed path can never be guessed).
//
// What each channel matches (documented boundaries, not type-checked truth):
//
//	angular-route      js/ts  object literals with a literal path: inside
//	                   RouterModule.forRoot([...]) / .forChild([...]) /
//	                   provideRouter([...]). The array may also be a bare
//	                   identifier resolved to a unique top-level
//	                   `const <name> = [...]` in the same file. component:
//	                   identifier → handles edge; loadChildren → route node
//	                   without an edge (lazy); nested children: compose.
//	react-router-route tsx/jsx JSX <Route path="/x" element={<Foo/>}/> (also
//	                   Component={Foo}), nested <Route> children compose; and
//	                   createBrowserRouter/createHashRouter/createMemoryRouter
//	                   object literals (same object shape as angular).
//	vue-router-route   js/ts  createRouter({routes: [...]}) — same object
//	                   shape; routes may be an identifier (or {routes}
//	                   shorthand) resolved like the angular identifier case.
//
// A route object (or JSX Route) with a literal path but NO handler-ish key
// (component/Component/element/loadChildren) is a grouping node: it composes
// paths for its children but emits nothing itself.
package code

import "strings"

// frontendRouterRoutes recognizes Angular/React-data/Vue router registration
// calls at the call_expression index i.
func frontendRouterRoutes(raw []RawNode, i int, typeOf, text func(RawNode) string, rel, lang string) []routeSite {
	args := childAt(raw, i, "arguments", typeOf)
	if args < 0 {
		return nil
	}
	kids := namedChildren(raw, args, typeOf)
	if len(kids) == 0 {
		return nil
	}
	// Member callee: RouterModule.forRoot / RouterModule.forChild only.
	if mi := childAt(raw, i, "member_expression", typeOf); mi >= 0 && raw[mi].Start == raw[i].Start {
		mk := namedChildren(raw, mi, typeOf)
		if len(mk) != 2 || typeOf(raw[mk[0]]) != "identifier" || typeOf(raw[mk[1]]) != "property_identifier" {
			return nil
		}
		if text(raw[mk[0]]) != "RouterModule" {
			return nil
		}
		if p := text(raw[mk[1]]); p != "forRoot" && p != "forChild" {
			return nil
		}
		return routeArrayArg(raw, kids[0], typeOf, text, rel, lang, "angular-route")
	}
	id := childAt(raw, i, "identifier", typeOf)
	if id < 0 {
		return nil
	}
	switch text(raw[id]) {
	case "provideRouter":
		return routeArrayArg(raw, kids[0], typeOf, text, rel, lang, "angular-route")
	case "createBrowserRouter", "createHashRouter", "createMemoryRouter":
		return routeArrayArg(raw, kids[0], typeOf, text, rel, lang, "react-router-route")
	case "createRouter":
		// Vue: first argument is an options object carrying routes:.
		if typeOf(raw[kids[0]]) != "object" {
			return nil
		}
		for _, pi := range namedChildren(raw, kids[0], typeOf) {
			switch typeOf(raw[pi]) {
			case "pair":
				pk := namedChildren(raw, pi, typeOf)
				if len(pk) < 2 || typeOf(raw[pk[0]]) != "property_identifier" || text(raw[pk[0]]) != "routes" {
					continue
				}
				return routeArrayArg(raw, pk[1], typeOf, text, rel, lang, "vue-router-route")
			case "shorthand_property_identifier":
				if text(raw[pi]) == "routes" {
					return routeArrayArg(raw, pi, typeOf, text, rel, lang, "vue-router-route")
				}
			}
		}
	}
	return nil
}

// routeArrayArg dispatches a router call's routes argument: an array literal
// walks directly; a bare identifier resolves to a unique top-level
// `const <name> = [...]` in the same file (anything else: silent).
func routeArrayArg(raw []RawNode, ai int, typeOf, text func(RawNode) string, rel, lang, channel string) []routeSite {
	switch typeOf(raw[ai]) {
	case "array":
		return walkRouteObjects(raw, ai, typeOf, text, rel, lang, channel, "")
	case "identifier", "shorthand_property_identifier":
		if arr := topLevelArrayNamed(raw, text(raw[ai]), typeOf, text); arr >= 0 {
			return walkRouteObjects(raw, arr, typeOf, text, rel, lang, channel, "")
		}
	}
	return nil
}

// topLevelArrayNamed finds the UNIQUE variable_declarator in the file whose
// name matches and whose value is an array literal; -1 when absent or
// ambiguous (never guessed).
func topLevelArrayNamed(raw []RawNode, name string, typeOf, text func(RawNode) string) int {
	found := -1
	for i := range raw {
		if !raw[i].Named || typeOf(raw[i]) != "variable_declarator" {
			continue
		}
		id := childAt(raw, i, "identifier", typeOf)
		if id < 0 || text(raw[id]) != name {
			continue
		}
		arr := childAt(raw, i, "array", typeOf)
		if arr < 0 {
			continue
		}
		if found >= 0 {
			return -1 // two declarations of the same name — ambiguous
		}
		found = arr
	}
	return found
}

// walkRouteObjects emits route sites for the object literals directly inside
// the array at ai, recursing into children: arrays with the composed path.
func walkRouteObjects(raw []RawNode, ai int, typeOf, text func(RawNode) string, rel, lang, channel, base string) []routeSite {
	var out []routeSite
	for _, oi := range namedChildren(raw, ai, typeOf) {
		if typeOf(raw[oi]) != "object" {
			continue
		}
		var path, compName string
		pathSeen, pathLit, handlerish := false, false, false
		childrenIdx := -1
		for _, pi := range namedChildren(raw, oi, typeOf) {
			if typeOf(raw[pi]) != "pair" {
				continue
			}
			pk := namedChildren(raw, pi, typeOf)
			if len(pk) < 2 || typeOf(raw[pk[0]]) != "property_identifier" {
				continue
			}
			val := pk[1]
			switch text(raw[pk[0]]) {
			case "path":
				pathSeen = true
				if typeOf(raw[val]) == "string" {
					if s, ok := jsStringLit(text(raw[val])); ok {
						path, pathLit = s, true
					}
				}
			case "component", "Component":
				handlerish = true
				if typeOf(raw[val]) == "identifier" {
					compName = text(raw[val])
				}
			case "element":
				handlerish = true
				if tag, ok := jsxTagName(raw, val, typeOf, text); ok {
					compName = tag
				}
			case "loadChildren":
				handlerish = true // lazy route: node yes, edge no
			case "children":
				if typeOf(raw[val]) == "array" {
					childrenIdx = val
				}
			}
		}
		if !pathSeen || !pathLit {
			continue // absent or non-literal path: skip the object AND children
		}
		full := joinRoutePath(base, path)
		if handlerish {
			site := routeSite{
				node:    makeRouteNode(rel, lang, channel, "ROUTE", full, raw[oi].StartRow, raw[oi].EndRow),
				channel: channel, file: rel, handlerName: compName,
			}
			out = append(out, site)
		}
		if childrenIdx >= 0 {
			out = append(out, walkRouteObjects(raw, childrenIdx, typeOf, text, rel, lang, channel, full)...)
		}
	}
	return out
}

// jsxTagName extracts the component name of a JSX element node
// (<Foo/> or <Foo>…</Foo>). Member tags (<Foo.Bar/>) return false.
func jsxTagName(raw []RawNode, i int, typeOf, text func(RawNode) string) (string, bool) {
	switch typeOf(raw[i]) {
	case "jsx_self_closing_element":
		if id := childAt(raw, i, "identifier", typeOf); id >= 0 {
			return text(raw[id]), true
		}
	case "jsx_element":
		if op := childAt(raw, i, "jsx_opening_element", typeOf); op >= 0 {
			if id := childAt(raw, op, "identifier", typeOf); id >= 0 {
				return text(raw[id]), true
			}
		}
	}
	return "", false
}

// jsxRoutePath reads the path attribute of the opening/self-closing element
// at op. has = a path attribute exists; lit = its value is a string literal.
func jsxRoutePath(raw []RawNode, op int, typeOf, text func(RawNode) string) (path string, lit, has bool) {
	for _, ai := range namedChildren(raw, op, typeOf) {
		if typeOf(raw[ai]) != "jsx_attribute" {
			continue
		}
		ak := namedChildren(raw, ai, typeOf)
		if len(ak) == 0 || typeOf(raw[ak[0]]) != "property_identifier" || text(raw[ak[0]]) != "path" {
			continue
		}
		has = true
		if len(ak) >= 2 && typeOf(raw[ak[1]]) == "string" {
			if s, ok := jsStringLit(text(raw[ak[1]])); ok {
				return s, true, true
			}
		}
		return "", false, true // path={expr} — present but not a literal
	}
	return "", false, false
}

// jsxRouteSite recognizes <Route path="…" element={<Foo/>}/> (or
// Component={Foo}) at the jsx_element / jsx_self_closing_element index i,
// composing the path with enclosing <Route> ancestors. A non-literal path on
// the element or ANY Route ancestor poisons the site (skip, never guess);
// a path-less ancestor (layout route) contributes nothing.
func jsxRouteSite(raw []RawNode, i int, typeOf, text func(RawNode) string, rel, lang string) (routeSite, bool) {
	op := i
	if typeOf(raw[i]) == "jsx_element" {
		if op = childAt(raw, i, "jsx_opening_element", typeOf); op < 0 {
			return routeSite{}, false
		}
	}
	tag := childAt(raw, op, "identifier", typeOf)
	if tag < 0 || text(raw[tag]) != "Route" {
		return routeSite{}, false
	}
	path, lit, has := jsxRoutePath(raw, op, typeOf, text)
	if !has || !lit {
		return routeSite{}, false
	}
	compName, handlerish := "", false
	for _, ai := range namedChildren(raw, op, typeOf) {
		if typeOf(raw[ai]) != "jsx_attribute" {
			continue
		}
		ak := namedChildren(raw, ai, typeOf)
		if len(ak) < 2 || typeOf(raw[ak[0]]) != "property_identifier" {
			continue
		}
		key := text(raw[ak[0]])
		if key != "element" && key != "Component" {
			continue
		}
		handlerish = true
		if typeOf(raw[ak[1]]) != "jsx_expression" {
			continue
		}
		ek := namedChildren(raw, ak[1], typeOf)
		if len(ek) == 0 {
			continue
		}
		if typeOf(raw[ek[0]]) == "identifier" {
			compName = text(raw[ek[0]])
		} else if tagName, ok := jsxTagName(raw, ek[0], typeOf, text); ok {
			compName = tagName
		}
	}
	if !handlerish {
		return routeSite{}, false // grouping <Route>: children compose via ancestors
	}
	full := path
	for j := i - 1; j >= 0; j-- {
		if !raw[j].Named || typeOf(raw[j]) != "jsx_element" {
			continue
		}
		if raw[j].Start > raw[i].Start || raw[j].End < raw[i].End {
			continue // not an ancestor
		}
		aop := childAt(raw, j, "jsx_opening_element", typeOf)
		if aop < 0 {
			continue
		}
		atag := childAt(raw, aop, "identifier", typeOf)
		if atag < 0 || text(raw[atag]) != "Route" {
			continue
		}
		p, plit, phas := jsxRoutePath(raw, aop, typeOf, text)
		if phas && !plit {
			return routeSite{}, false // dynamic ancestor path poisons the branch
		}
		if phas {
			full = joinRoutePath(p, full)
		}
	}
	if !strings.HasPrefix(full, "/") {
		full = "/" + full
	}
	return routeSite{
		node:    makeRouteNode(rel, lang, "react-router-route", "ROUTE", full, raw[i].StartRow, raw[i].EndRow),
		channel: "react-router-route", file: rel, handlerName: compName,
	}, true
}
