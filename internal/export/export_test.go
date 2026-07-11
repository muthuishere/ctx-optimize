package export

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// fixture returns a small graph in deliberately unsorted order, with a slug
// collision (two "Parse()" labels) and a module node.
func fixture() ([]schema.Node, []schema.Edge) {
	nodes := []schema.Node{
		{ID: "z.go::Parse", Label: "Parse()", Kind: "function", FileType: "code", Source: "z.go", Location: "L10-L20", Metadata: map[string]string{"producer": "code"}},
		{ID: "a.go::Parse", Label: "Parse()", Kind: "function", FileType: "code", Source: "a.go", Location: "L1-L9", Metadata: map[string]string{"producer": "code"}},
		{ID: "module://fmt", Label: "fmt", Kind: "module", FileType: "code", Source: "module://fmt"},
		{ID: "docs/readme.md", Label: "readme.md", Kind: "document", FileType: "document", Source: "docs/readme.md", Metadata: map[string]string{"producer": "markdown"}},
	}
	edges := []schema.Edge{
		{Source: "z.go::Parse", Target: "a.go::Parse", Relation: "calls", Confidence: schema.Inferred, Weight: 2},
		{Source: "a.go::Parse", Target: "module://fmt", Relation: "imports", Confidence: schema.Extracted},
		{Source: "docs/readme.md", Target: "a.go::Parse", Relation: "references", Confidence: schema.Extracted},
	}
	return nodes, edges
}

// reversed returns the same graph in the opposite input order (determinism
// tests feed both orders and expect identical bytes).
func reversed() ([]schema.Node, []schema.Edge) {
	nodes, edges := fixture()
	for i, j := 0, len(nodes)-1; i < j; i, j = i+1, j-1 {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	}
	for i, j := 0, len(edges)-1; i < j; i, j = i+1, j-1 {
		edges[i], edges[j] = edges[j], edges[i]
	}
	return nodes, edges
}

func TestGraphMLRoundTrip(t *testing.T) {
	nodes, edges := fixture()
	var buf bytes.Buffer
	if err := GraphML(&buf, nodes, edges); err != nil {
		t.Fatal(err)
	}

	var doc struct {
		XMLName xml.Name `xml:"graphml"`
		Graph   struct {
			EdgeDefault string `xml:"edgedefault,attr"`
			Nodes       []struct {
				ID   string `xml:"id,attr"`
				Data []struct {
					Key   string `xml:"key,attr"`
					Value string `xml:",chardata"`
				} `xml:"data"`
			} `xml:"node"`
			Edges []struct {
				Source string `xml:"source,attr"`
				Target string `xml:"target,attr"`
				Data   []struct {
					Key   string `xml:"key,attr"`
					Value string `xml:",chardata"`
				} `xml:"data"`
			} `xml:"edge"`
		} `xml:"graph"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("GraphML does not parse back: %v\n%s", err, buf.String())
	}
	if doc.XMLName.Space != "http://graphml.graphdrawing.org/xmlns" {
		t.Fatalf("xmlns: %q", doc.XMLName.Space)
	}
	if doc.Graph.EdgeDefault != "directed" {
		t.Fatalf("edgedefault: %q", doc.Graph.EdgeDefault)
	}
	if len(doc.Graph.Nodes) != len(nodes) || len(doc.Graph.Edges) != len(edges) {
		t.Fatalf("got %d nodes, %d edges", len(doc.Graph.Nodes), len(doc.Graph.Edges))
	}
	// Nodes sorted by ID; first is a.go::Parse with full data.
	first := doc.Graph.Nodes[0]
	if first.ID != "a.go::Parse" {
		t.Fatalf("nodes not sorted: first = %s", first.ID)
	}
	data := map[string]string{}
	for _, d := range first.Data {
		data[d.Key] = d.Value
	}
	want := map[string]string{"label": "Parse()", "kind": "function", "file_type": "code", "source": "a.go", "location": "L1-L9", "producer": "code"}
	for k, v := range want {
		if data[k] != v {
			t.Fatalf("node data %s = %q, want %q", k, data[k], v)
		}
	}
	// Edge data carries relation/confidence/weight.
	var sawWeighted bool
	for _, e := range doc.Graph.Edges {
		ed := map[string]string{}
		for _, d := range e.Data {
			ed[d.Key] = d.Value
		}
		if ed["relation"] == "" || ed["confidence"] == "" {
			t.Fatalf("edge %s->%s missing relation/confidence", e.Source, e.Target)
		}
		if e.Source == "z.go::Parse" && ed["weight"] == "2" {
			sawWeighted = true
		}
	}
	if !sawWeighted {
		t.Fatal("weighted edge lost its weight")
	}
}

func TestObsidianVault(t *testing.T) {
	nodes, edges := fixture()
	dir := t.TempDir()
	files, err := Obsidian(dir, nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	// 3 non-module notes + _index.md; the module node gets no note.
	if files != 4 {
		t.Fatalf("files = %d, want 4", files)
	}
	// Slug collision: a.go::Parse (lower ID) keeps parse, z.go::Parse gets parse-2.
	a, err := os.ReadFile(filepath.Join(dir, "parse.md"))
	if err != nil {
		t.Fatal(err)
	}
	z, err := os.ReadFile(filepath.Join(dir, "parse-2.md"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "fmt.md")); !os.IsNotExist(err) {
		t.Fatal("module node got a note")
	}
	if !strings.Contains(string(a), "source: \"a.go\"") || !strings.Contains(string(a), "kind: \"function\"") {
		t.Fatalf("parse.md frontmatter:\n%s", a)
	}
	// z calls a → outgoing wikilink in parse-2.md, incoming in parse.md.
	if !strings.Contains(string(z), "- calls [[parse]]") {
		t.Fatalf("parse-2.md missing outgoing link:\n%s", z)
	}
	if !strings.Contains(string(a), "- [[parse-2]] calls") || !strings.Contains(string(a), "- [[readme-md]] references") {
		t.Fatalf("parse.md missing incoming links:\n%s", a)
	}
	// Edges into module nodes produce no dead links.
	if strings.Contains(string(a), "[[fmt]]") {
		t.Fatalf("dead link to module node:\n%s", a)
	}
	idx, err := os.ReadFile(filepath.Join(dir, "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"[[parse]]", "[[parse-2]]", "[[readme-md]]"} {
		if !strings.Contains(string(idx), slug) {
			t.Fatalf("index missing %s:\n%s", slug, idx)
		}
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Parse()":        "parse",
		"HTTP/2 Server":  "http-2-server",
		"--weird--":      "weird",
		"":               "node",
		"日本語":            "node",
		"readme.md":      "readme-md",
		"Already-Good-1": "already-good-1",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCSV(t *testing.T) {
	nodes, edges := fixture()
	var nb, eb bytes.Buffer
	if err := CSV(&nb, &eb, nodes, edges); err != nil {
		t.Fatal(err)
	}
	nLines := strings.Split(strings.TrimSpace(nb.String()), "\n")
	eLines := strings.Split(strings.TrimSpace(eb.String()), "\n")
	if nLines[0] != "id,label,kind,file_type,source,location,producer" {
		t.Fatalf("nodes header: %s", nLines[0])
	}
	if eLines[0] != "source,target,relation,confidence,weight" {
		t.Fatalf("edges header: %s", eLines[0])
	}
	if len(nLines) != len(nodes)+1 || len(eLines) != len(edges)+1 {
		t.Fatalf("rows: %d nodes, %d edges", len(nLines)-1, len(eLines)-1)
	}
	if !strings.HasPrefix(nLines[1], "a.go::Parse,") {
		t.Fatalf("nodes not sorted: %s", nLines[1])
	}
}

func TestDeterminism(t *testing.T) {
	n1, e1 := fixture()
	n2, e2 := reversed()

	var g1, g2 bytes.Buffer
	if err := GraphML(&g1, n1, e1); err != nil {
		t.Fatal(err)
	}
	if err := GraphML(&g2, n2, e2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g1.Bytes(), g2.Bytes()) {
		t.Fatal("GraphML output depends on input order")
	}

	var n1b, e1b, n2b, e2b bytes.Buffer
	if err := CSV(&n1b, &e1b, n1, e1); err != nil {
		t.Fatal(err)
	}
	if err := CSV(&n2b, &e2b, n2, e2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(n1b.Bytes(), n2b.Bytes()) || !bytes.Equal(e1b.Bytes(), e2b.Bytes()) {
		t.Fatal("CSV output depends on input order")
	}

	d1, d2 := t.TempDir(), t.TempDir()
	if _, err := Obsidian(d1, n1, e1); err != nil {
		t.Fatal(err)
	}
	if _, err := Obsidian(d2, n2, e2); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(d1)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range ents {
		b1, err := os.ReadFile(filepath.Join(d1, ent.Name()))
		if err != nil {
			t.Fatal(err)
		}
		b2, err := os.ReadFile(filepath.Join(d2, ent.Name()))
		if err != nil {
			t.Fatalf("vaults diverge: %s only in one: %v", ent.Name(), err)
		}
		if !bytes.Equal(b1, b2) {
			t.Fatalf("obsidian note %s depends on input order", ent.Name())
		}
	}
}
