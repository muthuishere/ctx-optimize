// Tree sync — the multi-module face of push/pull. A multi-module repo's
// store is a TREE of stores (the root store + one mirrored store per module,
// possibly nested); one remote URL carries the whole tree with the backend
// root mapped to the root store dir and every sub-store under its rel path.
// The remote gains one extra object, stores.json — the index of store rel
// paths — written LAST on push (like each store's manifest) so a racing
// reader never learns about a store before its files exist. A remote without
// stores.json is a plain single-store remote (full back-compat).
package remote

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/store"
)

const indexKey = "stores.json"

type treeIndex struct {
	Version int      `json:"version"`
	Stores  []string `json:"stores"` // rel paths under the root store; "" = the root store itself
}

// LocalStoreRels walks a store dir and returns every store found inside it
// ("" for dir itself when it is one), sorted — the push-side enumeration.
func LocalStoreRels(dir string) ([]string, error) {
	var rels []string
	var walk func(d, rel string) error
	walk = func(d, rel string) error {
		if _, err := os.Stat(filepath.Join(d, "graph")); err == nil {
			rels = append(rels, rel)
		}
		entries, err := os.ReadDir(d)
		if err != nil {
			if os.IsNotExist(err) && rel == "" {
				return fmt.Errorf("no store at %s — run `ctx-optimize add` first", d)
			}
			return nil
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			switch e.Name() {
			case "graph", "wiki", "cards", "hooks", "memory", "reflections":
				continue // store artifacts, not nested stores
			}
			child := e.Name()
			if rel != "" {
				child = rel + "/" + e.Name()
			}
			if err := walk(filepath.Join(d, e.Name()), child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(dir, ""); err != nil {
		return nil, err
	}
	sort.Strings(rels)
	return rels, nil
}

// PushTree pushes every store under rootStoreDir whose rel path is in rels
// (each against its prefixed keyspace), then writes the merged stores.json
// index last. Transferred paths come back rel-prefixed so the caller's
// report reads like the tree.
func PushTree(storeRoot, rootKey string, rels []string, b Backend) (*Result, error) {
	agg := &Result{}
	for _, rel := range rels {
		key := rootKey
		if rel != "" {
			key = rootKey + "/" + rel
		}
		s, err := store.Open(storeRoot, key)
		if err != nil {
			return nil, err
		}
		res, err := Push(s, WithPrefix(b, rel))
		if err != nil {
			return nil, fmt.Errorf("push %s: %w", orRoot(rel), err)
		}
		mergeResult(agg, res, rel)
	}
	idx, err := loadIndex(b)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, r := range idx.Stores {
		seen[r] = true
	}
	for _, r := range rels {
		if !seen[r] {
			idx.Stores = append(idx.Stores, r)
		}
	}
	sort.Strings(idx.Stores)
	data, err := json.Marshal(idx)
	if err != nil {
		return nil, err
	}
	if err := b.Put(indexKey, data); err != nil {
		return nil, fmt.Errorf("push %s: %w", indexKey, err)
	}
	return agg, nil
}

// PullTree pulls every indexed store whose rel path sits under prefix
// ("" = everything). A remote with no index is a single-store remote:
// prefix "" degrades to a plain pull, anything else is an error worth
// saying out loud.
func PullTree(storeRoot, rootKey, prefix string, b Backend) (*Result, error) {
	idx, err := loadIndex(b)
	if err != nil {
		return nil, err
	}
	if len(idx.Stores) == 0 {
		if prefix != "" {
			return nil, fmt.Errorf("remote has no stores.json index — it holds a single store; pull from the repo root instead")
		}
		s, err := store.Open(storeRoot, rootKey)
		if err != nil {
			return nil, err
		}
		return Pull(s, b)
	}
	agg := &Result{}
	matched := false
	for _, rel := range idx.Stores {
		if prefix != "" && rel != prefix && !strings.HasPrefix(rel, prefix+"/") {
			continue
		}
		matched = true
		key := rootKey
		if rel != "" {
			key = rootKey + "/" + rel
		}
		s, err := store.Open(storeRoot, key)
		if err != nil {
			return nil, err
		}
		res, err := Pull(s, WithPrefix(b, rel))
		if err != nil {
			return nil, fmt.Errorf("pull %s: %w", orRoot(rel), err)
		}
		mergeResult(agg, res, rel)
	}
	if !matched {
		return nil, fmt.Errorf("remote index has no store under %q (stores: %s)", prefix, strings.Join(idx.Stores, ", "))
	}
	return agg, nil
}

func loadIndex(b Backend) (*treeIndex, error) {
	data, err := b.Get(indexKey)
	if err == ErrNotFound {
		return &treeIndex{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	idx := &treeIndex{}
	if err := json.Unmarshal(data, idx); err != nil {
		return nil, fmt.Errorf("parse remote %s: %w", indexKey, err)
	}
	return idx, nil
}

func mergeResult(agg, res *Result, rel string) {
	for _, t := range res.Transferred {
		if rel != "" {
			t = rel + "/" + t
		}
		agg.Transferred = append(agg.Transferred, t)
	}
	agg.Skipped += res.Skipped
}

func orRoot(rel string) string {
	if rel == "" {
		return "(root store)"
	}
	return rel
}
