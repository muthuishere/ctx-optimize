// Package code is the tier-1 code producer: tree-sitter grammars compiled to
// WASI, hosted by wazero (pure Go, single binary), fanned across worker
// goroutines — the graphify-speed requirement is carried by parallelism.
//
// Per file it emits: a file node, one node per declaration (functions,
// methods, classes/structs/interfaces/enums/traits/types) with qualified
// labels (Class.method) and L#-L# locations, contains edges (file→decl,
// decl→nested decl), and import edges (file→module). Call sites resolve
// module-wide by name AFTER all files parse: a unique match becomes an
// INFERRED call edge; ambiguous names are dropped, not guessed — the same
// honesty graphify applies.
package code

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

const ProducerName = "code"

// maxFileBytes skips generated monsters (bundles, lock outputs).
const maxFileBytes = 2 << 20

type fileResult struct {
	nodes []schema.Node
	edges []schema.Edge
	calls []callSite
	decls []declRef
	err   error
	path  string
}

type callSite struct {
	callerID string // innermost enclosing decl (or file) id
	callee   string // callee name as written
	file     string
}

type declRef struct {
	id    string
	label string // unqualified name
	file  string
}

// Extract parses every recognized code file under root.
func Extract(root string) (*schema.Batch, error) {
	ctx := context.Background()
	eng, err := NewEngine(ctx)
	if err != nil {
		return nil, err
	}
	defer eng.Close(ctx)

	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" ||
				name == "vendor" || name == "target" || name == "dist" || name == "build" ||
				strings.HasSuffix(name, "-out")) {
				return filepath.SkipDir
			}
			return nil
		}
		if LangForFile(name) == nil {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Size() > maxFileBytes {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files) // deterministic output regardless of walk order

	// Symbol tables once per language (shared, read-only after this).
	symTab := map[int][]string{}
	{
		inst, err := eng.NewInstance(ctx)
		if err != nil {
			return nil, err
		}
		for _, l := range Languages {
			names, err := inst.Symbols(ctx, l.ID)
			if err != nil {
				inst.Close(ctx)
				return nil, fmt.Errorf("symbols %s: %w", l.Name, err)
			}
			symTab[l.ID] = names
		}
		inst.Close(ctx)
	}

	workers := runtime.NumCPU() - 1
	if workers < 1 {
		workers = 1
	}
	if workers > len(files) {
		workers = len(files)
	}
	jobs := make(chan string)
	results := make(chan fileResult, 64)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inst, err := eng.NewInstance(ctx)
			if err != nil {
				for path := range jobs {
					results <- fileResult{path: path, err: err}
				}
				return
			}
			defer inst.Close(ctx)
			for path := range jobs {
				res := extractFile(ctx, inst, symTab, root, path)
				results <- res
			}
		}()
	}
	go func() {
		for _, f := range files {
			jobs <- f
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	batch := &schema.Batch{Producer: ProducerName}
	var calls []callSite
	var decls []declRef
	for res := range results {
		if res.err != nil {
			// One unparseable file must not kill the gather — skip loudly.
			fmt.Fprintf(os.Stderr, "ctx-optimize: skip %s: %v\n", res.path, res.err)
			continue
		}
		batch.Nodes = append(batch.Nodes, res.nodes...)
		batch.Edges = append(batch.Edges, res.edges...)
		calls = append(calls, res.calls...)
		decls = append(decls, res.decls...)
	}

	// Module-wide call resolution: unique name → INFERRED edge; ambiguous →
	// dropped. Self-calls and unknown names are dropped too.
	byName := map[string][]declRef{}
	for _, d := range decls {
		byName[d.label] = append(byName[d.label], d)
	}
	seen := map[string]bool{}
	for _, c := range calls {
		targets := byName[c.callee]
		if len(targets) != 1 || targets[0].id == c.callerID {
			continue
		}
		key := c.callerID + "\x00" + targets[0].id
		if seen[key] {
			continue
		}
		seen[key] = true
		batch.Edges = append(batch.Edges, schema.Edge{
			Source: c.callerID, Target: targets[0].id,
			Relation: "calls", Confidence: "INFERRED", Weight: 1,
		})
	}
	sortBatch(batch)
	return batch, nil
}

func extractFile(ctx context.Context, inst *Instance, symTab map[int][]string, root, path string) fileResult {
	res := fileResult{path: path}
	lang := LangForFile(filepath.Base(path))
	src, err := os.ReadFile(path)
	if err != nil {
		res.err = err
		return res
	}
	raw, err := inst.Parse(ctx, lang.ID, src)
	if err != nil {
		res.err = err
		return res
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)
	syms := symTab[lang.ID]
	typeOf := func(n RawNode) string {
		if int(n.Symbol) < len(syms) {
			return syms[n.Symbol]
		}
		return ""
	}
	text := func(n RawNode) string {
		if n.Start < n.End && int(n.End) <= len(src) {
			return string(src[n.Start:n.End])
		}
		return ""
	}

	fileID := rel
	res.nodes = append(res.nodes, schema.Node{
		ID: fileID, Label: filepath.Base(rel), Kind: "file", FileType: "code",
		Source: rel, Metadata: map[string]string{"lang": lang.Name},
	})

	// nameFor finds the declaration's identifier: first Names-typed node in
	// the subtree within 3 levels, before anything else claims it.
	nameFor := func(i int) (string, bool) {
		d := raw[i].Depth
		for j := i + 1; j < len(raw) && raw[j].Depth > d; j++ {
			if raw[j].Depth > d+3 {
				continue
			}
			if lang.Names[typeOf(raw[j])] {
				return text(raw[j]), true
			}
		}
		return "", false
	}

	type openDecl struct {
		id    string
		depth uint32
	}
	var stack []openDecl
	callerAt := func() string {
		if len(stack) == 0 {
			return fileID
		}
		return stack[len(stack)-1].id
	}

	for i := 0; i < len(raw); i++ {
		n := raw[i]
		if !n.Named {
			continue
		}
		for len(stack) > 0 && n.Depth <= stack[len(stack)-1].depth {
			stack = stack[:len(stack)-1]
		}
		t := typeOf(n)

		if kind, ok := lang.Decls[t]; ok {
			name, found := nameFor(i)
			if !found || name == "" {
				continue
			}
			qual := name
			if len(stack) > 0 {
				parentID := stack[len(stack)-1].id
				if idx := strings.LastIndex(parentID, "::"); idx >= 0 {
					qual = parentID[idx+2:] + "." + name
				}
			}
			id := rel + "::" + qual
			parent := callerAt()
			res.nodes = append(res.nodes, schema.Node{
				ID: id, Label: qual, Kind: kind, FileType: "code", Source: rel,
				Location: fmt.Sprintf("L%d-L%d", n.StartRow+1, n.EndRow+1),
				Metadata: map[string]string{"lang": lang.Name},
			})
			res.edges = append(res.edges, schema.Edge{
				Source: parent, Target: id, Relation: "contains",
				Confidence: "EXTRACTED", Weight: 1,
			})
			res.decls = append(res.decls, declRef{id: id, label: name, file: rel})
			stack = append(stack, openDecl{id: id, depth: n.Depth})
			continue
		}

		if lang.Calls[t] {
			if callee, ok := nameFor(i); ok && callee != "" {
				res.calls = append(res.calls, callSite{callerID: callerAt(), callee: callee, file: rel})
			}
			continue
		}

		if lang.Imports[t] {
			target := importTarget(raw, i, typeOf, text)
			if target == "" {
				continue
			}
			modID := "module://" + target
			res.nodes = append(res.nodes, schema.Node{
				ID: modID, Label: target, Kind: "module", FileType: "code", Source: modID,
			})
			res.edges = append(res.edges, schema.Edge{
				Source: fileID, Target: modID, Relation: "imports",
				Confidence: "EXTRACTED", Weight: 1,
			})
		}
	}
	return res
}

// importTarget extracts what an import statement points at: the last named
// child's text, unquoted — good enough across all ten grammars ("fmt",
// 'react', <stdio.h>, java.util.List, crate::foo::Bar).
func importTarget(raw []RawNode, i int, typeOf func(RawNode) string, text func(RawNode) string) string {
	d := raw[i].Depth
	last := -1
	for j := i + 1; j < len(raw) && raw[j].Depth > d; j++ {
		if raw[j].Depth == d+1 && raw[j].Named {
			last = j
		}
	}
	if last < 0 {
		return ""
	}
	t := strings.TrimSpace(text(raw[last]))
	t = strings.Trim(t, `"'`)
	t = strings.TrimPrefix(t, "<")
	t = strings.TrimSuffix(t, ">")
	if len(t) > 120 { // a use-tree forest is not a module name
		t = t[:120]
	}
	return t
}

func sortBatch(b *schema.Batch) {
	sort.Slice(b.Nodes, func(i, j int) bool { return b.Nodes[i].ID < b.Nodes[j].ID })
	sort.Slice(b.Edges, func(i, j int) bool {
		a, c := b.Edges[i], b.Edges[j]
		if a.Source != c.Source {
			return a.Source < c.Source
		}
		if a.Target != c.Target {
			return a.Target < c.Target
		}
		return a.Relation < c.Relation
	})
	// Duplicate module nodes across files collapse here (same ID).
	out := b.Nodes[:0]
	for i, n := range b.Nodes {
		if i > 0 && n.ID == b.Nodes[i-1].ID {
			continue
		}
		out = append(out, n)
	}
	b.Nodes = out
}
