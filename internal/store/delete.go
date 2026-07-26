package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Delete removes ONE store — the derived graph for one module — and nothing
// else. It is the CLI counterpart of the dashboard's store delete, and it is
// deliberately not a wrapper around os.RemoveAll, because a store dir is not
// necessarily a leaf.
//
// A multi-module repo NESTS its module stores inside the root store:
//
//	~/ctxoptimize/reqsume/            ← root store (graph/, wiki/, manifest.json)
//	~/ctxoptimize/reqsume/e2e/        ← a DIFFERENT store
//	~/ctxoptimize/reqsume/regressiontest/
//
// So `RemoveAll(reqsume/)` destroys three stores while reporting one. Delete
// removes only the target's own artifacts and leaves nested stores standing,
// returning their keys so the caller can say what it kept. withNested opts into
// the recursive behaviour, which must always be an explicit request.
//
// Guards, in order: the key must sanitize to something; the target must not be
// the store root itself (that holds every repo's store plus audit.ndjson); and
// it must actually BE a store — a dir with a graph/ — so a typo'd key cannot
// delete an unrelated directory that happens to sit under the root.
func Delete(root, key string, withNested bool) (deleted string, keptNested []string, err error) {
	clean := SanitizeKeyPath(key)
	if clean == "" {
		return "", nil, fmt.Errorf("store delete: empty key")
	}
	// Sanitizing is right for CREATING a key and wrong for DELETING one: it
	// silently rewrote "repo/.." into "repo", i.e. deleted a store the caller
	// never named. For a destructive verb the key must survive cleaning
	// untouched — name the store exactly, or get an error.
	if trimmed := strings.Trim(key, "/"); clean != trimmed {
		return "", nil, fmt.Errorf("store delete: key %q is not a clean store key (it would be rewritten to %q) — name the store exactly", key, clean)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", nil, err
	}
	dir := filepath.Join(absRoot, filepath.FromSlash(clean))
	if dir == absRoot {
		return "", nil, fmt.Errorf("store delete: refusing to delete the store ROOT %s — it holds every repo's store and the audit log", absRoot)
	}
	if !strings.HasPrefix(dir, absRoot+string(filepath.Separator)) {
		return "", nil, fmt.Errorf("store delete: %q escapes the store root", key)
	}
	if _, serr := os.Stat(filepath.Join(dir, "graph")); serr != nil {
		return "", nil, fmt.Errorf("store delete: %s is not a store (no graph/ dir) — nothing removed", dir)
	}

	nested, err := nestedStores(dir)
	if err != nil {
		return "", nil, err
	}
	if withNested || len(nested) == 0 {
		if err := os.RemoveAll(dir); err != nil {
			return "", nil, err
		}
		return clean, nil, nil
	}

	// Keep every path on the way down to a nested store; remove the rest.
	// nested now includes deeply-nested stores, which is what we want here too:
	// keeping the chain to the deepest one keeps its parents by construction.
	keep := map[string]bool{}
	for _, n := range nested {
		for p := n; p != dir; p = filepath.Dir(p) {
			keep[p] = true
		}
	}
	var walkErr error
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if keep[p] {
			continue
		}
		if rerr := os.RemoveAll(p); rerr != nil && walkErr == nil {
			walkErr = rerr
		}
	}
	if walkErr != nil {
		return "", nil, walkErr
	}
	for _, n := range nested {
		rel, _ := filepath.Rel(absRoot, n)
		keptNested = append(keptNested, filepath.ToSlash(rel))
	}
	sort.Strings(keptNested)
	return clean, keptNested, nil
}

// nestedStores returns EVERY store dir strictly inside dir, at any depth.
//
// It used to SkipDir at the first store it found, which made the count wrong
// whenever modules nest (a repo declaring both `svcB` and `svcB/inner` reported
// 2 stores where there were 3). The delete was still correct — RemoveAll takes
// the whole subtree — but a confirmation prompt that UNDER-states the blast
// radius is the one direction that must never happen.
func nestedStores(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == dir {
			return nil
		}
		switch d.Name() {
		case "graph", "wiki", "reflections", "cards", "hooks", "metrics":
			return filepath.SkipDir // this store's own artifacts, never a module
		}
		if _, serr := os.Stat(filepath.Join(p, "graph")); serr == nil {
			out = append(out, p) // and KEEP walking: stores nest arbitrarily deep
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// PreviewDelete reports which nested module stores a Delete would KEEP, without
// touching anything — so a confirmation prompt can name the blast radius
// instead of asking the user to trust it.
func PreviewDelete(root, key string) ([]string, error) {
	clean := SanitizeKeyPath(key)
	if clean == "" {
		return nil, fmt.Errorf("store delete: empty key")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(absRoot, filepath.FromSlash(clean))
	nested, err := nestedStores(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range nested {
		rel, _ := filepath.Rel(absRoot, n)
		out = append(out, filepath.ToSlash(rel))
	}
	return out, nil
}
