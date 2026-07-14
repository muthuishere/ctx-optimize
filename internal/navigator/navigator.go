// Package navigator writes the multi-module root's first-class artifact: a
// small module index instead of a giant merged graph. modules.json is the
// machine map (pre-expanded paths → O(1) cwd resolution, hub symbols → owner
// lookup); navigator.md is the same content for humans/agents and doubles as
// the unified wiki front page. Deterministic text derived from the stores —
// regenerated on every multi-module add, never inferred.
package navigator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/analyze"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

const hubCap = 100 // symbols per module in the directory — keeps the file KBs

// ModuleEntry is one module's card in the index.
type ModuleEntry struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`  // repo-relative, pre-expanded (never a glob)
	Store   string   `json:"store"` // store key ("<root>/<path>")
	Nodes   int      `json:"nodes"`
	Edges   int      `json:"edges"`
	Hubs    []string `json:"hubs,omitempty"` // top labels by degree
	Summary string   `json:"summary,omitempty"`
}

type Index struct {
	Version int           `json:"version"`
	Root    string        `json:"root"` // root store key
	Modules []ModuleEntry `json:"modules"`
}

// Build assembles the index by reading each module store. repoRoot is the
// repo path (for README summaries); storeRoot/rootKey locate the stores.
func Build(repoRoot, storeRoot, rootKey string, modules []ModuleEntry) (*Index, error) {
	idx := &Index{Version: 1, Root: rootKey}
	for _, m := range modules {
		s, err := store.Open(storeRoot, m.Store)
		if err != nil {
			return nil, err
		}
		nodes, err := s.Nodes()
		if err != nil {
			return nil, err
		}
		edges, err := s.Edges()
		if err != nil {
			return nil, err
		}
		m.Nodes, m.Edges = len(nodes), len(edges)
		m.Hubs = hubLabels(nodes, edges)
		m.Summary = readmeSummary(filepath.Join(repoRoot, filepath.FromSlash(m.Path)))
		if m.Summary == "" {
			// No README to summarize → derive the about line from the graph:
			// the top community label ("store.go (internal/store)"). Uses the
			// nodes+edges already loaded above — no extra store reads.
			if cs := analyze.Communities(nodes, edges); len(cs) > 0 {
				m.Summary = cs[0].Label
			}
		}
		idx.Modules = append(idx.Modules, m)
	}
	sort.Slice(idx.Modules, func(i, j int) bool { return idx.Modules[i].Path < idx.Modules[j].Path })
	return idx, nil
}

// Write persists modules.json + navigator.md into the root store dir and
// mirrors navigator.md as the unified wiki front page.
func (idx *Index) Write(rootStoreDir string) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(rootStoreDir, "modules.json"), append(data, '\n')); err != nil {
		return err
	}
	md := idx.render()
	if err := atomicWrite(filepath.Join(rootStoreDir, "navigator.md"), []byte(md)); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(rootStoreDir, "wiki"), 0o755); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(rootStoreDir, "wiki", "index.md"), []byte(md))
}

// Load reads modules.json from a root store dir (absent → nil, no error:
// single-module stores have no navigator).
func Load(rootStoreDir string) (*Index, error) {
	data, err := os.ReadFile(filepath.Join(rootStoreDir, "modules.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse modules.json: %w", err)
	}
	return &idx, nil
}

// ModuleFor resolves a repo-relative path (slash-form) to the module whose
// Path is its longest prefix — O(modules), pre-expanded, no globbing.
func (idx *Index) ModuleFor(rel string) *ModuleEntry {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	var best *ModuleEntry
	for i := range idx.Modules {
		p := idx.Modules[i].Path
		if rel == p || strings.HasPrefix(rel, p+"/") {
			if best == nil || len(p) > len(best.Path) {
				best = &idx.Modules[i]
			}
		}
	}
	return best
}

// OwnerOf finds modules whose hub directory contains label (exact match).
func (idx *Index) OwnerOf(label string) []ModuleEntry {
	var out []ModuleEntry
	for _, m := range idx.Modules {
		for _, h := range m.Hubs {
			if h == label || strings.HasSuffix(h, "."+label) || strings.HasSuffix(h, "::"+label) {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

// Rank orders modules by lexical match of query terms against name, path,
// summary and hubs. Deterministic: score desc, then path asc.
func (idx *Index) Rank(terms []string) []ModuleEntry {
	type scored struct {
		m ModuleEntry
		s int
	}
	var list []scored
	for _, m := range idx.Modules {
		hay := strings.ToLower(m.Name + " " + m.Path + " " + m.Summary + " " + strings.Join(m.Hubs, " "))
		s := 0
		for _, t := range terms {
			t = strings.ToLower(t)
			if t == "" {
				continue
			}
			s += strings.Count(hay, t)
		}
		list = append(list, scored{m, s})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].s != list[j].s {
			return list[i].s > list[j].s
		}
		return list[i].m.Path < list[j].m.Path
	})
	out := make([]ModuleEntry, len(list))
	for i, e := range list {
		out[i] = e.m
	}
	return out
}

func (idx *Index) render() string {
	var sb strings.Builder
	sb.WriteString("# " + idx.Root + " — module navigator\n\n")
	sb.WriteString(fmt.Sprintf("%d modules. Query from a module dir answers from that module; from the root it federates via this index.\n\n", len(idx.Modules)))
	sb.WriteString("| module | path | nodes | edges | about |\n|---|---|---:|---:|---|\n")
	for _, m := range idx.Modules {
		sb.WriteString(fmt.Sprintf("| %s | %s | %d | %d | %s |\n", m.Name, m.Path, m.Nodes, m.Edges, m.Summary))
	}
	sb.WriteString("\n")
	for _, m := range idx.Modules {
		if len(m.Hubs) == 0 {
			continue
		}
		top := m.Hubs
		if len(top) > 10 {
			top = top[:10]
		}
		sb.WriteString(fmt.Sprintf("## %s\nhubs: %s\nwiki: [%s](../%s/wiki/index.md)\n\n",
			m.Name, strings.Join(top, ", "), m.Path, m.Path))
	}
	return sb.String()
}

func hubLabels(nodes []schema.Node, edges []schema.Edge) []string {
	hubs := analyze.Hubs(nodes, edges, hubCap)
	out := make([]string, 0, len(hubs))
	for _, h := range hubs {
		out = append(out, h.Node.Label)
	}
	return out
}

// readmeSummary pulls the first markdown heading of the module's README (or
// the first plain line when there is none) — the one-line "about". License
// boilerplate and comment blocks are skipped, not summarized.
func readmeSummary(dir string) string {
	for _, name := range []string{"README.md", "readme.md", "README.txt"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		fallback := ""
		inComment := false
		for _, raw := range lines {
			line := strings.TrimSpace(raw)
			if strings.HasPrefix(line, "<!--") {
				inComment = !strings.Contains(line, "-->")
				continue
			}
			if inComment {
				inComment = !strings.Contains(line, "-->")
				continue
			}
			if strings.HasPrefix(line, "#") {
				if h := strings.TrimSpace(strings.TrimLeft(line, "#")); h != "" {
					return clip(h)
				}
				continue
			}
			if fallback == "" && line != "" && !strings.HasPrefix(line, "[!") &&
				!strings.HasPrefix(line, "Licensed to") && !strings.HasPrefix(line, "license") {
				fallback = line
			}
		}
		return clip(fallback)
	}
	return ""
}

func clip(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return s
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
