// Package export renders the graph for other tools: GraphML (yEd, Gephi,
// networkx), an Obsidian vault (wikilinked notes), and CSV pairs (spreadsheets,
// pandas, Neo4j LOAD CSV). Pure functions over the schema shapes — no store,
// no flags, no network. Inputs are copied and sorted so output is
// byte-deterministic regardless of caller ordering (house rule: everything a
// user might commit must diff cleanly).
package export

import (
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// sortedCopies returns deterministic working copies: nodes by ID, edges by
// (source, target, relation).
func sortedCopies(nodes []schema.Node, edges []schema.Edge) ([]schema.Node, []schema.Edge) {
	ns := append([]schema.Node(nil), nodes...)
	es := append([]schema.Edge(nil), edges...)
	sort.Slice(ns, func(i, j int) bool { return ns[i].ID < ns[j].ID })
	sort.Slice(es, func(i, j int) bool {
		if es[i].Source != es[j].Source {
			return es[i].Source < es[j].Source
		}
		if es[i].Target != es[j].Target {
			return es[i].Target < es[j].Target
		}
		return es[i].Relation < es[j].Relation
	})
	return ns, es
}

// ---- GraphML ----

type gmlData struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}

type gmlKey struct {
	ID   string `xml:"id,attr"`
	For  string `xml:"for,attr"`
	Name string `xml:"attr.name,attr"`
	Type string `xml:"attr.type,attr"`
}

type gmlNode struct {
	ID   string    `xml:"id,attr"`
	Data []gmlData `xml:"data"`
}

type gmlEdge struct {
	Source string    `xml:"source,attr"`
	Target string    `xml:"target,attr"`
	Data   []gmlData `xml:"data"`
}

type gmlGraph struct {
	ID          string    `xml:"id,attr"`
	EdgeDefault string    `xml:"edgedefault,attr"`
	Nodes       []gmlNode `xml:"node"`
	Edges       []gmlEdge `xml:"edge"`
}

type gmlDoc struct {
	XMLName xml.Name `xml:"graphml"`
	XMLNS   string   `xml:"xmlns,attr"`
	Keys    []gmlKey `xml:"key"`
	Graph   gmlGraph `xml:"graph"`
}

// GraphML writes the graph as a directed GraphML document. Node data keys:
// label, kind, file_type, source, location, producer; edge data keys:
// relation, confidence, weight. encoding/xml handles escaping; empty optional
// values (location, producer, zero weight) are omitted per-element.
func GraphML(w io.Writer, nodes []schema.Node, edges []schema.Edge) error {
	ns, es := sortedCopies(nodes, edges)
	doc := gmlDoc{
		XMLNS: "http://graphml.graphdrawing.org/xmlns",
		Keys: []gmlKey{
			{ID: "label", For: "node", Name: "label", Type: "string"},
			{ID: "kind", For: "node", Name: "kind", Type: "string"},
			{ID: "file_type", For: "node", Name: "file_type", Type: "string"},
			{ID: "source", For: "node", Name: "source", Type: "string"},
			{ID: "location", For: "node", Name: "location", Type: "string"},
			{ID: "producer", For: "node", Name: "producer", Type: "string"},
			{ID: "relation", For: "edge", Name: "relation", Type: "string"},
			{ID: "confidence", For: "edge", Name: "confidence", Type: "string"},
			{ID: "weight", For: "edge", Name: "weight", Type: "double"},
		},
		Graph: gmlGraph{ID: "ctxoptimize", EdgeDefault: "directed"},
	}
	for _, n := range ns {
		gn := gmlNode{ID: n.ID, Data: []gmlData{
			{Key: "label", Value: n.Label},
			{Key: "kind", Value: n.Kind},
			{Key: "file_type", Value: n.FileType},
			{Key: "source", Value: n.Source},
		}}
		if n.Location != "" {
			gn.Data = append(gn.Data, gmlData{Key: "location", Value: n.Location})
		}
		if p := n.Metadata["producer"]; p != "" {
			gn.Data = append(gn.Data, gmlData{Key: "producer", Value: p})
		}
		doc.Graph.Nodes = append(doc.Graph.Nodes, gn)
	}
	for _, e := range es {
		ge := gmlEdge{Source: e.Source, Target: e.Target, Data: []gmlData{
			{Key: "relation", Value: e.Relation},
			{Key: "confidence", Value: e.Confidence},
		}}
		if e.Weight != 0 {
			ge.Data = append(ge.Data, gmlData{Key: "weight", Value: strconv.FormatFloat(e.Weight, 'g', -1, 64)})
		}
		doc.Graph.Edges = append(doc.Graph.Edges, ge)
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

// ---- Obsidian ----

// Slug turns a label into an Obsidian-safe file stem: lowercase [a-z0-9-],
// runs of anything else collapse to one hyphen. Empty input slugs to "node".
func Slug(s string) string {
	var b strings.Builder
	prevHyphen := true // suppress a leading hyphen
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	out := strings.TrimSuffix(b.String(), "-")
	if out == "" {
		return "node"
	}
	return out
}

// yamlQuote renders s as a double-quoted YAML scalar (frontmatter-safe).
func yamlQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// Obsidian writes the graph as an Obsidian vault under dir: one note per
// non-module node (module nodes are external-import placeholders — noise as
// notes) plus an _index.md. Notes carry frontmatter (kind, source, location)
// and [[wikilink]] lists for outgoing/incoming relations; links only target
// neighbors that themselves have notes, so the vault has no dead links.
// Slugs collide → -2/-3 suffixes, assigned in node-ID order (deterministic).
// Returns the number of files written.
func Obsidian(dir string, nodes []schema.Node, edges []schema.Edge) (int, error) {
	ns, es := sortedCopies(nodes, edges)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}

	// Assign slugs in sorted-ID order so dedupe suffixes never depend on
	// input ordering.
	slugByID := make(map[string]string, len(ns))
	taken := make(map[string]bool, len(ns))
	noted := make([]schema.Node, 0, len(ns))
	for _, n := range ns {
		if n.Kind == "module" {
			continue
		}
		base := Slug(n.Label)
		slug := base
		for i := 2; taken[slug]; i++ {
			slug = fmt.Sprintf("%s-%d", base, i)
		}
		taken[slug] = true
		slugByID[n.ID] = slug
		noted = append(noted, n)
	}

	type link struct{ relation, slug string }
	outgoing := make(map[string][]link)
	incoming := make(map[string][]link)
	for _, e := range es {
		ss, sok := slugByID[e.Source]
		ts, tok := slugByID[e.Target]
		if !sok || !tok {
			continue
		}
		outgoing[e.Source] = append(outgoing[e.Source], link{e.Relation, ts})
		incoming[e.Target] = append(incoming[e.Target], link{e.Relation, ss})
	}

	files := 0
	for _, n := range noted {
		var b strings.Builder
		b.WriteString("---\n")
		fmt.Fprintf(&b, "kind: %s\n", yamlQuote(n.Kind))
		fmt.Fprintf(&b, "source: %s\n", yamlQuote(n.Source))
		if n.Location != "" {
			fmt.Fprintf(&b, "location: %s\n", yamlQuote(n.Location))
		}
		b.WriteString("---\n\n")
		fmt.Fprintf(&b, "# %s\n", n.Label)
		if out := outgoing[n.ID]; len(out) > 0 {
			b.WriteString("\n## Outgoing\n\n")
			for _, l := range out { // edge order is already deterministic
				fmt.Fprintf(&b, "- %s [[%s]]\n", l.relation, l.slug)
			}
		}
		if in := incoming[n.ID]; len(in) > 0 {
			b.WriteString("\n## Incoming\n\n")
			for _, l := range in {
				fmt.Fprintf(&b, "- [[%s]] %s\n", l.slug, l.relation)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, slugByID[n.ID]+".md"), []byte(b.String()), 0o644); err != nil {
			return files, err
		}
		files++
	}

	var idx strings.Builder
	idx.WriteString("# Index\n\n")
	fmt.Fprintf(&idx, "%d notes.\n\n", len(noted))
	for _, n := range noted {
		fmt.Fprintf(&idx, "- [[%s]] — %s (%s)\n", slugByID[n.ID], n.Label, n.Kind)
	}
	if err := os.WriteFile(filepath.Join(dir, "_index.md"), []byte(idx.String()), 0o644); err != nil {
		return files, err
	}
	files++
	return files, nil
}

// ---- CSV ----

// CSV writes nodes (id,label,kind,file_type,source,location,producer) to
// nodesW and edges (source,target,relation,confidence,weight) to edgesW,
// sorted. encoding/csv handles quoting.
func CSV(nodesW, edgesW io.Writer, nodes []schema.Node, edges []schema.Edge) error {
	ns, es := sortedCopies(nodes, edges)
	nw := csv.NewWriter(nodesW)
	if err := nw.Write([]string{"id", "label", "kind", "file_type", "source", "location", "producer"}); err != nil {
		return err
	}
	for _, n := range ns {
		if err := nw.Write([]string{n.ID, n.Label, n.Kind, n.FileType, n.Source, n.Location, n.Metadata["producer"]}); err != nil {
			return err
		}
	}
	nw.Flush()
	if err := nw.Error(); err != nil {
		return err
	}
	ew := csv.NewWriter(edgesW)
	if err := ew.Write([]string{"source", "target", "relation", "confidence", "weight"}); err != nil {
		return err
	}
	for _, e := range es {
		if err := ew.Write([]string{e.Source, e.Target, e.Relation, e.Confidence, strconv.FormatFloat(e.Weight, 'g', -1, 64)}); err != nil {
			return err
		}
	}
	ew.Flush()
	return ew.Error()
}
