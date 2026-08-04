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
// ADR 2026-07-27-wiki-off-by-default (landed 2026-08-04) then inverted the
// DEFAULT. #9 made the wiki configurable and left absent meaning enabled, so no
// existing repo silently lost its wiki. That was half a fix: linux and chromium
// have no .ctxoptimize/config.json at all, so a config-only lever never reached
// the repos paying the most — 1,317.8s of a 1,475.4s linux gather (89.3%) for a
// byte-identical graph.
//
// So the pinned property MOVED: absent now means DISABLED. What did NOT move,
// and is the reason the flip is safe, is that "off" never means "unavailable" —
// `ctx-optimize wiki` still builds a complete wiki, and `--wiki` forces one for
// a single gather.

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

// The default. A silent config — and, identically, no config at all, which is
// the linux/chromium case — gathers no wiki. This inverts the #9-era pin
// deliberately: the guarantee it protected (no repo silently loses its wiki)
// was worth 89.3% of a cold gather, and it is replaced by the weaker but
// sufficient one below — nothing becomes unavailable, only unbuilt.
func TestWikiAbsentMeansDisabled(t *testing.T) {
	repo, storeRoot := wikiRepo(t, `{"name":"wikiabsent"}`)
	out, code := runCLIIn(t, storeRoot, "add", repo)
	if code != 0 {
		t.Fatalf("add failed: %s", out)
	}
	if n := wikiPages(t, storeRoot, "wikiabsent"); n != 0 {
		t.Errorf("a config with no `wiki` key generated %d pages — absent must mean off", n)
	}
	// Unbuilt, not unavailable: the skip line has to name the way back.
	if !strings.Contains(out, "ctx-optimize wiki") {
		t.Errorf("the skip message must name the verb that builds it:\n%s", out)
	}
	if out, code := runCLIIn(t, storeRoot, "wiki", "--path", repo); code != 0 {
		t.Fatalf("wiki verb failed: %s", out)
	}
	if n := wikiPages(t, storeRoot, "wikiabsent"); n == 0 {
		t.Error("`ctx-optimize wiki` must build a complete wiki regardless of the default")
	}
}

// --wiki is the per-run reversal of the new default, for the repos that have no
// config to edit — exactly the population the config-only lever of #9 missed.
func TestWikiFlagForcesGenerationWithNoConfig(t *testing.T) {
	repo, storeRoot := t.TempDir(), t.TempDir()
	writeRepoAt(t, repo, map[string]string{"a.go": "package a\n\nfunc Alpha() {}\n"})
	if out, code := runCLIIn(t, storeRoot, "add", repo, "--wiki"); code != 0 {
		t.Fatalf("add --wiki failed: %s", out)
	}
	if n := wikiPages(t, storeRoot, filepath.Base(repo)); n == 0 {
		t.Error("--wiki must generate a wiki when no config exists")
	}
}

// Precedence: the explicit per-run flag beats the committed config, in both
// directions. A user who types --wiki has said something more specific than a
// file checked in months ago.
func TestWikiFlagBeatsConfigFalse(t *testing.T) {
	repo, storeRoot := wikiRepo(t, `{"name":"wikibeat","wiki":false}`)
	if out, code := runCLIIn(t, storeRoot, "add", repo, "--wiki"); code != 0 {
		t.Fatalf("add --wiki failed: %s", out)
	}
	if n := wikiPages(t, storeRoot, "wikibeat"); n == 0 {
		t.Error("--wiki must beat a committed `wiki: false`")
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
