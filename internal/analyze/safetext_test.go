package analyze

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// Repo content is untrusted terminal input. The attack that matters is not the
// screen-clear, it is `real\rFAKE`: a lone CR makes a terminal render only
// FAKE, so a hostile file can visually REPLACE what the tool appears to say —
// and the whole product is "cite this, do not re-verify".
func TestSafeLineDisarmsControlBytes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"real\rFAKE", `real\rFAKE`},                 // the overwrite attack
		{"a\x1b[2Jb", `a\x1b[2Jb`},                   // screen clear
		{"a\x07b", `a\x07b`},                         // BEL
		{"a\x1b]0;title\x07b", `a\x1b]0;title\x07b`}, // OSC title-set
		{"line\nforged", `line\nforged`},             // a newline would forge a row
		{"tab\there", `tab\there`},
		{"del\x7fx", `del\x7fx`},
		{"plain text", "plain text"},     // untouched
		{"unicode → ok", "unicode → ok"}, // multibyte survives
	} {
		if got := SafeLine(tc.in); got != tc.want {
			t.Errorf("SafeLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A body is verbatim source: newline and tab ARE the content (indentation is
// meaning in Python), so they survive. A lone CR does not — it is the attack,
// and in a CRLF file it is redundant once the LF is kept.
func TestSafeBlockKeepsLayoutButNotEscapes(t *testing.T) {
	in := "def f():\n\treturn 1\r\n\x1b[31mred\x07"
	got := SafeBlock(in)
	if !strings.Contains(got, "\n\treturn 1") {
		t.Errorf("newline/tab must survive a body: %q", got)
	}
	for _, bad := range []string{"\r", "\x1b", "\x07"} {
		if strings.Contains(got, bad) {
			t.Errorf("raw %q survived into a body: %q", bad, got)
		}
	}
}

// End to end: a poisoned label and doc must not reach the rendered card raw.
func TestRenderCardSanitizes(t *testing.T) {
	c := &CardData{
		Node: schema.Node{
			ID: "x", Label: "safe\rEVIL", Kind: "function",
			FileType: "code", Source: "a.go", Location: "L1-L2",
		},
		Signature: "func f()\x1b[2J",
		Doc:       "docs\x07here",
		Body:      "line1\n\tline2",
	}
	out := RenderCard(c)
	for _, bad := range []string{"\r", "\x1b", "\x07"} {
		if strings.Contains(out, bad) {
			t.Errorf("raw control byte %q reached rendered card:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "line1\n") || !strings.Contains(out, "\tline2") {
		t.Errorf("body layout was mangled — indentation is meaning:\n%s", out)
	}
}
