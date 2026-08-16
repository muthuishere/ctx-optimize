// Package tomlwalk is the shared TOML table walker used by the manifests
// producer's pyproject/Cargo lanes. Like its sibling internal/extract/yamlwalk
// it is deliberately NOT a parser (stdlib rule: no TOML library) — a
// deterministic flattener that is good enough for exactly the shapes those
// lanes read: which TABLE a key lives in, its logical value (joined across
// continuation lines), arrays of strings, and inline tables.
//
// The table anchor is the whole point (ADR 2026-07-25-structured-formats S7,
// requirement 1): callers harvest dependencies ONLY from the tables that
// declare them, never by sniffing arrays for version-looking strings. Flask's
// own `[tool.tox.env.tests-min] commands = [["uv","pip","install",
// "blinker==1.9.0",…]]` makes the sniffing heuristic invent ~12 phantom deps;
// knowing the table makes it invisible.
//
// Anything it cannot confidently represent is DROPPED, never guessed:
// multi-line basic/literal strings (`"""` / `”'`) are skipped whole — a
// `description = """…dependencies = ["x"]…"""` must not yield a dependency —
// and a value line with no `=` outside quotes is ignored. Multi-line inline
// tables are illegal TOML 1.0 and are not chased.
package tomlwalk

import "strings"

// Entry is one meaningful TOML line. A header line (`[a.b]` / `[[a.b]]`) is
// emitted with an empty Key so callers can see a table exists even when it
// holds no keys; every other Entry is a `key = value` assignment tagged with
// the table it belongs to. Val is the logical value: comments cut,
// continuation lines joined, surrounding whitespace trimmed, quotes intact.
type Entry struct {
	Table      []string
	Key        string
	Val        string
	ArrayTable bool
	Num        int
}

// Path is the dotted table path, the form callers match against.
func (e Entry) Path() string { return strings.Join(e.Table, ".") }

// Parse flattens one TOML document into Entries, in file order.
func Parse(content string) []Entry {
	lines := strings.Split(content, "\n")
	var out []Entry
	var table []string
	arrayTable := false
	i := 0
	for i < len(lines) {
		raw := strings.TrimRight(lines[i], "\r")
		body, open, delta := scanLine(raw)
		num := i + 1
		if open != "" { // multi-line string starts here: opaque text
			i = skipMultiline(lines, i, open)
			continue
		}
		i++
		t := strings.TrimSpace(body)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "[") {
			segs, isArr, ok := parseHeader(t)
			if !ok {
				continue
			}
			table, arrayTable = segs, isArr
			out = append(out, Entry{Table: table, ArrayTable: isArr, Num: num})
			continue
		}
		eq := findEq(t)
		if eq < 0 {
			continue // not an assignment we can represent
		}
		keyPart, val := strings.TrimSpace(t[:eq]), strings.TrimSpace(t[eq+1:])
		// Join continuation lines while brackets/braces stay unbalanced.
		dropped := false
		for delta > 0 && i < len(lines) {
			nraw := strings.TrimRight(lines[i], "\r")
			nbody, nopen, nd := scanLine(nraw)
			if nopen != "" { // a multi-line string inside a value: unrepresentable
				i = skipMultiline(lines, i, nopen)
				dropped = true
				break
			}
			i++
			val += " " + strings.TrimSpace(nbody)
			delta += nd
		}
		segs := splitDotted(keyPart)
		if dropped || val == "" || len(segs) == 0 {
			continue
		}
		tbl := table
		if len(segs) > 1 {
			tbl = append(append([]string{}, table...), segs[:len(segs)-1]...)
		}
		out = append(out, Entry{
			Table: tbl, ArrayTable: arrayTable,
			Key: segs[len(segs)-1], Val: strings.TrimSpace(val), Num: num,
		})
	}
	return out
}

// scanLine cuts an unquoted trailing `#` comment and reports (a) the open
// multi-line-string delimiter when the line ends inside one and (b) the net
// bracket/brace depth change outside strings.
func scanLine(s string) (body, open string, delta int) {
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], `"""`), strings.HasPrefix(s[i:], `'''`):
			d := s[i : i+3]
			if j := strings.Index(s[i+3:], d); j >= 0 {
				i += 3 + j + 3
				continue
			}
			return s, d, delta
		case s[i] == '"' || s[i] == '\'':
			j := skipString(s, i)
			if j < 0 {
				return s, "", delta // unterminated: refuse to guess
			}
			i = j
		case s[i] == '#':
			return s[:i], "", delta
		case s[i] == '[' || s[i] == '{':
			delta++
			i++
		case s[i] == ']' || s[i] == '}':
			delta--
			i++
		default:
			i++
		}
	}
	return s, "", delta
}

// skipMultiline returns the index of the first line AFTER the one closing an
// open multi-line string that started on line i.
func skipMultiline(lines []string, i int, delim string) int {
	for j := i + 1; j < len(lines); j++ {
		if strings.Contains(lines[j], delim) {
			return j + 1
		}
	}
	return len(lines)
}

// skipString returns the index just past the single-line string starting at i.
func skipString(s string, i int) int {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		if q == '"' && s[j] == '\\' {
			j++
			continue
		}
		if s[j] == q {
			return j + 1
		}
	}
	return -1
}

// findEq returns the index of the first `=` outside a string, or -1.
func findEq(s string) int {
	for i := 0; i < len(s); {
		switch {
		case s[i] == '"' || s[i] == '\'':
			j := skipString(s, i)
			if j < 0 {
				return -1
			}
			i = j
		case s[i] == '=':
			return i
		default:
			i++
		}
	}
	return -1
}

// parseHeader splits `[a."b.c"]` / `[[a.b]]` into unquoted segments.
func parseHeader(t string) (segs []string, arrayTable, ok bool) {
	body := t[1:]
	if strings.HasPrefix(t, "[[") {
		arrayTable, body = true, t[2:]
	}
	end := -1
	for i := 0; i < len(body) && end < 0; {
		switch {
		case body[i] == '"' || body[i] == '\'':
			j := skipString(body, i)
			if j < 0 {
				return nil, false, false
			}
			i = j
		case body[i] == ']':
			end = i
		default:
			i++
		}
	}
	if end < 0 {
		return nil, false, false
	}
	segs = splitDotted(body[:end])
	return segs, arrayTable, len(segs) > 0
}

// splitDotted splits a dotted key/header path at `.` outside strings and
// unquotes each segment. An empty segment invalidates the whole path.
func splitDotted(s string) []string {
	var segs []string
	start := 0
	flush := func(end int) bool {
		seg := Unquote(strings.TrimSpace(s[start:end]))
		if seg == "" {
			return false
		}
		segs = append(segs, seg)
		return true
	}
	for i := 0; i < len(s); {
		switch {
		case s[i] == '"' || s[i] == '\'':
			j := skipString(s, i)
			if j < 0 {
				return nil
			}
			i = j
		case s[i] == '.':
			if !flush(i) {
				return nil
			}
			i++
			start = i
		default:
			i++
		}
	}
	if !flush(len(s)) {
		return nil
	}
	return segs
}

// Unquote strips one layer of matching basic/literal quotes.
func Unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// IsArray / IsInlineTable classify a logical value.
func IsArray(val string) bool       { return strings.HasPrefix(strings.TrimSpace(val), "[") }
func IsInlineTable(val string) bool { return strings.HasPrefix(strings.TrimSpace(val), "{") }

// Elements splits an array value into its top-level element texts.
func Elements(val string) []string { return splitTop(inside(val, '[', ']')) }

// Strings is Elements keeping only quoted string elements, unquoted — a nested
// array or inline table element is DROPPED (the tox-commands guard).
func Strings(val string) []string {
	var out []string
	for _, el := range Elements(val) {
		if len(el) >= 2 && (el[0] == '"' || el[0] == '\'') {
			out = append(out, Unquote(el))
		}
	}
	return out
}

// InlineFields splits an inline table into ordered {name, raw value} pairs.
func InlineFields(val string) [][2]string {
	var out [][2]string
	for _, f := range splitTop(inside(val, '{', '}')) {
		eq := findEq(f)
		if eq < 0 {
			continue
		}
		segs := splitDotted(f[:eq])
		if len(segs) == 0 {
			continue
		}
		out = append(out, [2]string{segs[len(segs)-1], strings.TrimSpace(f[eq+1:])})
	}
	return out
}

// Field returns an inline table's raw field value, or "".
func Field(val, name string) string {
	for _, f := range InlineFields(val) {
		if f[0] == name {
			return f[1]
		}
	}
	return ""
}

// inside returns the text between v's leading open delimiter and its matching
// close, or "" when v is not that shape.
func inside(v string, open, closeCh byte) string {
	v = strings.TrimSpace(v)
	if len(v) < 2 || v[0] != open {
		return ""
	}
	depth := 0
	for i := 0; i < len(v); {
		switch {
		case v[i] == '"' || v[i] == '\'':
			j := skipString(v, i)
			if j < 0 {
				return ""
			}
			i = j
			continue
		case v[i] == '[' || v[i] == '{':
			depth++
		case v[i] == ']' || v[i] == '}':
			depth--
			if depth == 0 {
				if v[i] != closeCh {
					return ""
				}
				return v[1:i]
			}
		}
		i++
	}
	return ""
}

// splitTop splits at top-level commas, dropping empty pieces (a trailing
// comma is legal TOML).
func splitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	add := func(end int) {
		if p := strings.TrimSpace(s[start:end]); p != "" {
			out = append(out, p)
		}
	}
	for i := 0; i < len(s); {
		switch {
		case s[i] == '"' || s[i] == '\'':
			j := skipString(s, i)
			if j < 0 {
				return out
			}
			i = j
			continue
		case s[i] == '[' || s[i] == '{':
			depth++
		case s[i] == ']' || s[i] == '}':
			depth--
		case s[i] == ',' && depth == 0:
			add(i)
			start = i + 1
		}
		i++
	}
	add(len(s))
	return out
}
