// Package markdown is the first tier-1 producer: deterministic extraction of
// markdown/plain-text documents into the one emit schema. Docs are nodes in
// the SAME graph as code — a doc node, section nodes per heading, contains
// edges, and reference edges for [[wikilinks]] and relative markdown links.
// Zero LLM, zero network; code producers (tree-sitter wasm) follow the same
// Producer contract in a later story.
package markdown

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

const ProducerName = "markdown"

var (
	headingRe  = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	wikilinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	mdLinkRe   = regexp.MustCompile(`\]\(([^)#]+\.md)[^)]*\)`)
)

// Extract walks root and emits one batch covering every .md/.txt file.
// Node IDs are root-relative paths (path-qualified — the lesson from
// graphify's bare-name collisions at 287k nodes).
func Extract(root string) (*schema.Batch, error) {
	b := &schema.Batch{Producer: ProducerName}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			// Skip the usual noise; a store must never ingest itself.
			if name == ".git" || name == "node_modules" || name == "vendor" || strings.HasSuffix(name, "-out") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".md" && ext != ".txt" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		extractFile(b, rel, string(data))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

func extractFile(b *schema.Batch, rel, content string) {
	docID := rel
	b.Nodes = append(b.Nodes, schema.Node{
		ID: docID, Label: filepath.Base(rel), Kind: "document",
		FileType: "document", Source: rel, Location: "L1",
	})

	lines := strings.Split(content, "\n")
	var stack []openSection
	sectionStart := map[string]int{}

	closeTo := func(level, endLine int) {
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			// Patch the section node's location now that we know its extent.
			for i := range b.Nodes {
				if b.Nodes[i].ID == top.id {
					b.Nodes[i].Location = fmt.Sprintf("L%d-L%d", sectionStart[top.id], endLine)
				}
			}
		}
	}

	for i, line := range lines {
		lineNo := i + 1
		if m := headingRe.FindStringSubmatch(line); m != nil {
			level := len(m[1])
			title := m[2]
			closeTo(level, lineNo-1)
			secID := fmt.Sprintf("%s::%s", rel, slug(title))
			parent := docID
			if len(stack) > 0 {
				parent = stack[len(stack)-1].id
			}
			b.Nodes = append(b.Nodes, schema.Node{
				ID: secID, Label: title, Kind: "section",
				FileType: "document", Source: rel, Location: fmt.Sprintf("L%d", lineNo),
			})
			b.Edges = append(b.Edges, schema.Edge{
				Source: parent, Target: secID, Relation: "contains", Confidence: schema.Extracted,
			})
			stack = append(stack, openSection{id: secID, level: level})
			sectionStart[secID] = lineNo
		}
		// Links become reference edges. Targets may resolve to nodes from
		// other batches (or nothing yet) — cross-batch linking is the point.
		for _, m := range wikilinkRe.FindAllStringSubmatch(line, -1) {
			b.Edges = append(b.Edges, schema.Edge{
				Source: currentScope(stack, docID), Target: strings.TrimSpace(m[1]),
				Relation: "references", Confidence: schema.Inferred,
			})
		}
		for _, m := range mdLinkRe.FindAllStringSubmatch(line, -1) {
			target := filepath.ToSlash(filepath.Join(filepath.Dir(rel), m[1]))
			b.Edges = append(b.Edges, schema.Edge{
				Source: currentScope(stack, docID), Target: target,
				Relation: "references", Confidence: schema.Extracted,
			})
		}
	}
	closeTo(1, len(lines))
}

type openSection struct {
	id    string
	level int
}

func currentScope(stack []openSection, docID string) string {
	if len(stack) > 0 {
		return stack[len(stack)-1].id
	}
	return docID
}

func slug(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
