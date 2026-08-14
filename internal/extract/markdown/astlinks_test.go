package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// mkrepo writes a fixture tree; keys are slash-relative paths.
func mkrepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func extract(t *testing.T, files map[string]string) *schema.Batch {
	t.Helper()
	b, err := Extract(mkrepo(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("producer emitted an invalid batch: %v", err)
	}
	return b
}

func sections(b *schema.Batch) map[string]schema.Node {
	out := map[string]schema.Node{}
	for _, n := range b.Nodes {
		if n.Kind == "section" {
			out[n.ID] = n
		}
	}
	return out
}

func refTargets(b *schema.Batch) map[string]schema.Edge {
	out := map[string]schema.Edge{}
	for _, e := range b.Edges {
		if e.Relation == "references" {
			out[e.Target] = e
		}
	}
	return out
}

// THE headline bug (ADR 4 D0): a heading inside a code block is not a heading.
// 67 such phantoms in this repo, 551 in reqsume — each queryable and citable.
func TestCodeBlockHeadingsAreNotSections(t *testing.T) {
	b := extract(t, map[string]string{"README.md": "# Real Heading\n\n" +
		"```bash\n# Install the thing\ncd /tmp\n```\n\n" +
		"```markdown\n# Example Doc Title\n```\n\n" +
		"~~~sh\n# Tilde Fenced\n~~~\n\n" +
		"    # Indented Code Heading\n\n" +
		"## Second Real\n"})
	secs := sections(b)
	for id, n := range secs {
		for _, phantom := range []string{"install", "example", "tilde", "indented"} {
			if strings.Contains(id, phantom) {
				t.Errorf("phantom section from a code block: %s (%q)", id, n.Label)
			}
		}
	}
	if _, ok := secs["README.md::real-heading"]; !ok {
		t.Error("the REAL heading was swallowed — the parser over-consumed")
	}
	if _, ok := secs["README.md::second-real"]; !ok {
		t.Error("the real heading AFTER the code blocks was swallowed")
	}
	if len(secs) != 2 {
		t.Errorf("want exactly 2 real sections, got %d: %v", len(secs), secs)
	}
}

// The over-consumption failure mode the ADR names as most likely: a fence that
// never closes, or fences nested in a list, must not eat the rest of the file.
func TestUnterminatedAndNestedFencesDoNotSwallowRealSections(t *testing.T) {
	t.Run("nested in list", func(t *testing.T) {
		b := extract(t, map[string]string{"a.md": "# Top\n\n" +
			"- item\n\n  ```go\n  // # not a heading\n  ```\n\n" +
			"## After List\n"})
		if _, ok := sections(b)["a.md::after-list"]; !ok {
			t.Errorf("a fence inside a list swallowed the next heading: %v", sections(b))
		}
	})
	t.Run("unterminated at EOF", func(t *testing.T) {
		b := extract(t, map[string]string{"b.md": "# Top\n\n```bash\n# never closed\n"})
		secs := sections(b)
		if _, ok := secs["b.md::top"]; !ok {
			t.Error("the heading before an unterminated fence was lost")
		}
		if len(secs) != 1 {
			t.Errorf("an unterminated fence must swallow its contents only: %v", secs)
		}
	})
}

// Setext headings are real headings the `^#` regex never saw (16 across the two
// measured repos) — false NEGATIVES, not phantoms.
func TestSetextHeadingsExtracted(t *testing.T) {
	b := extract(t, map[string]string{"s.md": "Setext Title\n============\n\nbody\n\nSub Head\n--------\n\nmore\n"})
	secs := sections(b)
	top, ok := secs["s.md::setext-title"]
	if !ok {
		t.Fatalf("setext H1 missing: %v", secs)
	}
	// splitLines yields a trailing empty element after the final newline, so a
	// 9-line file measures 10 — the same count the pre-goldmark code used.
	if top.Location != "L1-L10" {
		t.Errorf("setext section range wrong: %s", top.Location)
	}
	sub, ok := secs["s.md::sub-head"]
	if !ok {
		t.Fatalf("setext H2 missing: %v", secs)
	}
	if sub.Location != "L6-L10" {
		t.Errorf("setext H2 range = %s, want L6-L10", sub.Location)
	}
}

// D1: resolve-or-drop. Dead, absolute and escaping targets get no edge — this
// is what keeps 84 dead /private/tmp links out of our own store.
func TestDeadAbsoluteAndEscapingLinksDropped(t *testing.T) {
	b := extract(t, map[string]string{
		"docs/page.md": "# P\n" +
			"[live](../real.md)\n" +
			"[dead](./missing.md)\n" +
			"[abs](/private/tmp/scratch/bio.c:1828)\n" +
			"[escape](../../outside.md)\n" +
			"[dir](../sub)\n" +
			"[ext](https://api.example.com/v1)\n" +
			"[mail](mailto:x@y.z)\n",
		"real.md":     "# R\n",
		"sub/keep.md": "# K\n",
	})
	got := refTargets(b)
	if _, ok := got["real.md"]; !ok {
		t.Errorf("the one resolvable link was dropped: %v", got)
	}
	if len(got) != 1 {
		t.Errorf("only the resolvable link may survive, got %v", got)
	}
}

// D2/D3/D4: where a surviving link points.
func TestLinkTargetsAnchorsImagesAndCode(t *testing.T) {
	b := extract(t, map[string]string{
		"README.md": "# Home\n\n" +
			"[code](internal/store/store.go)\n" +
			"[line](internal/store/store.go#L42)\n" +
			"[self](#usage)\n" +
			"[cross](docs/cli.md#flags)\n" +
			"[nosuch](docs/cli.md#absent)\n" +
			"![logo](assets/logo.png)\n\n" +
			"## Usage\n",
		"docs/cli.md":             "# CLI\n\n## Flags\n",
		"internal/store/store.go": "package store\n",
		"assets/logo.png":         "\x89PNG\r\n",
	})
	got := refTargets(b)

	if e, ok := got["internal/store/store.go"]; !ok {
		t.Error("D2: doc -> code link missing")
	} else if e.Confidence != schema.Extracted {
		t.Errorf("D2: want EXTRACTED, got %s", e.Confidence)
	} else if e.Metadata["anchor"] != "L42" {
		// both links share one target; the #L42 one carries the anchor
		t.Errorf("D2: #L42 fragment must survive as metadata.anchor, got %v", e.Metadata)
	}
	if _, ok := got["README.md::usage"]; !ok {
		t.Errorf("D3: same-doc #anchor must target the section node: %v", got)
	}
	if _, ok := got["docs/cli.md::flags"]; !ok {
		t.Errorf("D3: cross-file #anchor must target the section node: %v", got)
	}
	if e, ok := got["docs/cli.md"]; !ok {
		t.Error("D3: an anchor with no matching section must fall back to the file node")
	} else if e.Metadata["anchor"] != "absent" {
		t.Errorf("D3 fallback must keep the fragment: %v", e.Metadata)
	}
	if e, ok := got["assets/logo.png"]; !ok {
		t.Error("D4: image link missing")
	} else if e.Metadata["link"] != "image" {
		t.Errorf("D4: image needs metadata link=image, got %v", e.Metadata)
	}
	for _, e := range b.Edges {
		if e.Relation == "uses_image" {
			t.Error("D4: images must NOT use uses_image — docker/k8s owns that relation")
		}
	}
}

// Wikilinks are not CommonMark, so they keep a regex — but it now runs over
// block source, which excludes code by construction.
func TestWikilinksSkipCodeBlocksAndKeepScope(t *testing.T) {
	b := extract(t, map[string]string{"w.md": "# Top\n\n" +
		"Prose with [[Real Target]].\n\n" +
		"```bash\n[[Fenced Target]]\n```\n\n" +
		"    [[Indented Target]]\n\n" +
		"## Sub\n\n[[Scoped Target]]\n"})
	var scoped string
	seen := map[string]bool{}
	for _, e := range b.Edges {
		if e.Relation != "references" {
			continue
		}
		seen[e.Target] = true
		if e.Target == "Scoped Target" {
			scoped = e.Source
			if e.Confidence != schema.Inferred {
				t.Errorf("wikilinks stay INFERRED, got %s", e.Confidence)
			}
		}
	}
	if !seen["Real Target"] {
		t.Error("a prose wikilink was lost")
	}
	if seen["Fenced Target"] || seen["Indented Target"] {
		t.Errorf("a wikilink inside code became an edge: %v", seen)
	}
	if scoped != "w.md::sub" {
		t.Errorf("wikilink must attach to its enclosing section, got %q", scoped)
	}
}

// Section ranges and nesting are the store's identity contract; goldmark must
// reproduce them exactly.
func TestSectionRangesAndNestingUnchanged(t *testing.T) {
	b := extract(t, map[string]string{"r.md": "# One\n\ntext\n\n## Two\n\nmore\n\n### Three\n\nx\n\n## Four\n\ny\n"})
	secs := sections(b)
	// A sibling closes at (heading line - 1); EOF closes at len(splitLines),
	// which counts the empty element after the trailing newline. Both rules are
	// carried over verbatim from the pre-goldmark implementation.
	want := map[string]string{
		"r.md::one": "L1-L16", "r.md::two": "L5-L12", "r.md::three": "L9-L12", "r.md::four": "L13-L16",
	}
	for id, loc := range want {
		n, ok := secs[id]
		if !ok {
			t.Fatalf("missing section %s: %v", id, secs)
		}
		if n.Location != loc {
			t.Errorf("%s location = %s, want %s", id, n.Location, loc)
		}
	}
	parent := map[string]string{}
	for _, e := range b.Edges {
		if e.Relation == "contains" {
			parent[e.Target] = e.Source
		}
	}
	if parent["r.md::three"] != "r.md::two" || parent["r.md::two"] != "r.md::one" || parent["r.md::one"] != "r.md" {
		t.Errorf("nesting broken: %v", parent)
	}
}

// Reference-style links resolve through their definition — the construct that
// decided goldmark over a hand scanner.
func TestReferenceStyleLinksResolve(t *testing.T) {
	b := extract(t, map[string]string{
		"d.md":          "# D\n\nSee [the guide][g] and [inline](docs/cli.md).\n\n[g]: docs/guide.md\n",
		"docs/guide.md": "# G\n",
		"docs/cli.md":   "# C\n",
	})
	got := refTargets(b)
	if _, ok := got["docs/guide.md"]; !ok {
		t.Errorf("reference-style link did not resolve through its definition: %v", got)
	}
}

func TestDeterministicAcrossRuns(t *testing.T) {
	files := map[string]string{
		"a.md":    "# A\n\n[x](b.md#beta) [[W]]\n\n## Alpha\n",
		"b.md":    "# B\n\n## Beta\n\n[back](a.md#alpha)\n",
		"c.md":    "# C\n\n![i](img.png)\n",
		"img.png": "\x89PNG\r\n",
	}
	root := mkrepo(t, files)
	first, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		again, err := Extract(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Nodes) != len(first.Nodes) || len(again.Edges) != len(first.Edges) {
			t.Fatalf("run %d: counts vary (%d/%d vs %d/%d)", i,
				len(again.Nodes), len(again.Edges), len(first.Nodes), len(first.Edges))
		}
		for j := range first.Nodes {
			if again.Nodes[j].ID != first.Nodes[j].ID {
				t.Fatalf("run %d: node order varies at %d", i, j)
			}
		}
		for j := range first.Edges {
			if again.Edges[j].Target != first.Edges[j].Target {
				t.Fatalf("run %d: edge order varies at %d", i, j)
			}
		}
	}
}
