// Package scan discovers module roots inside a repository — the generator
// half of multi-module support. It is a bounded, exhaustive, marker-based
// filesystem walk: build files declare where projects live, scan reports ALL
// of them, and `init --scan` writes the found list into the committed
// .ctxoptimize/config.json. Nothing here infers from code; the walk only
// trusts markers and explicit globs, so the result is deterministic for a
// given tree.
package scan

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Module is one discovered (or declared) module root.
//
// Two shapes:
//   - single-path: {path} (optionally {name} for the navigator label). The
//     store mirrors the folder; IDs are module-dir-relative. This is the
//     discovered form and the historical config form.
//   - multi-path (ADR 2026-07-14): {name, paths[]}. A module is a NAME plus a
//     SET of scattered folders (e.g. src/Billing + tests/Billing.Tests)
//     gathered into ONE store keyed by Name, with IDs recorded
//     repo-root-relative so the folders can't collide and code extracts in a
//     single pass (test→source calls resolve). `name` is REQUIRED here.
type Module struct {
	Path   string   `json:"path,omitempty"`  // single-path: repo-relative, slash-form
	Paths  []string `json:"paths,omitempty"` // multi-path: the scattered dirs; when set this is a multi-path module
	Name   string   `json:"name,omitempty"`  // store name override (single) / store key + REQUIRED (multi)
	Marker string   `json:"marker,omitempty"` // evidence: which marker declared it (scan output only)
}

// Multi reports whether this is a multi-path module (keyed by Name, gathered
// repo-root-relative).
func (m Module) Multi() bool { return len(m.Paths) > 0 }

// KeySeg is the store-key segment under the root: the mirrored Path for a
// single-path module, the (sanitized-by-caller) Name for a multi-path one.
func (m Module) KeySeg() string {
	if m.Multi() {
		return m.Name
	}
	return m.Path
}

// Dirs are the repo-relative folders this module gathers.
func (m Module) Dirs() []string {
	if m.Multi() {
		return m.Paths
	}
	return []string{m.Path}
}

// NSPrefix is the federation namespace prefix applied at read time: a
// single-path store is module-dir-relative (prefixed by Path to make it
// repo-root-relative), a multi-path store is ALREADY repo-root-relative (no
// prefix).
func (m Module) NSPrefix() string {
	if m.Multi() {
		return ""
	}
	return m.Path
}

// Options tunes the generator. Zero value = defaults (depth 5, built-in
// markers). Markers/Include/Exclude extend the built-ins, they never replace
// them.
type Options struct {
	Depth   int      `json:"depth,omitempty"`   // max depth below root (default 5)
	Markers []string `json:"markers,omitempty"` // extra marker file names
	Include []string `json:"include,omitempty"` // globs force-added as modules
	Exclude []string `json:"exclude,omitempty"` // globs pruned from the walk
}

// Result is a scan outcome: every module found, plus whether the depth bound
// clipped the walk (markers seen at the boundary — deeper ones may exist).
type Result struct {
	Modules []Module `json:"modules"`
	Clipped bool     `json:"clipped"`
	Depth   int      `json:"depth"`
}

const DefaultDepth = 5

// builtinMarkers: a build file in a directory declares a project root.
var builtinMarkers = map[string]bool{
	"go.mod": true, "go.work": true,
	"package.json":    true,
	"pom.xml":         true,
	"settings.gradle": true, "settings.gradle.kts": true,
	"build.gradle": true, "build.gradle.kts": true,
	"Cargo.toml":     true,
	"pyproject.toml": true, "setup.py": true,
}

// pruneDirs are never descended into — generated/vendored trees where a
// package.json is noise, not a project.
var pruneDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, "target": true, ".venv": true, "venv": true,
	".next": true, "__pycache__": true, ".gradle": true, ".idea": true,
}

// Scan walks root to Options.Depth and returns every module directory found,
// sorted by path. The root itself is never a module. Exhaustive by design:
// the walk always completes; Clipped reports when markers sat exactly at the
// depth boundary so the caller can suggest a deeper pass.
func Scan(root string, o Options) (*Result, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	depth := o.Depth
	if depth <= 0 {
		depth = DefaultDepth
	}
	markers := make(map[string]bool, len(builtinMarkers)+len(o.Markers))
	for m := range builtinMarkers {
		markers[m] = true
	}
	for _, m := range o.Markers {
		if m = strings.TrimSpace(m); m != "" {
			markers[m] = true
		}
	}

	found := map[string]string{} // rel dir → marker evidence
	clipped := false
	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip, stay exhaustive elsewhere
		}
		rel, rerr := filepath.Rel(absRoot, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		level := strings.Count(rel, "/") + 1
		if rel == "." {
			level = 0
		}
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			name := d.Name()
			if pruneDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			for _, g := range o.Exclude {
				if ok, _ := filepath.Match(g, rel); ok {
					return filepath.SkipDir
				}
			}
			if level > depth {
				clipped = clipped || hasMarker(path, markers)
				return filepath.SkipDir
			}
			// A child .ctxoptimize/ dir is itself evidence of a module.
			if name == ".ctxoptimize" { // unreachable via dot-prefix skip; kept for clarity
				return filepath.SkipDir
			}
			if _, e := os.Stat(filepath.Join(path, ".ctxoptimize")); e == nil {
				if _, ok := found[rel]; !ok {
					found[rel] = ".ctxoptimize"
				}
			}
			return nil
		}
		if level-1 > depth { // file deeper than bound (dir already handled)
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			return nil // the root is the root, not a module
		}
		if markers[d.Name()] {
			if _, ok := found[dir]; !ok {
				found[dir] = d.Name()
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, g := range o.Include {
		matches, _ := filepath.Glob(filepath.Join(absRoot, filepath.FromSlash(g)))
		for _, m := range matches {
			if fi, e := os.Stat(m); e != nil || !fi.IsDir() {
				continue
			}
			rel, e := filepath.Rel(absRoot, m)
			if e != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			if rel != "." {
				if _, ok := found[rel]; !ok {
					found[rel] = "include"
				}
			}
		}
	}

	res := &Result{Depth: depth, Clipped: clipped}
	for rel, marker := range found {
		res.Modules = append(res.Modules, Module{Path: rel, Marker: marker})
	}
	sort.Slice(res.Modules, func(i, j int) bool { return res.Modules[i].Path < res.Modules[j].Path })
	return res, nil
}

// DefaultName derives the store-name for a module path: slashes to dashes.
func DefaultName(path string) string {
	return strings.ReplaceAll(strings.Trim(filepath.ToSlash(path), "/"), "/", "-")
}

// Expand resolves a declared module list (which may contain globs) against
// the repo root into concrete existing directories, sorted, each at most
// once (first declaration wins its name).
func Expand(root string, declared []Module) ([]Module, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Module
	for _, m := range declared {
		if m.Multi() {
			em, err := expandMulti(absRoot, root, m, seen)
			if err != nil {
				return nil, err
			}
			if em != nil {
				out = append(out, *em)
			}
			continue
		}
		p := strings.Trim(filepath.ToSlash(m.Path), "/")
		if p == "" || p == "." {
			continue
		}
		var dirs []string
		if strings.ContainsAny(p, "*?[") {
			matches, _ := filepath.Glob(filepath.Join(absRoot, filepath.FromSlash(p)))
			sort.Strings(matches)
			dirs = matches
		} else {
			dirs = []string{filepath.Join(absRoot, filepath.FromSlash(p))}
		}
		for _, d := range dirs {
			fi, e := os.Stat(d)
			if e != nil || !fi.IsDir() {
				if !strings.ContainsAny(p, "*?[") {
					return nil, fmt.Errorf("declared module %q not found under %s", m.Path, root)
				}
				continue
			}
			rel, e := filepath.Rel(absRoot, d)
			if e != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			if rel == "." || seen[rel] {
				continue
			}
			seen[rel] = true
			name := m.Name
			if name == "" || strings.ContainsAny(p, "*?[") && len(dirs) > 1 {
				name = DefaultName(rel)
			}
			out = append(out, Module{Path: rel, Name: name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].KeySeg() < out[j].KeySeg() })
	return out, nil
}

// expandMulti resolves a multi-path module ({name, paths[]}) into one Module
// carrying the concrete, sorted, repo-relative dirs. Name is required (it is
// the store key); each path may glob; a non-glob path that doesn't exist is an
// error, so a typo fails loudly rather than silently dropping source+test.
func expandMulti(absRoot, root string, m Module, seen map[string]bool) (*Module, error) {
	name := strings.TrimSpace(m.Name)
	if name == "" {
		return nil, fmt.Errorf("multi-path module (paths %v) needs a \"name\" — it is the store key", m.Paths)
	}
	dirSet := map[string]bool{}
	for _, raw := range m.Paths {
		p := strings.Trim(filepath.ToSlash(raw), "/")
		if p == "" || p == "." {
			continue
		}
		var dirs []string
		if strings.ContainsAny(p, "*?[") {
			matches, _ := filepath.Glob(filepath.Join(absRoot, filepath.FromSlash(p)))
			sort.Strings(matches)
			dirs = matches
		} else {
			dirs = []string{filepath.Join(absRoot, filepath.FromSlash(p))}
		}
		for _, d := range dirs {
			fi, e := os.Stat(d)
			if e != nil || !fi.IsDir() {
				if !strings.ContainsAny(p, "*?[") {
					return nil, fmt.Errorf("module %q path %q not found under %s", name, raw, root)
				}
				continue
			}
			rel, e := filepath.Rel(absRoot, d)
			if e != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			if rel == "." || seen[rel] {
				continue
			}
			seen[rel] = true
			dirSet[rel] = true
		}
	}
	if len(dirSet) == 0 {
		return nil, nil
	}
	paths := make([]string, 0, len(dirSet))
	for d := range dirSet {
		paths = append(paths, d)
	}
	sort.Strings(paths)
	return &Module{Name: name, Paths: paths}, nil
}

// hasMarker reports whether dir directly contains any marker file — used
// only for the clipped check at the depth boundary.
func hasMarker(dir string, markers map[string]bool) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && markers[e.Name()] {
			return true
		}
	}
	return false
}
