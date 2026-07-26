package store

import (
	"os"
	"path/filepath"
	"testing"
)

// A store dir is NOT necessarily a leaf: a multi-module repo nests its module
// stores inside the root store (~/ctxoptimize/reqsume/ contains reqsume/e2e/
// and reqsume/regressiontest/). These tests pin what Delete touches in both
// directions — it must take the named repo's whole store set by default, and it
// must never reach a store the caller did not name.
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

// The DEFAULT for a repo's store is all of it: a multi-module repo's module
// stores nest inside its root store and are that same repo's data. Reporting
// `deleted store "chromium"` while leaving 33 chromium stores on disk is a lie
// by omission — measured on a real chromium checkout. What must stay impossible
// is touching a store the caller did not name.
func TestDeleteTakesTheReposOwnModuleStoresButNeverASibling(t *testing.T) {
	root := t.TempDir()
	mkStore(t, root, "chromium")
	mkStore(t, root, "chromium/third_party/node")
	mkStore(t, root, "chromium/third_party/node/deps") // nested TWO deep
	mkStore(t, root, "chromium/tools/grit")
	sibling := mkStore(t, root, "other-repo")

	if _, _, err := Delete(root, "chromium", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "chromium")); !os.IsNotExist(err) {
		t.Error("the repo's whole store set must go — module stores are the same repo's data")
	}
	if _, err := os.Stat(filepath.Join(sibling, "graph", "nodes.ndjson")); err != nil {
		t.Errorf("a SIBLING repo's store is never in scope: %v", err)
	}
}

// The --keep-nested path: remove the named store's own artifacts and leave the
// nested module stores standing. This is the shape that must NOT be the default
// (see the test above), but it has to work exactly when asked for, since a
// naive RemoveAll cannot express it at all.
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
		t.Error("the default (withNested) must remove the whole subtree")
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

// The preview feeds a confirmation prompt, so under-counting is the one
// direction that must never happen. It used to SkipDir at the first store found,
// reporting 2 stores for a repo that had 3 (a module nested inside a module).
func TestPreviewDeleteCountsStoresAtEveryDepth(t *testing.T) {
	root := t.TempDir()
	mkStore(t, root, "reqsume")
	mkStore(t, root, "reqsume/e2e")
	mkStore(t, root, "reqsume/e2e/fixtures") // two deep — was invisible
	mkStore(t, root, "other-repo")           // never in scope

	got, err := PreviewDelete(root, "reqsume")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"reqsume/e2e", "reqsume/e2e/fixtures"}
	if len(got) != len(want) {
		t.Fatalf("PreviewDelete = %v, want %v — a prompt must never under-state the blast radius", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if _, err := os.Stat(filepath.Join(root, "reqsume", "graph")); err != nil {
		t.Error("preview must not delete anything")
	}
}
