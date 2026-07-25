// cargo.go — Cargo.toml recognizer (ADR 2026-07-25-structured-formats S7),
// table-anchored on the same walker as pyproject.go. Crate names are used
// verbatim (measured: 181 distinct crate names, PEP-503-style normalization
// changes none of them — that rule is a pypi rule and would be wrong here).
//
// Dependency tables, each `<name> = "1.0"` or `<name> = { version = "1", … }`:
//
//	[dependencies] / [dev-dependencies] / [build-dependencies]
//	[workspace.<same>]                       → scope workspace-<base>
//	[target."cfg(windows)".<same>]           → scope target-<base>
//	[<any of the above>.<name>] sub-table    → version from its own fields
//
// Cargo.lock is a resolution, not a declaration — already refused by
// isLockfile.
package manifests

import (
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/tomlwalk"
)

const cratesNS = "crates"

// cargoSections are the three real dependency section names.
var cargoSections = map[string]bool{
	"dependencies": true, "dev-dependencies": true, "build-dependencies": true,
}

func extractCargo(c *collector, rel, content string) {
	entries := tomlwalk.Parse(content)
	for _, e := range entries {
		if e.Key == "" {
			continue
		}
		if scope, ok := cargoScope(e.Table); ok {
			addCargoDep(c, rel, e.Key, depVersion(e.Val), scope)
			continue
		}
		// The inline dependency-table form: `dependencies = { serde = "1" }`
		// written as one value on the table above (S7 requirement 6).
		if tomlwalk.IsInlineTable(e.Val) && cargoSections[e.Key] {
			if scope, ok := cargoScope(append(append([]string{}, e.Table...), e.Key)); ok {
				for _, f := range tomlwalk.InlineFields(e.Val) {
					addCargoDep(c, rel, f[0], depVersion(f[1]), scope)
				}
			}
		}
	}
	// Sub-table form: `[dependencies.serde]` + `version = "1"`.
	for _, e := range entries {
		if e.Key != "" || len(e.Table) == 0 {
			continue
		}
		parent, name := e.Table[:len(e.Table)-1], e.Table[len(e.Table)-1]
		if scope, ok := cargoScope(parent); ok {
			addCargoDep(c, rel, name, tableVersion(entries, e.Table), scope)
		}
	}
}

// cargoScope reports whether a table path IS a dependency section, and the raw
// scope name to record. `workspace-`/`target-` prefixes keep the base section's
// scope class (see scopeclass.go) while staying honest about where the
// declaration lived.
func cargoScope(table []string) (string, bool) {
	switch len(table) {
	case 1:
		if cargoSections[table[0]] {
			return table[0], true
		}
	case 2:
		if table[0] == "workspace" && cargoSections[table[1]] {
			return "workspace-" + table[1], true
		}
	case 3:
		// [target."cfg(windows)".dependencies] — the middle segment is the cfg
		// expression and is not part of the scope vocabulary.
		if table[0] == "target" && cargoSections[table[2]] {
			return "target-" + table[2], true
		}
	}
	return "", false
}

// addCargoDep drops a name that is not a crate identifier (crates.io allows
// ASCII alphanumerics, `-` and `_`) — absent beats wrong.
func addCargoDep(c *collector, rel, name, spec, scope string) {
	n := strings.TrimSpace(name)
	if n == "" {
		return
	}
	for i := 0; i < len(n); i++ {
		ch := n[i]
		ok := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' ||
			ch >= '0' && ch <= '9' || ch == '-' || ch == '_'
		if !ok {
			return
		}
	}
	c.declares(rel, c.depNode(cratesNS, n), spec, scope)
}
