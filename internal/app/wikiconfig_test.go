package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #9. Onboarding chromium wrote 434,597 wiki pages / 1.7 GB into one
// directory, and Generate's stale-page cleanup re-reads that directory on every
// later gather (8s just to list it). A page cap was rejected — any cap is a
// number nobody can justify, and it yields a wiki that is both incomplete and
// still large. Whether a per-file wiki is wanted is the REPO's call.
//
// The two properties that make the key safe are pinned here: absent means
// enabled (so no existing repo silently loses its wiki), and "off" never means
// "unavailable" (the verb still builds a complete one).

func wikiPages(t *testing.T, storeRoot, key string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(storeRoot, key, "wiki"))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

func wikiRepo(t *testing.T, cfg string) (repo, storeRoot string) {
	t.Helper()
	repo, storeRoot = t.TempDir(), t.TempDir()
	writeRepoAt(t, repo, map[string]string{
		"a.go":                     "package a\n\nfunc Alpha() {}\n",
		".ctxoptimize/config.json": cfg,
	})
	return repo, storeRoot
}

func TestWikiFalseSkipsGenerationButVerbStillBuildsIt(t *testing.T) {
	repo, storeRoot := wikiRepo(t, `{"name":"wikioff","wiki":false}`)

	out, code := runCLIIn(t, storeRoot, "add", repo)
	if code != 0 {
		t.Fatalf("add failed: %s", out)
	}
	if n := wikiPages(t, storeRoot, "wikioff"); n != 0 {
		t.Errorf("wiki:false still generated %d pages", n)
	}
	// "Off" must never read as "unavailable" — the message has to name the verb.
	if !strings.Contains(out, "ctx-optimize wiki") {
		t.Errorf("the skip message must name the verb that builds it:\n%s", out)
	}

	// And that verb must ignore the config: it is an explicit request.
	if out, code := runCLIIn(t, storeRoot, "wiki", "--path", repo); code != 0 {
		t.Fatalf("wiki verb failed: %s", out)
	}
	if n := wikiPages(t, storeRoot, "wikioff"); n == 0 {
		t.Error("`ctx-optimize wiki` must build the wiki even when config says wiki:false — the key gates the GATHER, not the verb")
	}
}

// An existing repo whose config predates this key must behave exactly as before.
// This is the property that makes adding the key safe at all.
func TestWikiAbsentMeansEnabled(t *testing.T) {
	repo, storeRoot := wikiRepo(t, `{"name":"wikiabsent"}`)
	if out, code := runCLIIn(t, storeRoot, "add", repo); code != 0 {
		t.Fatalf("add failed: %s", out)
	}
	if n := wikiPages(t, storeRoot, "wikiabsent"); n == 0 {
		t.Error("a config with no `wiki` key must keep generating — absent is not false")
	}
}

func TestWikiTrueGenerates(t *testing.T) {
	repo, storeRoot := wikiRepo(t, `{"name":"wikion","wiki":true}`)
	if out, code := runCLIIn(t, storeRoot, "add", repo); code != 0 {
		t.Fatalf("add failed: %s", out)
	}
	if n := wikiPages(t, storeRoot, "wikion"); n == 0 {
		t.Error("wiki:true must generate")
	}
}

// init scaffolds the key explicitly, because a knob nobody can see is a knob
// nobody uses.
func TestInitScaffoldsTheWikiKey(t *testing.T) {
	repo, storeRoot := t.TempDir(), t.TempDir()
	writeRepoAt(t, repo, map[string]string{"a.go": "package a\n"})
	if out, code := runCLIIn(t, storeRoot, "init", "--path", repo); code != 0 {
		t.Fatalf("init failed: %s", out)
	}
	raw, err := os.ReadFile(filepath.Join(repo, ".ctxoptimize", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"wiki"`) {
		t.Errorf("init did not scaffold the wiki key — it is undiscoverable:\n%s", raw)
	}
}
