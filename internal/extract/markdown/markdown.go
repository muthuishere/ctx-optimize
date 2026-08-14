// Package markdown is the first tier-1 producer: deterministic extraction of
// markdown/plain-text documents into the one emit schema. Docs are nodes in
// the SAME graph as code — a doc node, section nodes per heading, contains
// edges, and reference edges for [[wikilinks]] and relative markdown links.
// Zero LLM, zero network; code producers (tree-sitter wasm) follow the same
// Producer contract in a later story.
package markdown

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/ignore"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const ProducerName = "markdown"

// wikilinkRe is the ONE regex left in the markdown path. `[[target]]` is not
// CommonMark, so goldmark cannot see it — and it is not even a single inline
// node: goldmark tokenizes `[[Wiki Target]]` into four Text nodes
// ("Intro with a [", "[", "Wiki Target]", "] link."), because the brackets are
// literal text once no link definition matches. So this runs over a BLOCK's raw
// source lines, never over inline text nodes. Running it per block (rather than
// per file line, as before) is what excludes fenced and indented code, since
// code blocks are skipped by kind.
var wikilinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

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
	// One parser per Extract call, never a package-level singleton: producers
	// run in parallel across modules in a multi-module gather.
	ex := &docExtractor{
		root:  root,
		b:     b,
		md:    goldmark.New(),
		slugs: map[string]map[string]bool{},
	}
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
		ex.extractFile(rel, string(data), ext == ".md")
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Link targets resolve only once every file's headings are known: a link
	// may point at a section of a document the walk has not reached yet.
	ex.flushRefs()
	return b, nil
}

// docExtractor carries the state a single-pass walk cannot: the per-file
// section slug index (for cross-file `#anchor` targets, D3) and the link edges
// deferred until that index is complete.
type docExtractor struct {
	root    string
	b       *schema.Batch
	md      goldmark.Markdown
	slugs   map[string]map[string]bool // rel -> set of emitted section slugs
	pending []pendingRef
}

// pendingRef is a link seen but not yet resolvable.
type pendingRef struct {
	source  string // node id of the section (or document) the link sits in
	rel     string // the LINKING file, so relative targets resolve from its dir
	dest    string // raw destination, exactly as written
	isImage bool
}

// flushRefs resolves every deferred link and appends the surviving edges.
// Deterministic: pending order is walk order, which is filepath.WalkDir's
// lexical order, and resolution is pure.
func (ex *docExtractor) flushRefs() {
	for _, p := range ex.pending {
		target, meta, ok := ex.resolve(p)
		if !ok {
			continue // D1: resolve-or-drop. A miss is honest; a guess is not.
		}
		ex.b.Edges = append(ex.b.Edges, schema.Edge{
			Source: p.source, Target: target, Relation: "references",
			Confidence: schema.Extracted, Metadata: meta,
		})
	}
}

// resolve implements D1-D4: where a link points, and whether it points anywhere
// real. Returns ok=false for everything that does not land on a node we have —
// external URLs, dead paths, directories, and anything escaping the repo root.
func (ex *docExtractor) resolve(p pendingRef) (string, map[string]string, bool) {
	dest := strings.TrimSpace(p.dest)
	if dest == "" {
		return "", nil, false
	}
	// External destinations get no edge and no node (ADR 4 context: all 141
	// doc URLs measured across two repos were bibliography, not architecture).
	if strings.Contains(dest, "://") || strings.HasPrefix(dest, "mailto:") ||
		strings.HasPrefix(dest, "//") {
		return "", nil, false
	}
	rawPath, frag := dest, ""
	if i := strings.IndexByte(dest, '#'); i >= 0 {
		rawPath, frag = dest[:i], dest[i+1:]
	}
	meta := map[string]string{}
	if p.isImage {
		meta["link"] = "image" // D4: NOT uses_image — docker/k8s owns that relation.
	}

	// A bare `#anchor` targets a section of the linking document itself.
	if rawPath == "" {
		if s := slug(frag); s != "" && ex.slugs[p.rel][s] {
			return p.rel + "::" + s, orNil(meta), true
		}
		return "", nil, false
	}

	if filepath.IsAbs(rawPath) || strings.HasPrefix(rawPath, "~") {
		return "", nil, false // absolute paths are not repo-relative, by construction
	}
	target := path.Join(path.Dir(p.rel), rawPath)
	if target == ".." || strings.HasPrefix(target, "../") {
		return "", nil, false // escaped the repo; no node can exist for it
	}
	// The precision gate: the path must be a real FILE on disk. This is what
	// keeps the 84 dead `/private/tmp/...` links in our own proof/ transcripts
	// out of the graph, and directory links (which have no node) with them.
	st, err := os.Stat(filepath.Join(ex.root, filepath.FromSlash(target)))
	if err != nil || st.IsDir() {
		return "", nil, false
	}
	// D3: a fragment on a markdown file retargets to that file's section node
	// when the section exists; otherwise the file node keeps the edge and the
	// fragment is preserved as evidence rather than silently dropped (D2).
	if frag != "" {
		if s := slug(frag); s != "" && ex.slugs[target][s] {
			return target + "::" + s, orNil(meta), true
		}
		meta["anchor"] = frag
	}
	return target, orNil(meta), true
}

// orNil keeps edges byte-identical to the pre-ADR shape when there is nothing
// to say: an empty map would serialize as `"metadata":{}` and churn every
// existing snapshot line.
func orNil(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
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

// extractFile emits the per-file document node, and — only for real markdown —
// its heading sections and link edges.
//
// A `.txt` gets the document node and nothing else. `#` is a comment character
// in shell, python, conf, ini, requirements.txt, CMakeLists.txt, robots.txt and
// licence headers; it is a heading only in markdown. Measured over 30,289 real
// `.txt` files across 22 repos: 6,902 section nodes, of which **95.1% were
// comment lines or mid-sentence prose fragments**. Linux's 1,695 `.txt` files
// yielded ZERO genuine headings. And they were not harmless — they ranked FIRST:
// junk `.txt` sections took 26–30% of top-10 query slots wherever they existed,
// and one repo's 35 `.txt` files produced 16.1% of its entire store.
//
// A threshold rule ("is `#` a comment char in this file?") was designed and
// rejected: it needed two invented numbers, and this repo already refused a
// wiki page cap for exactly that reason. 95% junk means the extraction is not
// worth having, not that it needs tuning.
//
// The cost, stated: a `.txt` that genuinely IS markdown (`llms.txt`, LLM prompts
// kept as `.txt`, a manuscript) loses its sections and becomes reachable by
// filename rather than by content. The fix for those is to name the file `.md`,
// and the store's own tool-choice ladder already routes literal-text questions
// to grep. That is a smaller, more predictable cost than a heuristic misfiring
// in ways nobody can enumerate.
//
// The document node is kept UNCONDITIONALLY: internal/extract/manifests emits
// `declares` edges anchored on the file path and no node of its own, so this is
// the only node backing every python dependency edge — and PartitionValidate
// does not quarantine absent endpoints, so dropping it would dangle silently.
// The parse is a real CommonMark AST (goldmark) rather than per-line regexes.
// That is not a style preference: a line regex cannot see block context, so
// every `# comment` inside a ```bash example was emitted as a real section —
// 67 phantom sections in this repo and 551 in reqsume (9.4% of all sections),
// each one queryable and citable, each one a lie with a true file:line on it.
// Fenced AND indented code blocks are now excluded by construction, and the
// same parse yields setext headings, reference links and autolinks the regexes
// never saw. Cost: ~24ms per 3.5MB, against multi-second gathers.
func (ex *docExtractor) extractFile(rel, content string, markdown bool) {
	b := ex.b
	docID := rel
	b.Nodes = append(b.Nodes, schema.Node{
		ID: docID, Label: filepath.Base(rel), Kind: "document",
		FileType: "document", Source: rel, Location: "L1",
	})
	if !markdown {
		return
	}

	// Normalize CRLF before parsing. Line COUNTS are unaffected (every \r\n
	// becomes exactly one \n), so offsets still map to the right line, and the
	// CR can no longer ride into a label — the chromium defect splitLines fixed.
	content = strings.ReplaceAll(content, "\r\n", "\n")
	src := []byte(content)
	blankFrontmatter(src)
	lines := splitLines(content)
	lineAt := newLineIndex(src)

	var stack []openSection
	sectionStart := map[string]int{}
	usedSlugs := map[string]int{} // repeated headings ("Files changed") get -2, -3…
	slugSet := map[string]bool{}
	ex.slugs[rel] = slugSet

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
	scope := func() string { return currentScope(stack, docID) }

	doc := ex.md.Parser().Parse(text.NewReader(src))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Heading:
			title, lineNo, ok := headingText(v, src, lineAt)
			// A heading with no text is not a section: no name to cite, no slug
			// to build an id from. Skipping BEFORE closeTo also preserves the
			// old behavior that an empty heading does not close open sections.
			if !ok || slug(title) == "" {
				break
			}
			closeTo(v.Level, lineNo-1)
			s := slug(title)
			usedSlugs[s]++
			if k := usedSlugs[s]; k > 1 {
				s = fmt.Sprintf("%s-%d", s, k)
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
			stack = append(stack, openSection{id: secID, level: v.Level})
			sectionStart[secID] = lineNo
			slugSet[s] = true
		case *ast.Link:
			ex.pending = append(ex.pending, pendingRef{scope(), rel, string(v.Destination), false})
		case *ast.Image:
			ex.pending = append(ex.pending, pendingRef{scope(), rel, string(v.Destination), true})
		}
		// Wikilinks ride the block's RAW source: goldmark shreds `[[x]]` across
		// several Text nodes, so only the unparsed line still holds the shape.
		// Code blocks are skipped by kind — that is the fence fix for wikilinks.
		if n.Type() == ast.TypeBlock && !isCodeBlock(n) {
			if segs := n.Lines(); segs != nil {
				for i := 0; i < segs.Len(); i++ {
					seg := segs.At(i)
					for _, m := range wikilinkRe.FindAllStringSubmatch(string(src[seg.Start:seg.Stop]), -1) {
						b.Edges = append(b.Edges, schema.Edge{
							Source: scope(), Target: strings.TrimSpace(m[1]),
							Relation: "references", Confidence: schema.Inferred,
						})
					}
				}
			}
		}
		return ast.WalkContinue, nil
	})
	closeTo(1, len(lines))
}

// headingText returns a heading's raw source text and its 1-based line.
// RAW, not rendered: `## The ` + "`code`" + ` bit` must keep its backticks so
// labels and slugs stay byte-identical to what the regex produced.
func headingText(h *ast.Heading, src []byte, lineAt lineIndex) (string, int, bool) {
	segs := h.Lines()
	if segs == nil || segs.Len() == 0 {
		return "", 0, false // a bare `#` — no text, no section
	}
	first := segs.At(0)
	var parts []string
	for i := 0; i < segs.Len(); i++ { // a setext heading may span lines
		s := segs.At(i)
		parts = append(parts, strings.TrimSpace(string(src[s.Start:s.Stop])))
	}
	title := strings.TrimSpace(strings.Join(parts, " "))
	if title == "" {
		return "", 0, false
	}
	return title, lineAt.of(first.Start), true
}

// blankFrontmatter overwrites a leading YAML (`---`) or TOML (`+++`) metadata
// block with spaces, IN PLACE, so every byte offset — and therefore every line
// number — is untouched while the region parses as blank lines.
//
// This exists because CommonMark is right and we want something else: a `---`
// line closing frontmatter directly follows text, which by the spec makes the
// whole block a setext H2. Measured on this repo: bundled/ctx-optimize/SKILL.md
// turned its entire frontmatter into ONE section whose label was the full
// description paragraph — a 2,000-character heading. Frontmatter is metadata,
// not prose, and the pre-goldmark regex never saw it; blanking keeps that true.
// A file whose FIRST line is a thematic break is left alone (no closing
// delimiter is found), so genuine `---` rules are unaffected.
func blankFrontmatter(src []byte) {
	if len(src) < 4 {
		return
	}
	var delim string
	switch {
	case bytes.HasPrefix(src, []byte("---\n")):
		delim = "---"
	case bytes.HasPrefix(src, []byte("+++\n")):
		delim = "+++"
	default:
		return
	}
	end := -1
	for off := 4; off < len(src); {
		nl := bytes.IndexByte(src[off:], '\n')
		lineEnd := len(src)
		if nl >= 0 {
			lineEnd = off + nl
		}
		switch strings.TrimRight(string(src[off:lineEnd]), " \t") {
		case delim, "...": // `...` also closes a YAML document
			end = lineEnd
		}
		if end >= 0 || nl < 0 {
			break
		}
		off = lineEnd + 1
	}
	if end < 0 {
		return // unterminated: not frontmatter, leave the document as written
	}
	for i := 0; i < end; i++ {
		if src[i] != '\n' {
			src[i] = ' '
		}
	}
}

func isCodeBlock(n ast.Node) bool {
	k := n.Kind()
	return k == ast.KindFencedCodeBlock || k == ast.KindCodeBlock
}

// lineIndex maps a byte offset to a 1-based line number in O(log n), so a file
// with many headings does not become O(bytes x headings).
type lineIndex []int

func newLineIndex(src []byte) lineIndex {
	var li lineIndex
	for i, c := range src {
		if c == '\n' {
			li = append(li, i)
		}
	}
	return li
}

func (li lineIndex) of(off int) int { return sort.SearchInts(li, off) + 1 }

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
