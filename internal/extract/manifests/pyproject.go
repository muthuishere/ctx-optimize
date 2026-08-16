// pyproject.go — pyproject.toml recognizer (ADR 2026-07-25-structured-formats
// S7). TABLE-ANCHORED by construction: a dependency is harvested only from a
// table that declares dependencies, never from an array that happens to hold
// version-looking strings. Flask's `[tool.tox.env.tests-min] commands =
// [["uv","pip","install","blinker==1.9.0",…]]` is the proof case — the naive
// shape heuristic invents ~12 phantom deps there; this lane sees 31, the hand
// count, with 0 extras.
//
// Lanes, all exact table paths:
//
//	[project]                            dependencies        → scope dependencies
//	[project.optional-dependencies]      <extra> = [...]      → optional-dependencies:<extra>
//	[dependency-groups]                  <group> = [...]      → dependency-groups:<group>  (PEP 735)
//	[build-system]                       requires            → build-system
//	[tool.uv]                            dev-dependencies    → dev-dependencies
//	[tool.poetry.dependencies]           <name> = spec       → dependencies
//	[tool.poetry.dev-dependencies]       <name> = spec       → dev-dependencies
//	[tool.poetry.group.<g>.dependencies] <name> = spec       → group:<g>
//
// plus the sub-table form `[tool.poetry.dependencies.<name>]` and the inline
// dependency-TABLE form `dependencies = { a = "^1" }` under any poetry dep
// table. Poetry's `python` pseudo-dependency is not a package — skipped.
package manifests

import (
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/tomlwalk"
)

const pypiNS = "pypi"

func extractPyproject(c *collector, rel, content string) {
	entries := tomlwalk.Parse(content)
	for _, e := range entries {
		if e.Key == "" {
			continue // table header: handled by the sub-table pass below
		}
		path := e.Path()
		switch {
		// what this distribution IS (ADR 22 D0): [project] name, and poetry's
		// [tool.poetry] name. Table-anchored, so a `name` inside a dependency
		// table cannot be mistaken for the package's own.
		case (path == "project" || path == "tool.poetry") && e.Key == "name":
			c.publishes(pypiNS, tomlwalk.Unquote(e.Val), rel)
		// PEP 621 / PEP 735 / build-system: arrays of PEP 508 requirements.
		case path == "project" && e.Key == "dependencies":
			addPypiRequirements(c, rel, e.Val, "dependencies")
		case path == "project.optional-dependencies":
			addPypiRequirements(c, rel, e.Val, "optional-dependencies:"+e.Key)
		case path == "dependency-groups":
			addPypiRequirements(c, rel, e.Val, "dependency-groups:"+e.Key)
		case path == "build-system" && e.Key == "requires":
			addPypiRequirements(c, rel, e.Val, "build-system")
		case path == "tool.uv" && e.Key == "dev-dependencies":
			addPypiRequirements(c, rel, e.Val, "dev-dependencies")

		// Poetry: name = spec tables, plus the inline-table form written as one
		// value on the parent table.
		case path == "tool.poetry" && (e.Key == "dependencies" || e.Key == "dev-dependencies"):
			addPoetryInline(c, rel, e.Val, e.Key)
		case path == "tool.poetry.dependencies":
			addPoetryDep(c, rel, e.Key, e.Val, "dependencies")
		case path == "tool.poetry.dev-dependencies":
			addPoetryDep(c, rel, e.Key, e.Val, "dev-dependencies")
		default:
			if g, ok := poetryGroup(e.Table); ok {
				addPoetryDep(c, rel, e.Key, e.Val, "group:"+g)
			}
		}
	}
	// Sub-table form: `[tool.poetry.dependencies.requests]` + `version = "^2"`.
	// Keyed on the table one segment deeper than a dep table; the version comes
	// from that table's own fields.
	for _, e := range entries {
		if e.Key != "" || len(e.Table) == 0 {
			continue
		}
		parent, name := e.Table[:len(e.Table)-1], e.Table[len(e.Table)-1]
		scope := ""
		switch p := strings.Join(parent, "."); {
		case p == "tool.poetry.dependencies":
			scope = "dependencies"
		case p == "tool.poetry.dev-dependencies":
			scope = "dev-dependencies"
		default:
			if g, ok := poetryGroup(parent); ok {
				scope = "group:" + g
			}
		}
		if scope == "" {
			continue
		}
		addPypiDep(c, rel, name, tableVersion(entries, e.Table), scope)
	}
}

// poetryGroup matches `tool.poetry.group.<g>.dependencies` (the table a
// grouped poetry dep lives in) and returns <g>.
func poetryGroup(table []string) (string, bool) {
	if len(table) != 5 {
		return "", false
	}
	if table[0] != "tool" || table[1] != "poetry" || table[2] != "group" || table[4] != "dependencies" {
		return "", false
	}
	return table[3], true
}

// addPypiRequirements harvests an array of PEP 508 requirement strings — or,
// per S7 requirement 6, an inline dependency TABLE written in the same slot.
func addPypiRequirements(c *collector, rel, val, scope string) {
	if tomlwalk.IsInlineTable(val) {
		addPoetryInline(c, rel, val, scope)
		return
	}
	for _, req := range tomlwalk.Strings(val) {
		name, spec := pep508(req)
		addPypiDep(c, rel, name, spec, scope)
	}
}

// addPoetryInline treats each field of `{ a = "^1", b = { version = "^2" } }`
// as one declaration.
func addPoetryInline(c *collector, rel, val, scope string) {
	for _, f := range tomlwalk.InlineFields(val) {
		addPoetryDep(c, rel, f[0], f[1], scope)
	}
}

// addPoetryDep is the poetry/Cargo shape: the KEY is the package name, the
// value is a version string or an inline table.
func addPoetryDep(c *collector, rel, name, val, scope string) {
	if strings.EqualFold(name, "python") {
		return // poetry's interpreter constraint is not a package
	}
	addPypiDep(c, rel, name, depVersion(val), scope)
}

// addPypiDep emits the node+edge after PEP 503 normalization; an unusable name
// is silently dropped (absent beats wrong).
func addPypiDep(c *collector, rel, name, spec, scope string) {
	n := normalizePEP503(name)
	if n == "" {
		return
	}
	c.declares(rel, c.depNode(pypiNS, n), spec, scope)
}

// depVersion reads the version out of a bare string or an inline table,
// falling back to path/git/workspace so the version_spec metadata still says
// something true for a local or VCS dependency.
func depVersion(val string) string {
	if !tomlwalk.IsInlineTable(val) {
		return tomlwalk.Unquote(val)
	}
	if v := tomlwalk.Field(val, "version"); v != "" {
		return tomlwalk.Unquote(v)
	}
	for _, k := range []string{"path", "git", "url"} {
		if v := tomlwalk.Field(val, k); v != "" {
			return k + ":" + tomlwalk.Unquote(v)
		}
	}
	if tomlwalk.Field(val, "workspace") != "" {
		return "workspace"
	}
	return ""
}

// tableVersion is depVersion for the sub-table form, reading the fields of the
// dependency's own table.
func tableVersion(entries []tomlwalk.Entry, table []string) string {
	want := strings.Join(table, ".")
	fields := map[string]string{}
	for _, e := range entries {
		if e.Key != "" && e.Path() == want {
			fields[e.Key] = tomlwalk.Unquote(e.Val)
		}
	}
	if v := fields["version"]; v != "" {
		return v
	}
	for _, k := range []string{"path", "git", "url"} {
		if v := fields[k]; v != "" {
			return k + ":" + v
		}
	}
	if fields["workspace"] != "" {
		return "workspace"
	}
	return ""
}

// pep508 splits a PEP 508 requirement into its distribution name and the rest
// (version specifier, direct reference). The environment marker after `;` is
// not part of either. Extras (`pkg[all,fast]`) belong to the name syntax but
// not to the package identity.
func pep508(req string) (name, spec string) {
	s := strings.TrimSpace(req)
	if i := strings.IndexByte(s, ';'); i >= 0 { // env marker
		s = strings.TrimSpace(s[:i])
	}
	end := 0
	for end < len(s) && isNameByte(s[end]) {
		end++
	}
	name = s[:end]
	rest := strings.TrimSpace(s[end:])
	if strings.HasPrefix(rest, "[") { // extras
		if i := strings.IndexByte(rest, ']'); i >= 0 {
			rest = strings.TrimSpace(rest[i+1:])
		}
	}
	// What follows a distribution name can only be a specifier, a direct
	// reference or extras. Anything else means the candidate was never a name:
	// a bare URL (`https://…` → rest `://…`) or a VCS reference
	// (`git+https://…#egg=x` → rest `+https…`) yields NO node rather than the
	// phantom `https` / `git` a version-sniffing heuristic would invent.
	if rest != "" && !strings.ContainsRune("<>=!~(,@*", rune(rest[0])) {
		return "", ""
	}
	return name, rest
}

// isNameByte accepts the characters a PEP 503 distribution name may contain.
func isNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' ||
		b == '-' || b == '_' || b == '.'
}

// normalizePEP503 lowercases and collapses runs of `-`, `_`, `.` to a single
// `-` (PEP 503). Measured on the real corpus: collapses flit_core/poetry_core/
// typing_extensions onto their hyphenated twins, 166 raw names → 165 distinct.
// A candidate that is not a distribution name at all (empty, a URL, a path,
// not starting alphanumeric) yields "" — the caller drops it.
func normalizePEP503(name string) string {
	s := strings.TrimSpace(name)
	if s == "" || strings.ContainsAny(s, "/:\\") {
		return ""
	}
	if s[0] == '-' || s[0] == '_' || s[0] == '.' {
		return "" // a distribution name starts alphanumeric
	}
	var b strings.Builder
	sep := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '-' || ch == '_' || ch == '.':
			sep = true
		case isNameByte(ch):
			if sep && b.Len() > 0 {
				b.WriteByte('-')
			}
			sep = false
			if ch >= 'A' && ch <= 'Z' {
				ch += 'a' - 'A'
			}
			b.WriteByte(ch)
		default:
			return "" // not a distribution name
		}
	}
	out := b.String()
	if out == "" || !(out[0] >= 'a' && out[0] <= 'z' || out[0] >= '0' && out[0] <= '9') {
		return ""
	}
	return out
}
