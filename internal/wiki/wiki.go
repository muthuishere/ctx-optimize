// Package wiki renders the store's graph into an agent-crawlable markdown
// wiki — pages derived from nodes+edges ONLY, no LLM, no interpretation
// (graphify's wiki.py proved the shape works zero-model). The wiki OWNS the
// store's wiki/ dir: every run rewrites its pages and deletes stale .md files
// it didn't just write, and two runs over the same graph are byte-identical —
// everything is sorted, nothing depends on map order.
package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/analyze"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

const (
	topHubs     = 20 // hub pages: the top-N most-connected nodes
	relationCap = 30 // per-relation list cap on hub pages ("… N more")
)

// Generate renders index + hub + file pages into <store>/wiki and returns the
// number of pages written. Callers refresh the manifest afterwards.
func Generate(s *store.Store) (int, error) {
	nodes, err := s.Nodes()
	if err != nil {
		return 0, err
	}
	edges, err := s.Edges()
	if err != nil {
		return 0, err
	}
	dir := filepath.Join(s.Dir, "wiki")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}

	g := newGraph(nodes, edges)
	hubs := analyze.Hubs(nodes, edges, topHubs)

	// File pages: one per file node and per markdown document node.
	var fileNodes []schema.Node
	for _, n := range nodes {
		if n.Kind == "file" || n.Kind == "document" {
			fileNodes = append(fileNodes, n)
		}
	}
	sort.Slice(fileNodes, func(i, j int) bool { return fileNodes[i].ID < fileNodes[j].ID })

	// Page names: lowercase [a-z0-9-] slugs, duplicates deduped -2/-3 in a
	// deterministic order (files sorted by id, hubs in rank order).
	usedFile, usedHub := map[string]bool{}, map[string]bool{}
	filePage, hubPage := map[string]string{}, map[string]string{}
	for _, n := range fileNodes {
		filePage[n.ID] = uniqueName(usedFile, "file-", n.Label)
	}
	for _, h := range hubs {
		hubPage[h.Node.ID] = uniqueName(usedHub, "hub-", h.Node.Label)
	}
	// Canonical link target per node: the file page wins (it is the page
	// ABOUT the artifact); hubs without one link to their hub page.
	pageOf := map[string]string{}
	for id, p := range hubPage {
		pageOf[id] = p
	}
	for id, p := range filePage {
		pageOf[id] = p
	}

	written := map[string]bool{}
	write := func(name, body string) error {
		written[name] = true
		return writeAtomic(filepath.Join(dir, name), []byte(body))
	}

	if err := write("index.md", renderIndex(filepath.Base(s.Dir), g, nodes, edges, hubs, fileNodes, hubPage, filePage)); err != nil {
		return 0, err
	}
	for _, h := range hubs {
		name := hubPage[h.Node.ID]
		if err := write(name, renderHub(g, h, pageOf, name)); err != nil {
			return 0, err
		}
	}
	for _, n := range fileNodes {
		name := filePage[n.ID]
		if err := write(name, renderFile(g, n, pageOf, name)); err != nil {
			return 0, err
		}
	}

	// The wiki owns wiki/: .md pages from an earlier graph that this run
	// didn't re-emit are stale — remove them (and nothing else).
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || written[e.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return 0, err
		}
	}
	return len(written), nil
}

// ---- graph view ----

// graph indexes nodes and edges for rendering: node by id, and per-node
// outgoing/incoming neighbors grouped by relation, all lists sorted.
type graph struct {
	byID map[string]schema.Node
	out  map[string]map[string][]string // id → relation → target ids
	in   map[string]map[string][]string // id → relation → source ids
}

func newGraph(nodes []schema.Node, edges []schema.Edge) *graph {
	g := &graph{
		byID: make(map[string]schema.Node, len(nodes)),
		out:  map[string]map[string][]string{},
		in:   map[string]map[string][]string{},
	}
	for _, n := range nodes {
		g.byID[n.ID] = n
	}
	add := func(m map[string]map[string][]string, key, rel, id string) {
		if m[key] == nil {
			m[key] = map[string][]string{}
		}
		m[key][rel] = append(m[key][rel], id)
	}
	for _, e := range edges {
		add(g.out, e.Source, e.Relation, e.Target)
		add(g.in, e.Target, e.Relation, e.Source)
	}
	for _, m := range []map[string]map[string][]string{g.out, g.in} {
		for _, rels := range m {
			for r := range rels {
				sort.Strings(rels[r])
			}
		}
	}
	return g
}

// item renders one neighbor: a relative link where the neighbor has a page
// (never a self-link), otherwise its label — or the raw id for edge targets
// that resolve to no node (dangling references are shown, not hidden).
func (g *graph) item(id string, pageOf map[string]string, self string) string {
	label := id
	if n, ok := g.byID[id]; ok {
		label = n.Label
	}
	if p := pageOf[id]; p != "" && p != self {
		return fmt.Sprintf("[%s](%s)", label, p)
	}
	return "`" + label + "`"
}

// ---- pages ----

func renderIndex(module string, g *graph, nodes []schema.Node, edges []schema.Edge, hubs []analyze.Hub, fileNodes []schema.Node, hubPage, filePage map[string]string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", module)
	fmt.Fprintf(&sb, "Deterministic wiki generated from the graph (nodes + edges only). Regenerated on every `add`.\n\n")
	fmt.Fprintf(&sb, "- nodes: %d\n- edges: %d\n", len(nodes), len(edges))

	byKind := map[string]int{}
	for _, n := range nodes {
		byKind[n.Kind]++
	}
	sb.WriteString("\n## Nodes by kind\n\n| kind | count |\n|---|---:|\n")
	for _, k := range sortedKeys(byKind) {
		fmt.Fprintf(&sb, "| %s | %d |\n", cell(k), byKind[k])
	}

	byRel := map[string]int{}
	for _, e := range edges {
		byRel[e.Relation]++
	}
	sb.WriteString("\n## Edges by relation\n\n| relation | count |\n|---|---:|\n")
	for _, r := range sortedKeys(byRel) {
		fmt.Fprintf(&sb, "| %s | %d |\n", cell(r), byRel[r])
	}

	sb.WriteString("\n## Hubs\n\n")
	if len(hubs) == 0 {
		sb.WriteString("(none)\n")
	} else {
		fmt.Fprintf(&sb, "Top %d most-connected nodes.\n\n| page | kind | in | out | source |\n|---|---|---:|---:|---|\n", len(hubs))
		for _, h := range hubs {
			fmt.Fprintf(&sb, "| [%s](%s) | %s | %d | %d | %s |\n",
				cell(h.Node.Label), hubPage[h.Node.ID], cell(h.Node.Kind), h.In, h.Out, cell(h.Node.Source))
		}
	}

	sb.WriteString("\n## Files & documents\n\n")
	if len(fileNodes) == 0 {
		sb.WriteString("(none)\n")
	} else {
		sb.WriteString("| page | kind | contains | source |\n|---|---|---:|---|\n")
		for _, n := range fileNodes {
			fmt.Fprintf(&sb, "| [%s](%s) | %s | %d | %s |\n",
				cell(n.Label), filePage[n.ID], cell(n.Kind), len(g.out[n.ID]["contains"]), cell(n.Source))
		}
	}
	return sb.String()
}

func renderHub(g *graph, h analyze.Hub, pageOf map[string]string, self string) string {
	var sb strings.Builder
	writeFacts(&sb, h.Node)
	fmt.Fprintf(&sb, "- degree: %d in / %d out\n", h.In, h.Out)
	writeRelations(&sb, "Outgoing", g.out[h.Node.ID], g, pageOf, self)
	writeRelations(&sb, "Incoming", g.in[h.Node.ID], g, pageOf, self)
	sb.WriteString("\n[index](index.md)\n")
	return sb.String()
}

func renderFile(g *graph, n schema.Node, pageOf map[string]string, self string) string {
	var sb strings.Builder
	writeFacts(&sb, n)

	if decls := g.out[n.ID]["contains"]; len(decls) > 0 {
		fmt.Fprintf(&sb, "\n## Contains (%d)\n\n", len(decls))
		for _, id := range decls {
			line := "- " + g.item(id, pageOf, self)
			if d, ok := g.byID[id]; ok {
				line += fmt.Sprintf(" (%s)", d.Kind)
				if d.Location != "" {
					line += " " + d.Location
				}
			}
			sb.WriteString(line + "\n")
		}
	}
	if imports := g.out[n.ID]["imports"]; len(imports) > 0 {
		fmt.Fprintf(&sb, "\n## Imports (%d)\n\n", len(imports))
		for _, id := range imports {
			fmt.Fprintf(&sb, "- %s\n", g.item(id, pageOf, self))
		}
	}
	writeRelations(&sb, "Referenced by", g.in[n.ID], g, pageOf, self)
	sb.WriteString("\n[index](index.md)\n")
	return sb.String()
}

// writeFacts is the shared page header: title + the node's plain facts.
func writeFacts(sb *strings.Builder, n schema.Node) {
	fmt.Fprintf(sb, "# %s\n\n", n.Label)
	fmt.Fprintf(sb, "- id: `%s`\n- kind: %s (%s)\n- source: %s\n", n.ID, n.Kind, n.FileType, n.Source)
	if n.Location != "" {
		fmt.Fprintf(sb, "- location: %s\n", n.Location)
	}
	if p := n.Metadata["producer"]; p != "" {
		fmt.Fprintf(sb, "- producer: %s\n", p)
	}
}

// writeRelations renders one direction's edges grouped by relation, each list
// capped at relationCap with an explicit "… N more".
func writeRelations(sb *strings.Builder, title string, rels map[string][]string, g *graph, pageOf map[string]string, self string) {
	if len(rels) == 0 {
		return
	}
	fmt.Fprintf(sb, "\n## %s\n", title)
	for _, r := range sortedRelKeys(rels) {
		ids := rels[r]
		fmt.Fprintf(sb, "\n### %s (%d)\n\n", r, len(ids))
		for i, id := range ids {
			if i == relationCap {
				fmt.Fprintf(sb, "- … %d more\n", len(ids)-relationCap)
				break
			}
			fmt.Fprintf(sb, "- %s\n", g.item(id, pageOf, self))
		}
	}
}

// ---- helpers ----

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	out := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if out == "" {
		return "node"
	}
	return out
}

// uniqueName turns a label into "<prefix><slug>.md", deduping repeats with
// -2/-3… — callers assign in a deterministic order so names are stable.
func uniqueName(used map[string]bool, prefix, label string) string {
	s := slugify(label)
	name := prefix + s + ".md"
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s%s-%d.md", prefix, s, i)
	}
	used[name] = true
	return name
}

// cell escapes a value for a markdown table cell.
func cell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedRelKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path) // readers never see a half-written page
}
