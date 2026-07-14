// Package yamlwalk is the shared indentation-based YAML line walker used by
// the markdown producer's route lane (openapi/drupal/ingress) and the
// manifests producer's k8s lane. NOT a YAML parser: no yaml library (stdlib
// rule) — a deterministic flattener that is good enough for exactly the
// shapes those lanes read. Anything it can't confidently represent (tabs,
// flow mappings, anchors) is dropped or left as raw text; callers are
// literal-or-silent by contract.
package yamlwalk

import "strings"

// Line is one meaningful YAML line: indentation (a `- ` list marker counts
// as two columns of indent, so a list item's keys align with its siblings),
// an optional key, an unquoted scalar value, and the 1-based line number.
type Line struct {
	Indent int
	Key    string
	Val    string
	List   bool
	Num    int
}

// Parse flattens one YAML document into Lines. Blank lines, comments, and
// tab-indented lines (YAML forbids tabs; we refuse rather than misread) are
// dropped. offset is added to line numbers (multi-doc files pass the doc's
// starting index).
func Parse(lines []string, offset int) []Line {
	var out []Line
	for idx, line := range lines {
		num := offset + idx + 1
		t := strings.TrimRight(line, " \t\r")
		if t == "" {
			continue
		}
		ind := 0
		for ind < len(t) && t[ind] == ' ' {
			ind++
		}
		if ind < len(t) && t[ind] == '\t' {
			continue
		}
		content := t[ind:]
		if strings.HasPrefix(content, "#") || content == "-" {
			continue
		}
		isList := false
		if strings.HasPrefix(content, "- ") {
			isList = true
			ind += 2
			content = strings.TrimLeft(content[2:], " ")
		}
		key, val := splitKeyVal(content)
		out = append(out, Line{Indent: ind, Key: key, Val: val, List: isList, Num: num})
	}
	return out
}

// splitKeyVal splits `key: value` at the first colon followed by a space (or
// end of line) — URLs (`http://…`) stay whole. A colon-less line is a scalar.
// Surrounding quotes and trailing comments are stripped from the value.
func splitKeyVal(s string) (key, val string) {
	ci := -1
	for j := 0; j < len(s); j++ {
		if s[j] == ':' && (j == len(s)-1 || s[j+1] == ' ') {
			ci = j
			break
		}
	}
	if ci < 0 {
		return "", cleanScalar(s)
	}
	key = strings.Trim(strings.TrimSpace(s[:ci]), `"'`)
	return key, cleanScalar(strings.TrimSpace(s[ci+1:]))
}

// cleanScalar unquotes a scalar or cuts an unquoted trailing comment.
func cleanScalar(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		if end := strings.IndexByte(v[1:], v[0]); end >= 0 {
			return v[1 : 1+end]
		}
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// Span returns the index just past the block owned by ls[i]: every following
// line indented deeper than ls[i].
func Span(ls []Line, i int) int {
	j := i + 1
	for j < len(ls) && ls[j].Indent > ls[i].Indent {
		j++
	}
	return j
}

// ItemSpan is Span for a LIST ITEM: sibling keys of the item share its
// indent (the dash counted), so the item ends at a dedent or the next dash.
func ItemSpan(ls []Line, i int) int {
	j := i + 1
	for j < len(ls) && (ls[j].Indent > ls[i].Indent || (ls[j].Indent == ls[i].Indent && !ls[j].List)) {
		j++
	}
	return j
}
