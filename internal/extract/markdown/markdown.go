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

	"github.com/muthuishere/ctx-optimize/internal/extract/ignore"
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
func Extract(root string) (*schema.Batch, error) { return ExtractExcluding(root, nil) }

// ExtractExcluding is Extract with subtrees pruned — the multi-module root
// residual: module dirs (absolute paths) are gathered into their own stores
// and must not re-enter the parent's batch.
func ExtractExcluding(root string, exclude []string) (*schema.Batch, error) {
	skip := map[string]bool{}
	for _, e := range exclude {
		if abs, err := filepath.Abs(e); err == nil {
			skip[abs] = true
		}
	}
	ignored := ignore.New(root) // .gitignore semantics via git itself; nil = no git
	b := &schema.Batch{Producer: ProducerName}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if ignored != nil {
			if rel, err := filepath.Rel(root, path); err == nil && rel != "." && ignored(filepath.ToSlash(rel)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() {
			if len(skip) > 0 {
				if abs, err := filepath.Abs(path); err == nil && skip[abs] {
					return filepath.SkipDir
				}
			}
			// Skip hidden dirs (.git, .ctxoptimize, …) and the usual noise;
			// a store must never ingest itself or its own config. Same list
			// as the code walk — a .md inside dist/ is generated output.
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" ||
				name == "vendor" || name == "target" || name == "dist" || name == "build" ||
				strings.HasSuffix(name, "-out")) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		isDoc := ext == ".md" || ext == ".txt"
		isConfig := configFile(name)
		if !isDoc && !isConfig {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if info, err := d.Info(); err == nil && info.Size() > maxConfigBytes && isConfig {
			return nil // a 5MB "config" is data, not configuration
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		if isConfig {
			extractConfig(b, rel, string(data))
			if ext == ".yaml" || ext == ".yml" {
				extractYAMLRoutes(b, rel, string(data)) // routes in specs/config (yamlroutes.go)
			}
			return nil
		}
		extractFile(b, rel, string(data))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

const maxConfigBytes = 256 * 1024

// configExts are property/config formats indexed as searchable documents.
var configExts = map[string]bool{
	".properties": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true,
}

// manifestNames are build/dependency manifests indexed by exact basename —
// NOT their whole extension class (every .json would be data, not config).
var manifestNames = map[string]bool{
	"package.json": true, "pom.xml": true, "go.mod": true, "go.work": true,
	"build.gradle": true, "build.gradle.kts": true,
	"settings.gradle": true, "settings.gradle.kts": true,
	"Cargo.toml": true, "pyproject.toml": true, "Makefile": true,
	"Dockerfile": true, "docker-compose.yml": true, "docker-compose.yaml": true,
	"Taskfile.yml": true, "Taskfile.yaml": true,
}

// configFile decides if a file is an indexable config/manifest. Anything
// that smells like a secret store is refused outright — a knowledge graph
// must never become the place credentials leak from.
func configFile(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "secret") || strings.Contains(lower, "credential") ||
		strings.HasPrefix(lower, ".env") || strings.Contains(lower, "password") {
		return false
	}
	if manifestNames[name] {
		return true
	}
	return configExts[strings.ToLower(filepath.Ext(name))]
}

// extractConfig emits a config/manifest file as one document node plus one
// line-anchored config_key node per key-bearing line — at EVERY nesting depth,
// not just the top level (ADR 2026-07-25-structured-formats S1: the old
// depth guard fired only when an indented line happened to carry trailing
// whitespace, so the same content produced different graphs depending on
// invisible characters). Recognized shapes: `key: …` / `key = …` and
// `[toml/ini section]` headers; gradle/manifest lines that match neither are
// left whole. This is lexical, never parsed semantics.
//
// Two deliberate limits, stated rather than hidden:
//   - Labels are FLAT leaves. A nested `port:` is emitted as bare `port`, so
//     labels collide across files and across parents (measured: 37.4% of this
//     repo's config_key labels also exist in another file). Dotted, parent-
//     qualified labels are parked as P3a — they need the query ranker's
//     dotted-label penalty lifted first, so the ambiguity is real today.
//   - YAML block scalars are skipped as opaque data (S2, see below): their
//     bodies are payload, not config structure.
func extractConfig(b *schema.Batch, rel, content string) {
	b.Nodes = append(b.Nodes, schema.Node{
		ID: rel, Label: filepath.Base(rel), Kind: "config",
		FileType: "config", Source: rel, Location: "L1",
	})
	usedSlugs := map[string]int{}
	lines := splitLines(content)
	// S2: a `key: |` / `key: >` header (any indicator) opens a literal/folded
	// block whose body is DATA. Skip every line indented deeper than the
	// opening key until a non-blank line at or below that indent closes it.
	inBlock, blockIndent := false, 0
	for i, line := range lines {
		t := strings.TrimRight(line, " \t\r")
		trimmed := strings.TrimSpace(t)
		if inBlock {
			// Blank lines belong to the block (a body may contain them);
			// only a non-blank line that dedents to the header closes it.
			if trimmed == "" || indentWidth(line) > blockIndent {
				continue
			}
			inBlock = false // fall through: this line is real structure again
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		var key string
		switch {
		case strings.HasPrefix(t, "[") && strings.HasSuffix(trimmed, "]"):
			key = strings.Trim(trimmed, "[]") // toml/ini section
		case strings.ContainsAny(t, ":="):
			cut := strings.IndexAny(t, ":=")
			key = strings.TrimSpace(t[:cut])
			// The header line itself IS structure and stays indexed; only its
			// body is skipped. Works for `- key: |` inside a list too, where
			// the header indent is measured at the `-`.
			if t[cut] == ':' && blockScalarHeader(t[cut+1:]) {
				inBlock, blockIndent = true, indentWidth(line)
			}
		}
		if key == "" || len(key) > 80 || strings.ContainsAny(key, "{}\"' ") {
			continue
		}
		s := slug(key)
		if s == "" {
			continue
		}
		if n := usedSlugs[s]; n > 0 {
			usedSlugs[s] = n + 1
			s = fmt.Sprintf("%s-%d", s, n+1)
		} else {
			usedSlugs[s] = 1
		}
		id := rel + "#" + s
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: key, Kind: "config_key",
			FileType: "config", Source: rel, Location: fmt.Sprintf("L%d", i+1),
		})
		b.Edges = append(b.Edges, schema.Edge{
			Source: rel, Target: id, Relation: "contains", Confidence: "EXTRACTED",
		})
	}
}

// indentWidth counts leading spaces/tabs, PLUS two columns for every `- ` list
// marker — the same rule yamlwalk uses, because a list item's key aligns with
// its siblings, not with the dash. Without this, `- powershell: |` measures as
// indent 0 and its sibling keys (`env:`, `displayName:` at indent 2) look like
// block CONTENT and get swallowed: measured on Newtonsoft.Json's
// azure-pipelines.yml, 9 real keys were dropped alongside the 11 PowerShell
// lines the block skip is meant to remove.
//
// YAML forbids tabs for indentation, so counting either as one column is
// sufficient for the relative comparison this is used for.
func indentWidth(line string) int {
	n := 0
	for {
		for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
			n++
		}
		// A nested `- - key:` opens two levels; keep consuming markers.
		if strings.HasPrefix(line[n:], "- ") {
			n += 2
			continue
		}
		return n
	}
}

// blockScalarHeader reports whether the value part of a `key: …` line opens a
// YAML literal (`|`) or folded (`>`) block scalar. Every indicator form is
// accepted — `|`, `|-`, `>+`, `|2`, `|-2`, `|2-` — optionally followed by a
// comment. Anything else (including a quoted "|" ) is a plain value.
func blockScalarHeader(val string) bool {
	f := strings.Fields(val)
	if len(f) == 0 {
		return false
	}
	if len(f) > 1 && !strings.HasPrefix(f[1], "#") {
		return false
	}
	h := f[0]
	if h[0] != '|' && h[0] != '>' {
		return false
	}
	digits, chomp := 0, 0
	for _, r := range h[1:] {
		switch {
		case r >= '1' && r <= '9':
			digits++
		case r == '+' || r == '-':
			chomp++
		default:
			return false
		}
	}
	return digits <= 1 && chomp <= 1
}

func extractFile(b *schema.Batch, rel, content string) {
	docID := rel
	b.Nodes = append(b.Nodes, schema.Node{
		ID: docID, Label: filepath.Base(rel), Kind: "document",
		FileType: "document", Source: rel, Location: "L1",
	})

	lines := splitLines(content)
	var stack []openSection
	sectionStart := map[string]int{}
	usedSlugs := map[string]int{} // repeated headings ("Files changed") get -2, -3…

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
			title := strings.TrimSpace(m[2])
			// A heading with no text is not a section: there is no name to
			// cite and no slug to build an id from. Emitting one produced a
			// node with an empty label, which the schema correctly refuses —
			// better to never emit it than to have Validate quarantine it.
			if title == "" || slug(title) == "" {
				continue
			}
			closeTo(level, lineNo-1)
			s := slug(title)
			usedSlugs[s]++
			if n := usedSlugs[s]; n > 1 {
				s = fmt.Sprintf("%s-%d", s, n)
			}
			secID := fmt.Sprintf("%s::%s", rel, s)
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

// splitLines splits on \n and strips a trailing \r, so a CRLF file behaves
// exactly like an LF one. Without this the CR rides along into every value we
// store, and a bare "# " heading in a CRLF file yields the title "\r" — which
// slugs to nothing and emitted a node with an EMPTY label. Chromium's
// third_party/hunspell_dictionaries/*.txt (shell-comment licence headers, CRLF)
// quarantined 18 nodes exactly that way.
func splitLines(content string) []string {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

func slug(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
