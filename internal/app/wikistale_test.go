package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ADR 2026-07-27-wiki-off-by-default §4 — the hazard the default flip creates.
//
// A repo that gathered BEFORE the flip has real pages in <store>/wiki/. After
// it, `add` stops refreshing them and they go stale silently. A stale wiki is
// strictly worse than no wiki: it reads as current and cites lines that have
// moved. So `status` — the verb that answers "can I trust this store" — has to
// say so, and there has to be a way to remove it that is not "delete the whole
// graph".

// backdate makes the wiki older than the graph without a sleep, which is what a
// real upgrade does over days.
func backdate(t *testing.T, path string, d time.Duration) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	old := st.ModTime().Add(-d)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestStatusReportsStaleWiki(t *testing.T) {
	repo, storeRoot := wikiRepo(t, `{"name":"stalewiki","wiki":true}`)
	if out, code := runCLIIn(t, storeRoot, "add", repo); code != 0 {
		t.Fatalf("add failed: %s", out)
	}
	storeDir := filepath.Join(storeRoot, "stalewiki")

	// Fresh wiki: silence. A store that is fine must not nag.
	out, code := runCLIIn(t, storeRoot, "status", "--path", repo)
	if code != 0 {
		t.Fatalf("status failed: %s", out)
	}
	if strings.Contains(out, "NOT refreshed") {
		t.Errorf("status warned about a wiki that is current:\n%s", out)
	}

	backdate(t, filepath.Join(storeDir, "wiki", "index.md"), 72*time.Hour)

	out, code = runCLIIn(t, storeRoot, "status", "--path", repo)
	if code != 0 {
		t.Fatalf("status failed: %s", out)
	}
	if !strings.Contains(out, "NOT refreshed") {
		t.Errorf("status stayed silent about a wiki older than the graph:\n%s", out)
	}
	// Naming the problem without naming the remedies is half an answer.
	if !strings.Contains(out, "ctx-optimize wiki --delete") || !strings.Contains(out, "rebuild") {
		t.Errorf("the staleness line must name BOTH remedies:\n%s", out)
	}
}

// The trap that would make this feature unusable. store.New pre-creates
// graph/ wiki/ cards/ hooks/ in every store, so an EMPTY wiki/ is the normal
// state of every repo that has only ever gathered with the wiki off — i.e.
// every repo, by default. Keying the check off the DIRECTORY instead of
// wiki/index.md would print a staleness warning on essentially every store in
// existence, about a wiki nobody ever built.
func TestStatusSilentWhenWikiDirIsEmpty(t *testing.T) {
	repo, storeRoot := wikiRepo(t, `{"name":"nowiki"}`)
	if out, code := runCLIIn(t, storeRoot, "add", repo); code != 0 {
		t.Fatalf("add failed: %s", out)
	}
	dir := filepath.Join(storeRoot, "nowiki", "wiki")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("no wiki dir pre-created (%v) — the trap this pins cannot occur", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("expected an empty wiki dir after a default gather, got %d entries", len(entries))
	}
	backdate(t, dir, 72*time.Hour)

	out, code := runCLIIn(t, storeRoot, "status", "--path", repo)
	if code != 0 {
		t.Fatalf("status failed: %s", out)
	}
	if strings.Contains(out, "NOT refreshed") {
		t.Errorf("status warned about a wiki that was never built:\n%s", out)
	}
}

func TestWikiDeleteRemovesOnlyTheWiki(t *testing.T) {
	repo, storeRoot := wikiRepo(t, `{"name":"delwiki","wiki":true}`)
	if out, code := runCLIIn(t, storeRoot, "add", repo); code != 0 {
		t.Fatalf("add failed: %s", out)
	}
	if n := wikiPages(t, storeRoot, "delwiki"); n == 0 {
		t.Fatal("setup: expected a wiki to delete")
	}

	out, code := runCLIIn(t, storeRoot, "wiki", "--delete", "--path", repo)
	if code != 0 {
		t.Fatalf("wiki --delete failed: %s", out)
	}
	if n := wikiPages(t, storeRoot, "delwiki"); n != 0 {
		t.Errorf("wiki --delete left %d pages behind", n)
	}

	// The whole point of a SCOPED delete: the graph every verb reads survives.
	// If this ever regresses, users have `store delete` semantics under a name
	// that promises far less.
	if out, code := runCLIIn(t, storeRoot, "query", "Alpha", "--path", repo); code != 0 || !strings.Contains(out, "Alpha") {
		t.Errorf("the graph did not survive wiki --delete (code %d):\n%s", code, out)
	}

	// Reversible, and the message has to say so.
	if !strings.Contains(out, "ctx-optimize wiki") {
		t.Errorf("the delete message must name the way back:\n%s", out)
	}
	if out, code := runCLIIn(t, storeRoot, "wiki", "--path", repo); code != 0 {
		t.Fatalf("rebuild after delete failed: %s", out)
	}
	if n := wikiPages(t, storeRoot, "delwiki"); n == 0 {
		t.Error("`wiki` must rebuild a wiki that --delete removed")
	}
}

// The manifest fingerprints every store file on every gather and does NOT skip
// wiki/ — that is the ≈1.1s-per-gather tax a stale linux wiki keeps charging.
// Deleting the wiki has to actually stop the charge, which means dropping the
// entries, not just the files.
func TestWikiDeleteDropsManifestEntries(t *testing.T) {
	repo, storeRoot := wikiRepo(t, `{"name":"manwiki","wiki":true}`)
	if out, code := runCLIIn(t, storeRoot, "add", repo); code != 0 {
		t.Fatalf("add failed: %s", out)
	}
	manifest := filepath.Join(storeRoot, "manwiki", "manifest.json")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"wiki/`) {
		t.Fatal("setup: expected wiki entries in the manifest")
	}

	if out, code := runCLIIn(t, storeRoot, "wiki", "--delete", "--path", repo); code != 0 {
		t.Fatalf("wiki --delete failed: %s", out)
	}
	raw, err = os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"wiki/`) {
		t.Errorf("manifest still fingerprints deleted wiki pages:\n%s", raw)
	}
}

// Deleting nothing is not an error. Since the flip, most stores have no wiki at
// all, and a verb that exits non-zero for the normal state is a verb people
// stop trusting in scripts.
func TestWikiDeleteWithNoWikiSucceeds(t *testing.T) {
	repo, storeRoot := wikiRepo(t, `{"name":"emptywiki"}`)
	if out, code := runCLIIn(t, storeRoot, "add", repo); code != 0 {
		t.Fatalf("add failed: %s", out)
	}
	out, code := runCLIIn(t, storeRoot, "wiki", "--delete", "--path", repo)
	if code != 0 {
		t.Fatalf("wiki --delete on a store with no wiki must succeed, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "nothing to delete") {
		t.Errorf("expected a plain 'nothing to delete' line:\n%s", out)
	}
}
