// gomod.go — go.mod recognizer (line-based, both `require x v1` and the
// block form). dep:go/<module-path> with the version verbatim; `// indirect`
// becomes scope "indirect", everything else "require". replace/exclude
// directives are not dependency declarations — skipped.
package manifests

import "strings"

func extractGoMod(c *collector, rel, content string) {
	inRequire := false
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case inRequire && line == ")":
			inRequire = false
			continue
		case strings.HasPrefix(line, "require ("):
			inRequire = true
			continue
		}
		var entry string
		if inRequire {
			entry = line
		} else if strings.HasPrefix(line, "require ") {
			entry = strings.TrimSpace(strings.TrimPrefix(line, "require"))
		} else {
			continue
		}
		if entry == "" || strings.HasPrefix(entry, "//") {
			continue
		}
		scope := "require"
		if i := strings.Index(entry, "//"); i >= 0 {
			if strings.Contains(entry[i:], "indirect") {
				scope = "indirect"
			}
			entry = strings.TrimSpace(entry[:i])
		}
		fields := strings.Fields(entry)
		if len(fields) != 2 || !strings.HasPrefix(fields[1], "v") {
			continue // literal-or-silent: a require line is `module vX.Y.Z`
		}
		id := c.depNode("go", fields[0])
		c.declares(rel, id, fields[1], scope)
	}
}
