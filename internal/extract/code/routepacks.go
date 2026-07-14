// routepacks.go — drop-in ROUTE PACKS, the declarative counterpart of the
// core recognizers (routes.go / frontend_routes.go), mirroring grammar packs:
// core = embedded shape-recognizers for the big frameworks; packs = anything
// call-shaped a JSON rule can express. A pack is one <name>.json discovered at
// add time from two locations (repo wins on name collision):
//
//  1. <store-root>/routes (default ~/ctxoptimize/routes; CTX_OPTIMIZE_STORE
//     relocates it, which keeps tests hermetic) — machine-wide
//  2. <repo>/.ctxoptimize/routes — travels with the repo, committable
//
// Pack shape:
//
//	{"name": "myframework",
//	 "rules": [{"call": "registerRoute", "path_arg": 0, "handler_arg": 1,
//	            "method_arg": -1, "method": "GET"}]}
//
// A rule matches any call expression whose callee's LAST identifier equals
// `call` (registerRoute and api.registerRoute both match) in ANY language
// whose grammar maps call nodes — all embedded languages and every grammar
// pack that declares "calls". Argument positions count the argument list's
// named children (positional; python keyword arguments occupy positions too).
// Literal-or-silent: path_arg must be a quoted string literal (f-strings,
// template strings, raw/backtick strings never match). method_arg wins when
// it holds a literal, else the fixed method; neither → the method-less ROUTE
// token. handler_arg must be a bare identifier to earn a handles edge —
// anything else emits the route node alone. Channel: route-pack:<name>.
// Malformed packs fail LOUDLY at add time — a silently skipped pack reads as
// "covered" when it isn't (grammar-pack precedent).
package code

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/store"
)

// RouteRule is one declarative call-shaped route recognizer.
type RouteRule struct {
	Call       string `json:"call"`
	PathArg    *int   `json:"path_arg"`
	HandlerArg *int   `json:"handler_arg,omitempty"`
	MethodArg  *int   `json:"method_arg,omitempty"`
	Method     string `json:"method,omitempty"`
}

// RoutePack is one discovered pack; File records where it was loaded from.
type RoutePack struct {
	Name  string      `json:"name"`
	Rules []RouteRule `json:"rules"`
	File  string      `json:"-"`
}

// ParseRoutePack decodes and validates one pack. origin names the source in
// errors (a file path or URL) so a bad pack is a one-step fix.
func ParseRoutePack(data []byte, origin string) (*RoutePack, error) {
	var p RoutePack
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("route pack %s: %w", origin, err)
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, fmt.Errorf("route pack %s: name is required", origin)
	}
	if len(p.Rules) == 0 {
		return nil, fmt.Errorf("route pack %s: at least one rule is required", origin)
	}
	for i, r := range p.Rules {
		if strings.TrimSpace(r.Call) == "" {
			return nil, fmt.Errorf("route pack %s: rules[%d]: call is required", origin, i)
		}
		if r.PathArg == nil || *r.PathArg < 0 {
			return nil, fmt.Errorf("route pack %s: rules[%d] (%s): path_arg is required and must be >= 0", origin, i, r.Call)
		}
	}
	p.File = origin
	return &p, nil
}

// MachineRoutesDir is where machine-wide route packs live: <store-root>/routes.
func MachineRoutesDir() (string, error) {
	root, err := store.Root("")
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "routes"), nil
}

// RepoRoutesDir is the repo-level pack dir (committable).
func RepoRoutesDir(repo string) string {
	return filepath.Join(repo, ".ctxoptimize", "routes")
}

// LoadRoutePacks discovers route packs for a repo. Search order: machine dir
// first, repo dir second — later wins on name collision, so repo packs beat
// machine packs (same precedence semantics as grammar packs). Malformed packs
// fail loudly.
func LoadRoutePacks(repo string) ([]RoutePack, error) {
	machine, err := MachineRoutesDir()
	if err != nil {
		return nil, err
	}
	byName := map[string]RoutePack{}
	var order []string
	for _, dir := range []string{machine, RepoRoutesDir(repo)} {
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
			pack, err := ParseRoutePack(data, p)
			if err != nil {
				return nil, err
			}
			if _, seen := byName[pack.Name]; !seen {
				order = append(order, pack.Name)
			}
			byName[pack.Name] = *pack
		}
	}
	packs := make([]RoutePack, 0, len(order))
	for _, n := range order {
		packs = append(packs, byName[n])
	}
	return packs, nil
}

// packRule is one compiled rule with its channel baked in.
type packRule struct {
	rule    RouteRule
	channel string
}

// compileRoutePacks indexes rules by callee name for the visit's O(1) probe.
func compileRoutePacks(packs []RoutePack) map[string][]packRule {
	if len(packs) == 0 {
		return nil
	}
	idx := map[string][]packRule{}
	for _, p := range packs {
		for _, r := range p.Rules {
			idx[r.Call] = append(idx[r.Call], packRule{rule: r, channel: "route-pack:" + p.Name})
		}
	}
	return idx
}

// bareIdentRe: what earns a handles edge from handler_arg — a single plain
// identifier, resolved module-wide like every call edge.
var bareIdentRe = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// anyStringLit unquotes a string literal by TEXT, language-agnostically:
// the argument's source must start and end with a matching plain quote.
// f-strings (f"…"), raw/backtick strings, template literals, and anything
// computed fail the check — literal-or-silent.
func anyStringLit(txt string) (string, bool) {
	if len(txt) < 2 {
		return "", false
	}
	q := txt[0]
	if (q != '"' && q != '\'') || txt[len(txt)-1] != q {
		return "", false
	}
	inner := txt[1 : len(txt)-1]
	if strings.ContainsAny(inner, "\"'\n") {
		return "", false // concatenations / embedded quotes — not one plain literal
	}
	return inner, true
}

// packRouteSites applies the pack rules registered for callee to the call
// expression at index i.
func packRouteSites(raw []RawNode, i int, typeOf, text func(RawNode) string, rel, lang string, rules []packRule) []routeSite {
	// The argument container is the first direct named child whose type
	// mentions "argument" ("arguments", "argument_list") — true across all
	// embedded grammars.
	argsIdx := -1
	d := raw[i].Depth
	for j := i + 1; j < len(raw) && raw[j].Depth > d; j++ {
		if raw[j].Depth == d+1 && raw[j].Named && strings.Contains(typeOf(raw[j]), "argument") {
			argsIdx = j
			break
		}
	}
	if argsIdx < 0 {
		return nil
	}
	args := namedChildren(raw, argsIdx, typeOf)
	var out []routeSite
	for _, pr := range rules {
		pa := *pr.rule.PathArg
		if pa >= len(args) {
			continue
		}
		path, ok := anyStringLit(text(raw[args[pa]]))
		if !ok {
			continue
		}
		method := ""
		methodArgSet := pr.rule.MethodArg != nil && *pr.rule.MethodArg >= 0
		if methodArgSet && *pr.rule.MethodArg < len(args) {
			if m, ok := anyStringLit(text(raw[args[*pr.rule.MethodArg]])); ok {
				method = strings.ToUpper(m)
			}
		}
		if method == "" {
			method = strings.ToUpper(strings.TrimSpace(pr.rule.Method))
		}
		if method == "" {
			if methodArgSet {
				continue // method_arg demanded a literal, none found, no fallback
			}
			method = "ROUTE" // method-less custom route
		}
		site := routeSite{
			node:    makeRouteNode(rel, lang, pr.channel, method, path, raw[i].StartRow, raw[i].EndRow),
			channel: pr.channel, file: rel,
		}
		if ha := pr.rule.HandlerArg; ha != nil && *ha >= 0 && *ha < len(args) {
			if nm := text(raw[args[*ha]]); bareIdentRe.MatchString(nm) {
				site.handlerName = nm
			}
		}
		out = append(out, site)
	}
	return out
}
