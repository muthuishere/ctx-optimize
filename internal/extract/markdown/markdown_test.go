package markdown

import (
	"os"
	"path/filepath"
	"strings"
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

// Two identical headings in one file must not collide.
func TestDuplicateHeadingsGetSuffixes(t *testing.T) {
	dir := t.TempDir()
	md := "# Doc\n\n## Files changed\na\n\n## Files changed\nb\n"
	os.WriteFile(filepath.Join(dir, "d.md"), []byte(md), 0o644)
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("dup headings still collide: %v", err)
	}
	ids := map[string]bool{}
	for _, n := range b.Nodes {
		ids[n.ID] = true
	}
	if !ids["d.md::files-changed"] || !ids["d.md::files-changed-2"] {
		t.Fatalf("expected suffixed slugs, got %v", ids)
	}
}

func TestConfigAndManifestIngestion(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	write("application.properties", "server.port=8080\nspring.datasource.url=jdbc:pg\n")
	write("config.yaml", "database:\n  host: x\nqueue:\n  name: y\n")
	write("package.json", "{\n  \"name\": \"demo\"\n}\n")
	write("secrets.properties", "api_key=SHOULD_NEVER_ENTER\n")
	write(".env", "TOKEN=nope\n")
	write("dist/leftover.md", "# generated\n")
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, n := range b.Nodes {
		got[n.ID] = n.Kind
	}
	if got["application.properties"] != "config" || got["config.yaml"] != "config" || got["package.json"] != "config" {
		t.Fatalf("config files not ingested: %v", got)
	}
	if got["application.properties#server-port"] != "config_key" {
		t.Fatalf("top-level property key missing: %v", got)
	}
	if got["config.yaml#database"] != "config_key" || got["config.yaml#queue"] != "config_key" {
		t.Fatalf("yaml top-level keys missing: %v", got)
	}
	for id := range got {
		if strings.Contains(id, "secrets") || strings.Contains(id, ".env") {
			t.Fatalf("secret-ish file ingested: %s", id)
		}
		if strings.Contains(id, "dist/") {
			t.Fatalf("dist/ content ingested: %s", id)
		}
	}
}
