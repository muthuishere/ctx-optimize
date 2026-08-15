package analyze

import "strings"

// Repo content reaches a terminal, so it is UNTRUSTED INPUT to that terminal.
//
// Measured on a hostile fixture: a doc comment carried 4 ESC, 2 BEL and 1 CR
// straight to stdout. That permits screen-clear (`\x1b[2J`), OSC title-set,
// and — worst — `real\rFAKE`, which renders as only `FAKE`, so a hostile file
// can visually REPLACE what the tool appears to say. An answer the reader
// cannot trust to be the answer defeats the point of citing anything.
// docs/CRITIQUE.md item 3 names this class; this is it, concretely.
//
// `--json` is already correct (encoding/json escapes control bytes), so the
// fix belongs only on the text path.
//
// WHICH BYTES ARE STRUCTURAL depends on the surface, and that decision is the
// whole design:
//
//	SafeLine  — for values rendered INTO a line we compose: labels, signatures,
//	            ids, paths, boundary identifiers, a single doc line. Here a
//	            newline or tab is not content, it is a break in OUR layout, so
//	            every C0 byte and DEL is escaped. A label containing a newline
//	            would otherwise forge a second output row.
//
//	SafeBlock — for verbatim source we print as a block: a card BODY. Newline
//	            and tab ARE the content (indentation is meaning in Python, and
//	            a body without line breaks is unreadable), so those two survive
//	            and everything else in C0 plus DEL is escaped. CR does NOT
//	            survive: a lone CR is the overwrite attack, and a CRLF file's
//	            CR is redundant once the LF is kept.
//
// Escapes render as the Go literal (\x1b, \r) so the reader SEES what was in
// the file rather than losing it — the byte is disclosed, just disarmed.
func SafeLine(s string) string { return sanitize(s, false) }

// SafeBlock is SafeLine but keeps newline and tab, for verbatim source bodies.
func SafeBlock(s string) string { return sanitize(s, true) }

func sanitize(s string, keepLayout bool) string {
	// Fast path: the overwhelming majority of real content is clean, and this
	// runs per line of every rendered card.
	clean := true
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c == 0x7f {
			if keepLayout && (c == '\n' || c == '\t') {
				continue
			}
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != 0x7f {
			b.WriteByte(c)
			continue
		}
		if keepLayout && (c == '\n' || c == '\t') {
			b.WriteByte(c)
			continue
		}
		switch c {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			const hex = "0123456789abcdef"
			b.WriteString(`\x`)
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xf])
		}
	}
	return b.String()
}
