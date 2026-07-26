package store

import (
	"os"
	"path/filepath"
	"testing"
)

// A store dir is NOT necessarily a leaf: a multi-module repo nests its module
// stores inside the root store (~/ctxoptimize/reqsume/ contains reqsume/e2e/
// and reqsume/regressiontest/). So RemoveAll on a root store destroys three
// stores while reporting one. These tests pin that Delete removes exactly the
// store it names.
func mkStore(t *testing.T, root, key string) string {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Join(dir, "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph", "nodes.ndjson"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDeleteKeepsNestedModuleStores(t *testing.T) {
	root := t.TempDir()
	rootStore := mkStore(t, root, "reqsume")
	e2e := mkStore(t, root, "reqsume/e2e")
	reg := mkStore(t, root, "reqsume/regressiontest")
	sibling := mkStore(t, root, "other-repo")

	deleted, kept, err := Delete(root, "reqsume", false)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != "reqsume" {
		t.Errorf("deleted = %q", deleted)
	}
	if len(kept) != 2 {
		t.Errorf("kept = %v, want both nested stores named", kept)
	}
	// The named store's own artifacts are gone…
	if _, err := os.Stat(filepath.Join(rootStore, "graph")); !os.IsNotExist(err) {
		t.Error("the target store's graph/ survived")
	}
	if _, err := os.Stat(filepath.Join(rootStore, "manifest.json")); !os.IsNotExist(err) {
		t.Error("the target store's manifest survived")
	}
	// …and every other store is intact.
	for _, keepDir := range []string{e2e, reg, sibling} {
		if _, err := os.Stat(filepath.Join(keepDir, "graph", "nodes.ndjson")); err != nil {
			t.Errorf("collateral damage: %s was destroyed (%v)", keepDir, err)
		}
	}
}

func TestDeleteWithNestedRemovesEverything(t *testing.T) {
	root := t.TempDir()
	mkStore(t, root, "reqsume")
	mkStore(t, root, "reqsume/e2e")
	sibling := mkStore(t, root, "other-repo")

	if _, _, err := Delete(root, "reqsume", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "reqsume")); !os.IsNotExist(err) {
		t.Error("--with-nested must remove the whole subtree")
	}
	if _, err := os.Stat(filepath.Join(sibling, "graph")); err != nil {
		t.Error("a sibling store is never in scope")
	}
}

// The store root holds EVERY repo's store plus audit.ndjson. Nothing may
// resolve to it.
func TestDeleteRefusesTheStoreRoot(t *testing.T) {
	root := t.TempDir()
	mkStore(t, root, "repo")
	for _, key := range []string{"", ".", "/", "..", "../..", "repo/.."} {
		if _, _, err := Delete(root, key, true); err == nil {
			t.Errorf("Delete(%q) succeeded — the store root must be unreachable", key)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "repo", "graph")); err != nil {
		t.Error("a refused delete must not have touched anything")
	}
}

// A typo'd key must not delete an unrelated directory that happens to live
// under the store root.
func TestDeleteRefusesNonStoreDirs(t *testing.T) {
	root := t.TempDir()
	notAStore := filepath.Join(root, "cache")
	if err := os.MkdirAll(notAStore, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notAStore, "keep.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Delete(root, "cache", true); err == nil {
		t.Error("a dir with no graph/ is not a store and must not be deleted")
	}
	if _, err := os.Stat(filepath.Join(notAStore, "keep.bin")); err != nil {
		t.Error("the refused target was modified")
	}
	if _, _, err := Delete(root, "never-existed", true); err == nil {
		t.Error("a missing store must be an error, not a silent success")
	}
}

func TestPreviewDeleteTouchesNothing(t *testing.T) {
	root := t.TempDir()
	mkStore(t, root, "reqsume")
	mkStore(t, root, "reqsume/e2e")

	got, err := PreviewDelete(root, "reqsume")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "reqsume/e2e" {
		t.Errorf("PreviewDelete = %v, want [reqsume/e2e]", got)
	}
	if _, err := os.Stat(filepath.Join(root, "reqsume", "graph")); err != nil {
		t.Error("preview must not delete anything")
	}
}
