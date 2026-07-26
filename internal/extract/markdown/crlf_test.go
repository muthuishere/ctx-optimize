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
	// Real headings still land, without the CR.
	var found bool
	for _, n := range b.Nodes {
		if n.Label == "LICENCE / TRWYDDED" {
			found = true
		}
	}
	if !found {
		t.Error("a real CRLF heading must still be extracted, with the CR stripped")
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
