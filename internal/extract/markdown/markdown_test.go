package markdown

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func TestExtract(t *testing.T) {
	dir := t.TempDir()
	content := `# Title

Intro text with a [[Wiki Target]] link.

## Section One

Body referencing [other](other.md).

## Section Two

More.
`
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}

	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("producer emitted an invalid batch: %v", err)
	}

	byID := map[string]schema.Node{}
	for _, n := range b.Nodes {
		byID[n.ID] = n
	}
	if _, ok := byID["doc.md"]; !ok {
		t.Fatal("document node missing")
	}
	sec, ok := byID["doc.md::section-one"]
	if !ok {
		t.Fatalf("section node missing; have %v", keys(byID))
	}
	if sec.Kind != "section" || sec.Location == "" {
		t.Fatalf("section node malformed: %+v", sec)
	}

	var contains, wikiRefs, mdRefs int
	for _, e := range b.Edges {
		switch {
		case e.Relation == "contains":
			contains++
		case e.Relation == "references" && e.Target == "Wiki Target":
			wikiRefs++
		case e.Relation == "references" && e.Target == "other.md":
			mdRefs++
		}
	}
	if contains < 3 { // title + two sections
		t.Fatalf("want >=3 contains edges, got %d", contains)
	}
	if wikiRefs != 1 || mdRefs != 1 {
		t.Fatalf("reference edges wrong: wiki=%d md=%d", wikiRefs, mdRefs)
	}
}

func keys(m map[string]schema.Node) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
