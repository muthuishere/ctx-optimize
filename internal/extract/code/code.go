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

	"github.com/muthuishere/ctx-optimize/internal/extract/ignore"
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

// resolved routes a file to its language and the engine that parses it.
type resolved struct {
	lang      *Lang
	engineKey string // "" = embedded bundle; else the pack's wasm path
}

// Extract parses every recognized code file under root — embedded languages
// plus any grammar packs (see langs.go LoadPacks).
func Extract(root string) (*schema.Batch, error) { return ExtractExcluding(root, nil) }

// ExtractExcluding is Extract with subtrees pruned — the multi-module root
// residual: module dirs (absolute paths) are gathered into their own stores
// and must not re-enter the parent's batch.
func ExtractExcluding(root string, exclude []string) (*schema.Batch, error) {
	ctx := context.Background()
	skip := map[string]bool{}
	for _, e := range exclude {
		if abs, err := filepath.Abs(e); err == nil {
			skip[abs] = true
		}
	}

	packs, err := LoadPacks(root)
	if err != nil {
		return nil, err
	}
	// A pack extension beats the embedded set — users can override built-ins.
	packByExt := map[string]*Pack{}
	for i := range packs {
		for _, ext := range packs[i].Lang.Exts {
			packByExt[strings.ToLower(ext)] = &packs[i]
		}
	}
	resolve := func(name string) *resolved {
		lower := strings.ToLower(name)
		for ext, p := range packByExt {
			if strings.HasSuffix(lower, ext) {
				return &resolved{lang: &p.Lang, engineKey: p.WasmPath}
			}
		}
		if l := LangForFile(name); l != nil {
			return &resolved{lang: l}
		}
		return nil
	}

	engines := map[string]*Engine{}
	var engMu sync.Mutex
	defer func() {
		for _, e := range engines {
			e.Close(ctx)
		}
	}()
	getEngine := func(key string) (*Engine, error) {
		engMu.Lock()
		defer engMu.Unlock()
		if e, ok := engines[key]; ok {
			return e, nil
		}
		var e *Engine
		var err error
		if key == "" {
			e, err = NewEngine(ctx)
		} else {
			data, rerr := os.ReadFile(key)
			if rerr != nil {
				return nil, rerr
			}
			e, err = NewEngineFromBytes(ctx, data)
		}
		if err != nil {
			return nil, fmt.Errorf("engine %s: %w", key, err)
		}
		engines[key] = e
		return e, nil
	}

	ignored := ignore.New(root) // .gitignore semantics via git itself; nil = no git
	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if ignored != nil {
			if rel, rerr := filepath.Rel(root, path); rerr == nil && rel != "." && ignored(filepath.ToSlash(rel)) {
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
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" ||
				name == "vendor" || name == "target" || name == "dist" || name == "build" ||
				strings.HasSuffix(name, "-out")) {
				return filepath.SkipDir
			}
			return nil
		}
		if resolve(name) == nil {
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

	// Symbol tables once per engine+language (read-only after this).
	symTab := map[string]map[int][]string{}
	loadSyms := func(key string, langs []Lang) error {
		eng, err := getEngine(key)
		if err != nil {
			return err
		}
		inst, err := eng.NewInstance(ctx)
		if err != nil {
			return err
		}
		defer inst.Close(ctx)
		m := map[int][]string{}
		for _, l := range langs {
			names, err := inst.Symbols(ctx, l.ID)
			if err != nil {
				return fmt.Errorf("symbols %s: %w", l.Name, err)
			}
			m[l.ID] = names
		}
		symTab[key] = m
		return nil
	}
	if len(files) > 0 {
		if err := loadSyms("", Languages); err != nil {
			return nil, err
		}
	}
	for i := range packs {
		if err := loadSyms(packs[i].WasmPath, []Lang{packs[i].Lang}); err != nil {
			return nil, err
		}
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
			instances := map[string]*Instance{} // engineKey → this worker's instance
			defer func() {
				for _, inst := range instances {
					inst.Close(ctx)
				}
			}()
			for path := range jobs {
				r := resolve(filepath.Base(path))
				inst, ok := instances[r.engineKey]
				if !ok {
					eng, err := getEngine(r.engineKey)
					if err != nil {
						results <- fileResult{path: path, err: err}
						continue
					}
					inst, err = eng.NewInstance(ctx)
					if err != nil {
						results <- fileResult{path: path, err: err}
						continue
					}
					instances[r.engineKey] = inst
				}
				results <- extractFile(ctx, inst, r.lang, symTab[r.engineKey], root, path)
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

	// Call resolution: same-FILE unique match wins (self.audit resolves in
	// its own file even when the name repeats elsewhere), else module-wide
	// unique. Ambiguous and unknown names are dropped, never guessed.
	byName := map[string][]declRef{}
	for _, d := range decls {
		byName[d.label] = append(byName[d.label], d)
	}
	pick := func(c callSite) *declRef {
		cands := byName[c.callee]
		var inFile []*declRef
		for k := range cands {
			if cands[k].file == c.file {
				inFile = append(inFile, &cands[k])
			}
		}
		if len(inFile) == 1 {
			return inFile[0]
		}
		if len(inFile) == 0 && len(cands) == 1 {
			return &cands[0]
		}
		return nil
	}
	seen := map[string]bool{}
	for _, c := range calls {
		t := pick(c)
		if t == nil || t.id == c.callerID {
			continue
		}
		targets := []declRef{*t}
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

func extractFile(ctx context.Context, inst *Instance, lang *Lang, symTab map[int][]string, root, path string) fileResult {
	res := fileResult{path: path}
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

	// declName finds a declaration's identifier. Default: the SHALLOWEST
	// name-typed node in the subtree (first at that depth) — a Go method's
	// name (field_identifier, depth+1) beats its receiver variable (inside
	// parameter_list, depth+2). Strategies fix grammars where that lies:
	// "declarator" (C/C++: the name hides inside *_declarator, behind the
	// return type), "lastBeforeParams" (C#: a user-typed return type is
	// also a bare identifier — the name is the last one before params).
	declName := func(i int) (string, bool) {
		d := raw[i].Depth
		switch lang.NameStrategy[typeOf(raw[i])] {
		case "declarator":
			declDepth := -1
			for j := i + 1; j < len(raw) && raw[j].Depth > d; j++ {
				if declDepth >= 0 && int(raw[j].Depth) <= declDepth {
					declDepth = -1
				}
				t := typeOf(raw[j])
				if declDepth < 0 && strings.Contains(t, "declarator") {
					declDepth = int(raw[j].Depth)
					continue
				}
				if declDepth >= 0 && lang.Names[t] {
					return text(raw[j]), true
				}
			}
			return "", false
		case "lastBeforeParams":
			last := -1
			for j := i + 1; j < len(raw) && raw[j].Depth > d; j++ {
				if raw[j].Depth != d+1 {
					continue
				}
				t := typeOf(raw[j])
				if strings.Contains(t, "parameter") {
					break
				}
				if lang.Names[t] {
					last = j
				}
			}
			if last >= 0 {
				return text(raw[last]), true
			}
			return "", false
		default:
			best, bestDepth := -1, uint32(1<<31)
			for j := i + 1; j < len(raw) && raw[j].Depth > d; j++ {
				dep := raw[j].Depth - d
				if dep > 4 {
					continue
				}
				if lang.Names[typeOf(raw[j])] && dep < bestDepth {
					best, bestDepth = j, dep
				}
			}
			if best >= 0 {
				return text(raw[best]), true
			}
			return "", false
		}
	}

	// calleeName resolves a call site: the LAST name-typed node of the
	// callee expression, stopping at the arguments — `s.Merge(a)` is a call
	// to Merge, not to s; `self.bar()` is bar, not self.
	calleeName := func(i int) (string, bool) {
		d := raw[i].Depth
		last := -1
		for j := i + 1; j < len(raw) && raw[j].Depth > d; j++ {
			t := typeOf(raw[j])
			if strings.Contains(t, "argument") {
				break
			}
			if raw[j].Depth-d <= 3 && lang.Names[t] {
				last = j
			}
		}
		if last >= 0 {
			return text(raw[last]), true
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
			name, found := declName(i)
			if !found || name == "" {
				continue
			}
			qual := name
			if len(stack) > 0 {
				parentID := stack[len(stack)-1].id
				if idx := strings.LastIndex(parentID, "::"); idx >= 0 {
					qual = parentID[idx+2:] + "." + name
				}
			} else if lang.ReceiverQualify[t] {
				// Go method: the receiver type (first type_identifier before
				// the name) is the qualifier — Store.Merge, not Merge.
				for j := i + 1; j < len(raw) && raw[j].Depth > n.Depth; j++ {
					if txt := text(raw[j]); typeOf(raw[j]) == "type_identifier" {
						if txt == name {
							break
						}
						qual = txt + "." + name
						break
					}
				}
			}
			id := rel + "::" + qual
			parent := callerAt()
			meta := map[string]string{"lang": lang.Name}
			if sig := signatureOf(text(n)); sig != "" {
				meta["signature"] = sig
			}
			if doc := docAbove(raw, i, typeOf, text); doc != "" {
				meta["doc"] = doc
			}
			res.nodes = append(res.nodes, schema.Node{
				ID: id, Label: qual, Kind: kind, FileType: "code", Source: rel,
				Location: fmt.Sprintf("L%d-L%d", n.StartRow+1, n.EndRow+1),
				Metadata: meta,
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
			if callee, ok := calleeName(i); ok && callee != "" {
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

// signatureOf is the declaration's header line — what an agent needs to cite
// or call the symbol without opening the file (the symbol-card primitive; the
// spike measured pointer-chase file reads as the #1 context waste). First
// non-attribute line of the decl text, capped: decorators (@…), Rust #[…] and
// C# […] attributes are skipped so `@Override` doesn't shadow the method.
func signatureOf(declText string) string {
	lines := strings.Split(declText, "\n")
	start := -1
	for i, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "@") || strings.HasPrefix(l, "#[") ||
			strings.HasPrefix(l, "[") {
			continue
		}
		start = i
		break
	}
	if start < 0 {
		return ""
	}
	// A multi-line parameter list joins until parens balance — `def f(` alone
	// is not a signature.
	var sb strings.Builder
	depth := 0
	for i := start; i < len(lines) && i < start+8; i++ {
		l := strings.TrimSpace(lines[i])
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(l)
		depth += strings.Count(l, "(") - strings.Count(l, ")")
		if depth <= 0 || sb.Len() > 160 {
			break
		}
	}
	sig := strings.TrimSpace(strings.TrimRight(sb.String(), " \t{"))
	if len(sig) > 160 {
		sig = sig[:160] + "…"
	}
	return sig
}

// docAbove collects the comment block sitting DIRECTLY above a declaration.
// Preorder puts those comments immediately before the decl record (they start
// after the previous sibling's subtree), so walk backward while each record is
// a comment whose end row touches the running start row — a blank line breaks
// the chain, which is exactly the convention in every embedded language.
func docAbove(raw []RawNode, i int, typeOf func(RawNode) string, text func(RawNode) string) string {
	startRow := raw[i].StartRow
	var parts []string
	for j := i - 1; j >= 0; j-- {
		if !raw[j].Named { // newline/indent tokens (python) sit between
			continue
		}
		if raw[j].Start <= raw[i].Start && raw[j].End >= raw[i].End {
			continue // ancestor wrapper (python's block) — not a neighbor
		}
		if !strings.Contains(typeOf(raw[j]), "comment") || raw[j].EndRow+1 < startRow {
			break
		}
		parts = append([]string{strings.TrimSpace(text(raw[j]))}, parts...)
		startRow = raw[j].StartRow
	}
	doc := strings.Join(parts, "\n")
	if len(doc) > 500 {
		doc = doc[:500] + "…"
	}
	return doc
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
