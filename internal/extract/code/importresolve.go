// importresolve.go — in-repo import-specifier resolution (ADR 2026-08-13
// boundary-model-and-defaults, D7). The walker emits every import as
// `file --imports--> module://<spec>`, which is honest but dead-ended: on k8s
// only 1.8% of import edges reached anything traversable. This post-pass
// resolves the specifiers that name files INSIDE the repo:
//
//   - relative specifiers (./x, ../y — JS/TS, and python's leading-dot form)
//     and tsconfig/jsconfig path aliases (@/*) name exactly ONE file, but the
//     module:// ID is importer-relative — `module://./util` from two folders
//     is two different files sharing one node. The only correct join is a
//     per-edge REWRITE: the imports edge is retargeted at the file node and
//     the now-orphaned placeholder is pruned.
//   - go module imports (go.mod `module` prefix) name a PACKAGE — a directory
//     of files. The module node stays (its ID is globally unique) and gains
//     `module://<spec> --resolves_to--> <file>` edges, the same join shape
//     deplink uses for manifest dependencies.
//
// Everything else (npm packages, stdlib, anything not present in the gathered
// file set) is left exactly as it was: a miss is honest, a guessed join is
// not. Resolution is mechanical path arithmetic over declared config, so the
// edges stay EXTRACTED.
package code

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// jsExts is the runtime's resolution order for extensionless specifiers.
var jsExts = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts"}

type aliasRule struct {
	prefix string // spec prefix before '*', e.g. "@/"
	target string // base-relative dir the '*' maps into, e.g. "web/src/"
	exact  bool   // no '*': the whole spec maps to target
}

type goModRule struct {
	prefix string // module path from go.mod
	dir    string // base-relative dir of that go.mod ("" = repo root)
}

type importResolver struct {
	files   map[string]bool     // every gathered file node ID (slash rel path)
	goByDir map[string][]string // dir → sorted .go file IDs in it
	aliases []aliasRule
	goMods  []goModRule
}

func newImportResolver(base string, roots []string, batch *schema.Batch) *importResolver {
	r := &importResolver{files: map[string]bool{}, goByDir: map[string][]string{}}
	for _, n := range batch.Nodes {
		if n.Kind != "file" {
			continue
		}
		r.files[n.ID] = true
		if strings.HasSuffix(n.ID, ".go") {
			d := path.Dir(n.ID)
			r.goByDir[d] = append(r.goByDir[d], n.ID)
		}
	}
	for d := range r.goByDir {
		sort.Strings(r.goByDir[d])
	}
	// Config discovery is non-recursive at base + each root: per-module
	// gathers already pass the module dir as a root, so a monorepo app's own
	// tsconfig/go.mod is seen without a second tree walk. Malformed or absent
	// config just means no aliases — resolution is best-effort by design.
	dirs := map[string]bool{}
	if abs, err := filepath.Abs(base); err == nil {
		dirs[abs] = true
	}
	for _, wr := range roots {
		if abs, err := filepath.Abs(wr); err == nil {
			dirs[abs] = true
		}
	}
	var sorted []string
	for d := range dirs {
		sorted = append(sorted, d)
	}
	sort.Strings(sorted)
	for _, dir := range sorted {
		rel, err := filepath.Rel(base, dir)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		r.loadGoMod(filepath.Join(dir, "go.mod"), rel)
		for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
			r.loadTSPaths(filepath.Join(dir, name), rel)
		}
	}
	// Longest module prefix wins (nested go.mod under a parent module).
	sort.Slice(r.goMods, func(i, j int) bool { return len(r.goMods[i].prefix) > len(r.goMods[j].prefix) })
	return r
}

// loadGoMod reads the `module` line plus any `replace` directives whose
// target is a LOCAL path (`k8s.io/api => ./staging/src/k8s.io/api`) — the
// declared aliasing k8s-style monorepos rely on; remote replaces are not
// in-repo and are ignored.
func (r *importResolver) loadGoMod(p, dirRel string) {
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	inReplace := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module"); ok && !inReplace {
			mod := strings.TrimSpace(strings.Trim(strings.TrimSpace(rest), `"`))
			if mod != "" {
				r.goMods = append(r.goMods, goModRule{prefix: mod, dir: dirRel})
			}
			continue
		}
		stmt := ""
		switch {
		case inReplace && line == ")":
			inReplace = false
		case inReplace:
			stmt = line
		case strings.HasPrefix(line, "replace ("):
			inReplace = true
		case strings.HasPrefix(line, "replace "):
			stmt = strings.TrimPrefix(line, "replace ")
		}
		if stmt == "" {
			continue
		}
		lhs, rhs, ok := strings.Cut(stmt, "=>")
		if !ok {
			continue
		}
		tgt := strings.Fields(strings.TrimSpace(rhs))
		old := strings.Fields(strings.TrimSpace(lhs))
		if len(old) == 0 || len(tgt) != 1 { // versioned target = remote module
			continue
		}
		local := filepath.ToSlash(tgt[0])
		if !strings.HasPrefix(local, "./") && !strings.HasPrefix(local, "../") {
			continue
		}
		r.goMods = append(r.goMods, goModRule{prefix: old[0], dir: path.Join(dirRel, local)})
	}
}

// loadTSPaths reads compilerOptions.baseUrl + compilerOptions.paths from a
// tsconfig/jsconfig. tsconfig is JSONC, so comments are stripped first
// (string-aware — a "//" inside a path value survives).
func (r *importResolver) loadTSPaths(p, dirRel string) {
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var cfg struct {
		CompilerOptions struct {
			BaseURL string              `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(stripJSONC(data), &cfg); err != nil {
		return
	}
	baseURL := cfg.CompilerOptions.BaseURL
	if baseURL == "" {
		baseURL = "."
	}
	anchor := path.Join(dirRel, filepath.ToSlash(baseURL))
	var keys []string
	for k := range cfg.CompilerOptions.Paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		targets := cfg.CompilerOptions.Paths[k]
		if len(targets) == 0 {
			continue
		}
		tgt := filepath.ToSlash(targets[0]) // first mapping wins, like tsc
		star := strings.IndexByte(k, '*')
		if star < 0 {
			r.aliases = append(r.aliases, aliasRule{prefix: k, target: path.Join(anchor, tgt), exact: true})
			continue
		}
		tstar := strings.IndexByte(tgt, '*')
		if tstar < 0 {
			continue
		}
		r.aliases = append(r.aliases, aliasRule{
			prefix: k[:star],
			target: path.Join(anchor, tgt[:tstar]) + "/",
		})
	}
}

// stripJSONC removes // and /* */ comments without touching string contents.
func stripJSONC(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		c := in[i]
		switch {
		case c == '"':
			out = append(out, c)
			for i++; i < len(in); i++ {
				out = append(out, in[i])
				if in[i] == '\\' && i+1 < len(in) {
					i++
					out = append(out, in[i])
					continue
				}
				if in[i] == '"' {
					break
				}
			}
		case c == '/' && i+1 < len(in) && in[i+1] == '/':
			for i < len(in) && in[i] != '\n' {
				i++
			}
			if i < len(in) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(in) && in[i+1] == '*':
			for i += 2; i+1 < len(in) && !(in[i] == '*' && in[i+1] == '/'); i++ {
			}
			i++
		default:
			out = append(out, c)
		}
	}
	return out
}

// resolveFile maps a cleaned base-relative candidate path to the ONE file
// node it names, trying the runtime's extension/index order. "" = no match.
func (r *importResolver) resolveFile(cand string) string {
	if r.files[cand] {
		return cand
	}
	for _, ext := range jsExts {
		if r.files[cand+ext] {
			return cand + ext
		}
	}
	for _, ext := range jsExts {
		if idx := cand + "/index" + ext; r.files[idx] {
			return idx
		}
	}
	if r.files[cand+".py"] {
		return cand + ".py"
	}
	if init := cand + "/__init__.py"; r.files[init] {
		return init
	}
	return ""
}

// resolveEdge answers one import edge. Exactly one of file/goDir is set on
// success; both empty = leave the edge alone.
func (r *importResolver) resolveEdge(importer, spec string) (file, goDir string) {
	dir := path.Dir(importer)
	// Relative: ./x, ../y (JS/TS), and python's .mod/..pkg.mod form.
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") {
		return r.resolveFile(path.Clean(path.Join(dir, spec))), ""
	}
	if strings.HasPrefix(spec, ".") && strings.HasSuffix(importer, ".py") {
		dots := 0
		for dots < len(spec) && spec[dots] == '.' {
			dots++
		}
		up := dir
		for i := 1; i < dots; i++ {
			up = path.Dir(up)
		}
		rest := strings.ReplaceAll(spec[dots:], ".", "/")
		return r.resolveFile(path.Clean(path.Join(up, rest))), ""
	}
	// tsconfig/jsconfig alias.
	for _, a := range r.aliases {
		if a.exact {
			if spec == a.prefix {
				if f := r.resolveFile(path.Clean(a.target)); f != "" {
					return f, ""
				}
			}
			continue
		}
		if rest, ok := strings.CutPrefix(spec, a.prefix); ok {
			if f := r.resolveFile(path.Clean(a.target + rest)); f != "" {
				return f, ""
			}
		}
	}
	// Go module prefix → package directory.
	for _, m := range r.goMods {
		var pkg string
		if spec == m.prefix {
			pkg = m.dir
		} else if rest, ok := strings.CutPrefix(spec, m.prefix+"/"); ok {
			pkg = path.Join(m.dir, rest)
		} else {
			continue
		}
		if pkg == "" {
			pkg = "."
		}
		if len(r.goByDir[pkg]) > 0 {
			return "", pkg
		}
	}
	// Python absolute dotted module, resolved against the repo root only —
	// importer-relative absolute imports are py2 semantics and would guess.
	if strings.HasSuffix(importer, ".py") && !strings.Contains(spec, "/") {
		return r.resolveFile(strings.ReplaceAll(spec, ".", "/")), ""
	}
	return "", ""
}

// resolveImports runs the post-pass over a finished batch, in place.
func resolveImports(base string, roots []string, batch *schema.Batch) {
	r := newImportResolver(base, roots, batch)
	if len(r.files) == 0 {
		return
	}
	goResolved := map[string]string{} // module:// ID → package dir (emit once)
	seen := map[string]bool{}         // dedupe rewritten file→file edges
	out := batch.Edges[:0]
	for _, e := range batch.Edges {
		spec, isImport := strings.CutPrefix(e.Target, "module://")
		if !isImport || e.Relation != "imports" {
			out = append(out, e)
			continue
		}
		file, goDir := r.resolveEdge(e.Source, spec)
		if goDir != "" {
			goResolved[e.Target] = goDir
			out = append(out, e)
			continue
		}
		if file == "" || file == e.Source {
			out = append(out, e)
			continue
		}
		key := e.Source + "\x00" + file
		if seen[key] {
			continue // two specifiers landing on one file collapse
		}
		seen[key] = true
		e.Target = file
		out = append(out, e)
	}
	batch.Edges = out
	var goIDs []string
	for id := range goResolved {
		goIDs = append(goIDs, id)
	}
	sort.Strings(goIDs)
	for _, id := range goIDs {
		for _, f := range r.goByDir[goResolved[id]] {
			batch.Edges = append(batch.Edges, schema.Edge{
				Source: id, Target: f, Relation: "resolves_to",
				Confidence: schema.Extracted, Weight: 1,
				Metadata: map[string]string{"synthesized_by": "importresolve"},
			})
		}
	}
	// Prune module:// nodes the rewrite orphaned — a placeholder nothing
	// references is noise, and hubs/query would still rank it.
	referenced := map[string]bool{}
	for _, e := range batch.Edges {
		referenced[e.Source] = true
		referenced[e.Target] = true
	}
	nodes := batch.Nodes[:0]
	for _, n := range batch.Nodes {
		if n.Kind == "module" && strings.HasPrefix(n.ID, "module://") && !referenced[n.ID] {
			continue
		}
		nodes = append(nodes, n)
	}
	batch.Nodes = nodes
}
