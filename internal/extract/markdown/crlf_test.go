package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Found by onboarding chromium (2026-07-26): the gather printed
// "quarantined 18 invalid item(s) … label is required". The culprit was
// third_party/hunspell_dictionaries/README_*.txt — shell-comment licence
// headers, CRLF line endings, where every line starts with "#". A bare "# "
// line parsed as an H1 whose title was "\r", which slugged to nothing and
// produced a node with an EMPTY label and the id "file.txt::".
//
// Two separate defects, both pinned here: lines were split on "\n" only, so
// the CR rode along into stored values; and an empty heading was emitted
// rather than skipped.
func TestCRLFHeadingsDoNotEmitEmptyLabels(t *testing.T) {
	dir := t.TempDir()
	// Exactly the shape of the chromium file: CRLF, hash-prefixed prose, and
	// bare "# " separator lines.
	body := strings.Join([]string{
		"# LICENCE / TRWYDDED",
		"# (English text below).",
		"# ",
		"# AFF File:",
		"# ",
	}, "\r\n") + "\r\n"
	if err := os.WriteFile(filepath.Join(dir, "README_cy_GB.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range b.Nodes {
		if strings.TrimSpace(n.Label) == "" {
			t.Errorf("node %q has an empty label — the schema refuses it, so it must never be emitted", n.ID)
		}
		if strings.HasSuffix(n.ID, "::") {
			t.Errorf("node id %q ends in :: — an empty slug", n.ID)
		}
		if strings.Contains(n.Label, "\r") {
			t.Errorf("node %q carries a CR into its label: %q", n.ID, n.Label)
		}
	}
	// A .txt is plain text: `#` is a comment character, not a heading. So this
	// file yields ONE node — the document pointer — and no sections.
	// (This assertion is the reverse of what it said when written. It asserted
	// that `# LICENCE / TRWYDDED` was "a real CRLF heading"; measuring 30,289
	// real .txt files showed 95.1% of such "headings" are comment lines, so the
	// judgement was wrong, not the test.)
	for _, n := range b.Nodes {
		if n.Kind == "section" {
			t.Errorf("a .txt must not produce section nodes; got %q", n.Label)
		}
	}
	if len(b.Nodes) != 1 || b.Nodes[0].Kind != "document" {
		t.Errorf("want exactly one document node, got %d: %+v", len(b.Nodes), b.Nodes)
	}
}

// The CR-stripping and empty-heading fixes still matter — they just belong to
// markdown, which is where headings live. Same content, .md extension.
func TestCRLFHeadingsInMarkdown(t *testing.T) {
	dir := t.TempDir()
	body := strings.Join([]string{
		"# LICENCE / TRWYDDED",
		"# (English text below).",
		"# ",
		"# AFF File:",
	}, "\r\n") + "\r\n"
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range b.Nodes {
		if strings.TrimSpace(n.Label) == "" || strings.HasSuffix(n.ID, "::") {
			t.Errorf("empty heading emitted: %q", n.ID)
		}
		if strings.Contains(n.Label, "\r") {
			t.Errorf("node %q carries a CR into its label: %q", n.ID, n.Label)
		}
		if n.Label == "LICENCE / TRWYDDED" {
			found = true
		}
	}
	if !found {
		t.Error("a real CRLF heading in MARKDOWN must still be extracted, with the CR stripped")
	}
}

func TestSplitLinesStripsCR(t *testing.T) {
	got := splitLines("a\r\nb\nc\r\n")
	want := []string{"a", "b", "c", ""}
	if len(got) != len(want) {
		t.Fatalf("splitLines = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}
