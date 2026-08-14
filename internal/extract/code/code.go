// Package code is the tier-1 code producer: tree-sitter grammars compiled to
// WASI, hosted by wazero (pure Go, single binary), fanned across worker
// goroutines — the graphify-speed requirement is carried by parallelism.
//
// Per file it emits: a file node, one node per declaration (functions,
// methods, classes/structs/interfaces/enums/traits/types) with qualified
// labels (Class.method) and L#-L# locations, contains edges (file→decl,
// decl→nested decl), and import edges (file→module). Call sites resolve
// module-wide by name AFTER all files parse: a unique match becomes an
// INFERRED call edge. Two things are NOT guessed, and both surface as an
// AMBIGUOUS shortlist the agent can grep rather than as silence:
//
//   - the name is defined more than once (ADR 2026-07-25-abstain-out-loud);
//   - the callee is a METHOD reached through a receiver we could not tie to
//     its owner type (ADR 2026-07-25-method-call-resolution) — the graph holds
//     only OUR declarations, so a repo-unique method name is not evidence that
//     `err.Error()` meant ours. See receiverTies for the ties we do accept.
//
// Every traversal verb filters AMBIGUOUS out by default. Unknown names
// (stdlib, dependencies) have nothing in this repo to point at and are dropped
// outright.
package code

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/muthuishere/ctx-optimize/internal/boundaries"
	"github.com/muthuishere/ctx-optimize/internal/extract/ignore"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

const ProducerName = "code"

// maxFileBytes skips generated monsters (bundles, lock outputs).
const maxFileBytes = 2 << 20

// minifiedLineBytes: a source line longer than this marks a minified/generated
// file (hand-written code wraps; bundlers strip newlines). ~50k covers the
// longest realistic hand-written line (embedded data, long strings) while
// catching minified JS/CSS whose "lines" run to hundreds of KB.
const minifiedLineBytes = 50 * 1024

// receiverGate turns on receiver-aware resolution for METHOD candidates: a
// bare name is not evidence that a call targets a method of ours, because the
// graph holds only OUR declarations and can never see `error`, `io.Closer` or
// any dependency type. Sweepable so the trade stays a measurement.
// See receiverTies for the (exact, non-guessing) ties we accept.
var receiverGate = true

// ownerOf returns the immediate qualifier of a qualified label — Store for
// Store.Merge, Inner for Outer.Inner.run — and "" for an unqualified one.
func ownerOf(qual string) string {
	idx := strings.LastIndex(qual, ".")
	if idx < 0 {
		return ""
	}
	if prev := strings.LastIndex(qual[:idx], "."); prev >= 0 {
		return qual[prev+1 : idx]
	}
	return qual[:idx]
}

// receiverTies reports whether the call site gives us actual evidence that it
// targets d. It is deliberately narrow: every accepted tie is exact, none is a
// convention or a similarity score.
//
//	free function     — no receiver to check; the gate does not apply.
//	x.M() where x==T  — a call written on the type itself (Batch.Validate,
//	                    Python classmethods): the receiver IS the owner.
//	M() / self.M()    — an unqualified or self call from inside T: the
//	                    enclosing declaration is the receiver.
//	x.M(), T in scope — the owner type is NAMED in the same declaration as
//	                    the call (`var e = new Engine(); e.Add(1)`), and no
//	                    other declaration in the repo bears the name M. This
//	                    is the tie that keeps test→source edges alive; it is
//	                    evidence, hence INFERRED, not EXTRACTED.
//
// Anything else — `err.Error()` in a function that never names the error type
// — is unresolvable HERE, and gets shortlisted as AMBIGUOUS rather than
// attributed. Fixing those properly needs a type-aware producer (LSP/SCIP),
// which docs/VISION.md already names as the real answer.
func receiverTies(c callSite, d declRef, scope map[string]map[string]bool) bool {
	if d.owner == "" {
		return true
	}
	if c.recv == d.owner {
		return true
	}
	if c.recv == "" || selfReceivers[c.recv] {
		qual := c.callerID
		if idx := strings.Index(qual, "::"); idx >= 0 {
			qual = qual[idx+2:]
		}
		return ownerOf(qual) == d.owner
	}
	return scope[c.callerID][d.owner]
}

// ambiguousCap bounds how many candidates a single undecidable call site may
// shortlist. Above it we shortlist nothing — see shortlist() for why refusing
// beats 40 maybes. Sweepable so the value stays a measurement, not a guess.
var ambiguousCap = 4

// isMinified reports whether src looks machine-generated (minified) — judged by
// its longest line. Pure shape heuristic: no extension list to keep current,
// works for any language a bundler targets.
func isMinified(src []byte) bool {
	longest := 0
	start := 0
	for i, b := range src {
		if b == '\n' {
			if i-start > longest {
				longest = i - start
			}
			start = i + 1
		}
	}
	if len(src)-start > longest { // trailing line (no final newline)
		longest = len(src) - start
	}
	return longest > minifiedLineBytes
}

type fileResult struct {
	nodes  []schema.Node
	edges  []schema.Edge
	calls  []callSite
	decls  []declRef
	routes []routeSite
	bhits  []boundaries.Hit // boundary lane (ADR 2026-08-14): rides this walk
	// scopeNames maps a declaration id to the type-shaped names written
	// INSIDE it — the evidence receiverTies needs to accept `e.Add(1)` after
	// `var e = new Engine()` without ever guessing. Deliberately narrow: see
	// typeShaped for why the filter can only cost recall, never precision.
	scopeNames map[string]map[string]bool
	err        error
	path       string
}

// typeShaped reports whether a token looks like a type name in the languages
// we parse: CamelCase (Engine, HttpClient), not lowercase (locals, C
// identifiers) and not SHOUTING (macros, constants). It is a convention, so it
// is used ONLY to admit evidence — a type it misses stays unresolved and the
// call site is abstained on, never mis-attributed. That asymmetry is what
// makes a convention acceptable here.
func typeShaped(s string) bool {
	if s == "" {
		return false
	}
	r := rune(s[0])
	if r < 'A' || r > 'Z' {
		return false
	}
	for _, c := range s[1:] {
		if c >= 'a' && c <= 'z' {
			return true
		}
	}
	return false
}

type callSite struct {
	callerID string // innermost enclosing decl (or file) id
	callee   string // callee name as written
	recv     string // qualifier as written ("" when unqualified): s in s.Merge()
	file     string
}

type declRef struct {
	id    string
	label string // unqualified name
	owner string // qualifier of the declaration ("" for a free function): Store in Store.Merge
	file  string
}

// selfReceivers are receiver tokens that denote the enclosing instance rather
// than some other object, so `self.foo()` inside type T is evidence for T.foo
// in exactly the way `x.foo()` is not.
var selfReceivers = map[string]bool{"self": true, "this": true}

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
	return ExtractPaths(root, []string{root}, exclude)
}

// ExtractPaths gathers a multi-path module (ADR 2026-07-14): several scattered
// dirs (roots) parsed in ONE pass so calls resolve ACROSS them (test→source),
// with file paths recorded relative to base (the repo root) so the folders
// can't collide. Single-path callers pass base==root and roots==[root], which
// is byte-identical to the old single-root behavior.
// extractHits runs the walk purely for boundary sites (verify's HitSource).
func extractHits(root string, out *[]boundaries.Hit) error {
	_, _, hits, err := extractAll(root, []string{root}, nil)
	if err != nil {
		return err
	}
	*out = hits
	return nil
}

// ExtractPaths returns the code batch alone — the boundary batch it also
// produces is discarded. Callers that want both (the gather) use
// ExtractPathsWithBoundaries and pay for ONE walk instead of two.
func ExtractPaths(base string, roots []string, exclude []string) (*schema.Batch, error) {
	b, _, err := ExtractPathsWithBoundaries(base, roots, exclude)
	return b, err
}

// ExtractPathsWithBoundaries parses the tree ONCE and returns both the code
// batch and the boundary batch (ADR 2026-08-14 D2). The boundary lane used to
// re-walk and re-read every file to run regexes over the bytes; here its rules
// are probed against the node stream this walk already produced, so it costs a
// map lookup per candidate node rather than 90% of the gather.
func ExtractPathsWithBoundaries(base string, roots []string, exclude []string) (*schema.Batch, *schema.Batch, error) {
	cb, bb, _, err := extractAll(base, roots, exclude)
	return cb, bb, err
}

func extractAll(base string, roots []string, exclude []string) (*schema.Batch, *schema.Batch, []boundaries.Hit, error) {
	ctx := context.Background()
	skip := map[string]bool{}
	for _, e := range exclude {
		if abs, err := filepath.Abs(e); err == nil {
			skip[abs] = true
		}
	}

	packs, err := LoadPacks(base)
	if err != nil {
		return nil, nil, nil, err
	}
	// Route packs (routepacks.go): declarative call-shaped route rules,
	// discovered like grammar packs. Malformed packs fail the add loudly.
	routePacks, err := LoadRoutePacks(base)
	if err != nil {
		return nil, nil, nil, err
	}
	packRules := compileRoutePacks(routePacks)
	// Boundary rules (ADR 2026-08-14): loaded here so the ladder is resolved
	// once per gather and the matcher rides this walk. A malformed rule file
	// fails the add loudly, exactly like a malformed route pack.
	bndRules, err := boundaries.Load(base)
	if err != nil {
		return nil, nil, nil, err
	}
	bndIx := boundaries.BuildIndex(bndRules)
	bndSvcs, err := boundaries.LoadServices(base)
	if err != nil {
		return nil, nil, nil, err
	}
	bndIx.IndexServices(bndSvcs)
	// Declared resolutions (resolutions.go): the repo's own answers to what the
	// extractor refuses to decide. Malformed = hard error, like the packs.
	decl, err := LoadResolutions(base)
	if err != nil {
		return nil, nil, nil, err
	}
	external := newExternalSet(decl)
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

	ignored := ignore.New(base) // .gitignore semantics via git itself; nil = no git
	seenFile := map[string]bool{}
	var files []string
	for _, wr := range roots {
		err = filepath.WalkDir(wr, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if ignored != nil {
				if rel, rerr := filepath.Rel(base, path); rerr == nil && rel != "." && ignored(filepath.ToSlash(rel)) {
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
				if path != wr && (strings.HasPrefix(name, ".") || name == "node_modules" ||
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
			if !seenFile[path] { // scattered roots may nest; count each file once
				seenFile[path] = true
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, nil, nil, err
		}
	}
	sort.Strings(files) // deterministic output regardless of walk order

	// Symbol tables once per engine+language (read-only after this).
	symTab := map[string]map[int][]string{}
	loadSyms := func(key string, langs []Lang) error {
		if m, ok := cachedSymbols(key); ok {
			symTab[key] = m
			return nil
		}
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
		storeSymbols(key, m)
		symTab[key] = m
		return nil
	}
	if len(files) > 0 {
		if err := loadSyms("", Languages); err != nil {
			return nil, nil, nil, err
		}
	}
	for i := range packs {
		if err := loadSyms(packs[i].WasmPath, []Lang{packs[i].Lang}); err != nil {
			return nil, nil, nil, err
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
				results <- extractFile(ctx, inst, r.lang, symTab[r.engineKey], base, path, packRules, bndIx)
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
	dangling := 0 // broken symlinks: counted, summarized once, never per-file
	scopeNames := map[string]map[string]bool{}
	var calls []callSite
	var decls []declRef
	var routes []routeSite
	var bhits []boundaries.Hit
	for res := range results {
		if res.err != nil {
			// One unparseable file must not kill the gather — skip loudly.
			// EXCEPT a dangling symlink: the walker saw the link, the read
			// followed it to nothing. That is the repo's state, not a problem
			// with the file or with us, and a big tree has thousands of them
			// (chromium's third_party/nearby vendors broken links). Reporting
			// each one as an error trains the reader to ignore the channel that
			// carries the real skips.
			if errors.Is(res.err, fs.ErrNotExist) {
				dangling++
				continue
			}
			fmt.Fprintf(os.Stderr, "ctx-optimize: skip %s: %v\n", res.path, res.err)
			continue
		}
		batch.Nodes = append(batch.Nodes, res.nodes...)
		batch.Edges = append(batch.Edges, res.edges...)
		calls = append(calls, res.calls...)
		decls = append(decls, res.decls...)
		for id, names := range res.scopeNames {
			if scopeNames[id] == nil {
				scopeNames[id] = names
				continue
			}
			for n := range names {
				scopeNames[id][n] = true
			}
		}
		routes = append(routes, res.routes...)
		bhits = append(bhits, res.bhits...)
	}

	// Call resolution: same-FILE unique match wins (self.audit resolves in
	// its own file even when the name repeats elsewhere), else module-wide
	// unique. An undecidable name is never guessed: its candidates become an
	// AMBIGUOUS shortlist (see shortlist). Unknown/external names are dropped.
	byName := map[string][]declRef{}
	for _, d := range decls {
		byName[d.label] = append(byName[d.label], d)
	}
	pick := func(c callSite, gate bool) *declRef {
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
			if gate && receiverGate && !receiverTies(c, cands[0], scopeNames) {
				return nil
			}
			return &cands[0]
		}
		return nil
	}
	// shortlist returns the candidates for a callee that pick could NOT decide
	// between, or nil when there is nothing to shortlist. Two outcomes look
	// identical to pick but are completely different facts:
	//
	//   >1 candidate  → the name IS defined in this repo, we cannot say which.
	//                   Actionable: a grep on the name settles it.
	//    0 candidates → stdlib or a dependency. Nothing here to point at, so
	//                   there is no shortlist and no gap to report.
	//
	// Above ambiguousCap we shortlist nothing: a name like `get`/`new`/`append`
	// has dozens of definitions, and N edges per call site would pollute the
	// god-node ranking for no navigational gain (docs/VISION.md:284 measured
	// exactly that failure). Refusing to shortlist is the honest answer there —
	// grep is strictly better than 40 maybes.
	//
	// The two abstentions carry DIFFERENT reasons, and the reason decides
	// which grep settles it, so it is stamped on the edge rather than left
	// for the reader to assume (schema.AmbiguousNameCollision /
	// schema.AmbiguousUnresolvedReceiver).
	shortlist := func(c callSite) ([]declRef, string) {
		cands := byName[c.callee]
		var inFile []declRef
		for _, d := range cands {
			if d.file == c.file {
				inFile = append(inFile, d)
			}
		}
		if len(inFile) > 1 {
			cands = inFile // ambiguous within one file: never widen to the module
		}
		if len(cands) == 1 {
			// Only reachable when the receiver gate refused the sole
			// candidate. The name IS declared here — we simply never
			// established whose method the call site meant, so the candidate
			// is a maybe, not a miss.
			return cands, schema.AmbiguousUnresolvedReceiver
		}
		if len(cands) < 2 || len(cands) > ambiguousCap {
			return nil, ""
		}
		return cands, schema.AmbiguousNameCollision
	}
	seen := map[string]bool{}
	for _, c := range calls {
		t := pick(c, true)
		if t == nil {
			// Declared external: the repo says this method name belongs to a
			// type it does not own, so there is nothing here to shortlist.
			// Checked only on the abstention path — a declaration can retire a
			// maybe, never a resolved edge.
			if external.suppress(c) {
				continue
			}
			// Undecidable. Emit the shortlist as AMBIGUOUS so the agent can see
			// the candidates and grep, instead of the call site vanishing and
			// the graph looking complete. Every traversal verb filters these
			// out by default (analyze.WithoutAmbiguous) — the label is only
			// honest if the consumers honor it.
			cands, reason := shortlist(c)
			for _, cand := range cands {
				if cand.id == c.callerID {
					continue
				}
				key := c.callerID + "\x00" + cand.id
				if seen[key] {
					continue
				}
				seen[key] = true
				batch.Edges = append(batch.Edges, schema.Edge{
					Source: c.callerID, Target: cand.id,
					Relation: "calls", Confidence: schema.Ambiguous, Weight: 1,
					Metadata: map[string]string{"ambiguous_reason": reason},
				})
			}
			continue
		}
		if t.id == c.callerID {
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

	// Framework routes (routes.go): route nodes + handles edges. An express
	// identifier handler resolves exactly like a call site — same pick, same
	// ambiguity honesty. Duplicate route IDs (same METHOD+path re-registered
	// in one file) keep the first declaration, deterministically: IDs embed
	// the file path, and each file's routes arrive in source order.
	routeSeen := map[string]bool{}
	for _, r := range routes {
		if !routeSeen[r.node.ID] {
			routeSeen[r.node.ID] = true
			batch.Nodes = append(batch.Nodes, r.node)
		}
		target := r.handlerID
		if target == "" {
			if r.handlerName == "" {
				continue // inline anonymous handler — no decl node to point at
			}
			// A route handler is named, never called through a receiver, so
			// there is no receiver to gate on.
			t := pick(callSite{callee: r.handlerName, file: r.file}, false)
			if t == nil {
				continue
			}
			target = t.id
		}
		key := r.node.ID + "\x00" + target + "\x00handles"
		if seen[key] {
			continue
		}
		seen[key] = true
		batch.Edges = append(batch.Edges, schema.Edge{
			Source: r.node.ID, Target: target, Relation: "handles",
			Confidence: "INFERRED", Weight: 1,
			Metadata: map[string]string{"synthesized_by": r.channel},
		})
	}
	if dangling > 0 {
		fmt.Fprintf(os.Stderr, "ctx-optimize: skipped %d broken symlink(s)\n", dangling)
	}
	// A declared name that matched nothing is reported, not ignored: the author
	// believes the line is in force, and a stale one is a lie waiting to be read.
	if stale := external.unused(); len(stale) > 0 {
		fmt.Fprintf(os.Stderr, "ctx-optimize: %s: external_methods matched no call site: %s\n",
			ResolutionsFile(base), strings.Join(stale, ", "))
	}
	// D7 (ADR 2026-08-13): join in-repo imports to the files they name —
	// relative/alias specifiers rewrite the edge, go packages gain
	// resolves_to. External/unknown specifiers stay untouched.
	resolveImports(base, roots, batch)
	sortBatch(batch)
	bnd, berr := boundaries.Assemble(base, exclude, bndRules, bhits)
	if berr != nil {
		return nil, nil, nil, berr
	}
	return batch, bnd, bhits, nil
}

func extractFile(ctx context.Context, inst *Instance, lang *Lang, symTab map[int][]string, root, path string, packRules map[string][]packRule, bndIx *boundaries.Index) fileResult {
	res := fileResult{path: path, scopeNames: map[string]map[string]bool{}}
	src, err := os.ReadFile(path)
	if err != nil {
		res.err = err
		return res
	}
	// Minified/generated bundles (committed dist, *.min.js, webpack output) are
	// under maxFileBytes yet parse into thousands of junk symbols that dominate
	// hubs/query. Detect by shape — one enormous line — and skip so the graph
	// stays the hand-written code the agent actually asks about.
	if isMinified(src) {
		return res // no nodes/edges/err: silently drop, like an ignored file
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

	// Boundary lane (ADR 2026-08-14-boundaries-on-the-ast): declarative node
	// shapes probed against the stream we already have. nil whenever no rule
	// wants this file, so a repo the rules do not cover pays one comparison.
	var bnd *bndCtx
	if ext := strings.ToLower(filepath.Ext(rel)); bndIx.Applies(ext) {
		bnd = &bndCtx{ix: bndIx, rel: rel, ext: ext, hits: &res.bhits,
			raw: raw, src: src, class: shapeClassesFor(lang.ID, syms),
			mask:   bndIx.MaskFor(ext),
			typeOf: typeOf, text: text}
	}

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

	// calleeName resolves a call site into (receiver, callee): the LAST
	// name-typed node of the callee expression is the callee — `s.Merge(a)`
	// is a call to Merge, not to s; `self.bar()` is bar, not self — and the
	// one immediately before it is the receiver ("" when the call is
	// unqualified). The receiver used to be dropped here, which made every
	// `err.Error()` indistinguishable from `Error()` and let a repo-unique
	// method name absorb call sites that never targeted it (see the
	// receiver gate in Extract).
	calleeName := func(i int) (recv string, callee string, ok bool) {
		d := raw[i].Depth
		last, prev := -1, -1
		for j := i + 1; j < len(raw) && raw[j].Depth > d; j++ {
			t := typeOf(raw[j])
			if strings.Contains(t, "argument") {
				break
			}
			if raw[j].Depth-d <= 3 && lang.Names[t] {
				last, prev = j, last
			}
		}
		if last < 0 {
			return "", "", false
		}
		if prev >= 0 {
			recv = text(raw[prev])
		}
		return recv, text(raw[last]), true
	}

	// Route recognition (routes.go) rides this same visit — no second walk.
	isPy := lang.Name == "python"
	isJSFam := lang.Name == "javascript" || lang.Name == "typescript" || lang.Name == "tsx"
	isTS := lang.Name == "typescript" || lang.Name == "tsx"

	type openDecl struct {
		id       string
		depth    uint32
		ctrlBase string // NestJS @Controller base path (valid when isCtrl)
		isCtrl   bool
	}
	var stack []openDecl
	callerAt := func() string {
		if len(stack) == 0 {
			return fileID
		}
		return stack[len(stack)-1].id
	}

	// hasAncestor reports whether any ancestor of raw[i] is one of types. raw
	// is pre-order, so successive strictly-smaller depths walking backward are
	// exactly the ancestor chain.
	hasAncestor := func(i int, types []string) bool {
		if len(types) == 0 {
			return false
		}
		d := raw[i].Depth
		for j := i - 1; j >= 0 && d > 0; j-- {
			if raw[j].Depth < d {
				d = raw[j].Depth
				for _, s := range types {
					if typeOf(raw[j]) == s {
						return true
					}
				}
			}
		}
		return false
	}

	// headDecl resolves a homoiconic declaration: a container node whose FIRST
	// named child's literal text names a defining macro, and whose SECOND named
	// child is the name being defined. Both reads are literal; a head that does
	// not match exactly (including any namespace-qualified `s/def`) simply
	// misses, and a name slot that is not a plain symbol is skipped. Under-
	// claiming is the intended failure mode.
	headDecl := func(i int) (string, string, bool) {
		t := typeOf(raw[i])
		d := raw[i].Depth
		for _, r := range lang.DeclRules {
			if r.Node != t {
				continue
			}
			var head, nm int = -1, -1
			for j := i + 1; j < len(raw) && raw[j].Depth > d; j++ {
				if !raw[j].Named || raw[j].Depth != d+1 {
					continue
				}
				if head < 0 {
					head = j
				} else {
					nm = j
					break
				}
			}
			if head < 0 || nm < 0 {
				continue
			}
			if r.HeadType != "" && typeOf(raw[head]) != r.HeadType {
				continue
			}
			kind, ok := r.HeadMatch[text(raw[head])]
			if !ok {
				continue
			}
			// Step over a wrapper in the name slot — `(in-ns 'bri.cli)` wraps
			// the symbol in a quoting_lit. One level only.
			for _, w := range r.NameUnwrap {
				if typeOf(raw[nm]) != w {
					continue
				}
				inner := -1
				for j := nm + 1; j < len(raw) && raw[j].Depth > raw[nm].Depth; j++ {
					if raw[j].Named {
						inner = j
						break
					}
				}
				if inner >= 0 {
					nm = inner
				}
				break
			}
			if r.NameType != "" && typeOf(raw[nm]) != r.NameType {
				continue // metadata or destructuring in the name slot: skip
			}
			if hasAncestor(i, r.SkipInside) {
				continue // quoted: a macro CONSTRUCTING code, not a definition
			}
			if name := text(raw[nm]); name != "" {
				return kind, name, true
			}
		}
		return "", "", false
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

		// Record type-shaped names per enclosing declaration — the evidence
		// receiverTies uses to accept a receiver-qualified call. Collected on
		// the walk we already do; filtered by typeShaped so a lowercase-heavy
		// corpus (C) costs almost nothing.
		if lang.Names[t] {
			if txt := text(n); typeShaped(txt) {
				id := callerAt()
				set := res.scopeNames[id]
				if set == nil {
					set = map[string]bool{}
					res.scopeNames[id] = set
				}
				set[txt] = true
			}
		}

		kind, isDecl := lang.Decls[t]
		headName := ""
		if !isDecl && len(lang.DeclRules) > 0 {
			if k, nm, hit := headDecl(i); hit {
				kind, headName, isDecl = k, nm, true
			}
		}
		if isDecl {
			name, found := headName, headName != ""
			if !found {
				name, found = declName(i)
			}
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
			res.decls = append(res.decls, declRef{id: id, label: name, owner: ownerOf(qual), file: rel})

			var ctrlBase string
			var isCtrl bool
			if isPy && t == "function_definition" {
				res.routes = append(res.routes, pyDecoratorRoutes(raw, i, typeOf, text, rel, lang.Name, id)...)
			} else if isTS {
				switch t {
				case "class_declaration", "abstract_class_declaration":
					ctrlBase, isCtrl = nestControllerBase(raw, i, typeOf, text)
				case "method_definition":
					for k := len(stack) - 1; k >= 0; k-- {
						if stack[k].isCtrl {
							res.routes = append(res.routes, nestMethodRoutes(raw, i, typeOf, text, rel, lang.Name, stack[k].ctrlBase, id)...)
							break
						}
					}
				}
			}
			stack = append(stack, openDecl{id: id, depth: n.Depth, ctrlBase: ctrlBase, isCtrl: isCtrl})
			continue
		}

		// React Router JSX (frontend_routes.go): <Route path="…" …/> — the
		// js grammar parses JSX too, so .jsx and .tsx both land here.
		if isJSFam && (t == "jsx_element" || t == "jsx_self_closing_element") {
			if site, ok := jsxRouteSite(raw, i, typeOf, text, rel, lang.Name); ok {
				res.routes = append(res.routes, site)
			}
			continue
		}

		if bnd != nil {
			switch bnd.classAt(i) {
			case clsString:
				bnd.probeLiteral(i)
			case clsMember:
				bnd.probeMember(i)
			case clsSubscript:
				bnd.probeSubscript(i)
			case clsAnnotation:
				bnd.probeAnnotation(i)
			case clsNew:
				bnd.probeNew(i)
			}
		}

		if lang.Calls[t] {
			if isJSFam {
				if site, ok := expressRoute(raw, i, typeOf, text, rel, lang.Name); ok {
					res.routes = append(res.routes, site)
				}
				res.routes = append(res.routes, frontendRouterRoutes(raw, i, typeOf, text, rel, lang.Name)...)
			}
			if recv, callee, ok := calleeName(i); ok && callee != "" {
				if bnd != nil {
					bnd.probeCall(i, recv, callee)
				}
				if rules := packRules[callee]; len(rules) > 0 {
					res.routes = append(res.routes, packRouteSites(raw, i, typeOf, text, rel, lang.Name, rules)...)
				}
				res.calls = append(res.calls, callSite{callerID: callerAt(), callee: callee, recv: recv, file: rel})
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
