package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCfg(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(FileName)), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAbsentIsEmpty(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.Remote != nil || len(c.Adapters) != 0 || c.Name != "" {
		t.Fatalf("expected empty config, got %+v", c)
	}
}

func TestRemoteStringForm(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, `{"remote": "s3://bucket/prefix"}`)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Remote == nil || c.Remote.URL != "s3://bucket/prefix" {
		t.Fatalf("string remote not parsed: %+v", c.Remote)
	}
}

func TestRemoteObjectForm(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, `{
	  "remote": {"type": "s3", "url": "s3://bucket/${REPO}",
	             "credentials": {"access_key_id": "${KID}", "region": "auto"}}
	}`)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := c.Remote
	if r == nil || r.Type != "s3" || r.Credentials["access_key_id"] != "${KID}" {
		t.Fatalf("object remote not parsed: %+v", r)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := &Config{
		Name: "my-module",
		Remote: &Remote{Type: "s3", URL: "s3://bucket/prefix",
			Credentials: map[string]string{"access_key_id": "${KID}"}},
		Adapters: []Adapter{{Name: "kafka", Run: "node hooks/kafka.js"}},
	}
	if err := Save(dir, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != in.Name || out.Remote.URL != in.Remote.URL ||
		out.Remote.Credentials["access_key_id"] != "${KID}" ||
		len(out.Adapters) != 1 || out.Adapters[0].Run != in.Adapters[0].Run {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(FileName)))
	if data[len(data)-1] != '\n' {
		t.Fatal("file not newline-terminated")
	}
}

// A URL-only remote marshals back to the simple string form.
func TestSaveKeepsSimpleFormSimple(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, &Config{Remote: &Remote{URL: "file:///x"}}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(FileName)))
	if !strings.Contains(string(data), `"remote": "file:///x"`) {
		t.Fatalf("expected string form: %s", data)
	}
}

func TestResolve(t *testing.T) {
	t.Setenv("CTX_T_URL", "bucket-a")
	t.Setenv("CTX_T_KEY", "resolved-key")
	r := Remote{URL: "s3://${CTX_T_URL}/p",
		Credentials: map[string]string{"access_key_id": "${CTX_T_KEY}", "region": "auto"}}
	got, err := r.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "s3://bucket-a/p" || got.Credentials["access_key_id"] != "resolved-key" ||
		got.Credentials["region"] != "auto" {
		t.Fatalf("resolve wrong: %+v", got)
	}
	// Original untouched — placeholders stay in the config.
	if r.URL != "s3://${CTX_T_URL}/p" {
		t.Fatal("Resolve mutated the source")
	}
}

func TestResolveUnsetVarFailsNamingIt(t *testing.T) {
	r := Remote{URL: "s3://b", Credentials: map[string]string{"secret_access_key": "${CTX_T_DEFINITELY_UNSET}"}}
	_, err := r.Resolve()
	if err == nil || !strings.Contains(err.Error(), "CTX_T_DEFINITELY_UNSET") {
		t.Fatalf("expected error naming the unset var, got %v", err)
	}
}

func TestDiscoverAdapters(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, filepath.FromSlash(AdaptersDir))
	os.MkdirAll(dir, 0o755)
	for _, f := range []string{"kafka.js", "pg.py", "logs.sh", "README.md", "example.js.sample", ".hidden.js", "data.json"} {
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
	}
	got, err := DiscoverAdapters(repo)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"kafka": "node " + AdaptersDir + "/kafka.js",
		"logs":  "sh " + AdaptersDir + "/logs.sh",
		"pg":    "python3 " + AdaptersDir + "/pg.py",
	}
	if len(got) != len(want) {
		t.Fatalf("discovered %d adapters, want %d: %+v", len(got), len(want), got)
	}
	for _, a := range got {
		if want[a.Name] != a.Run {
			t.Fatalf("adapter %s run = %q, want %q", a.Name, a.Run, want[a.Name])
		}
	}
}

func TestDiscoverAdaptersNoDir(t *testing.T) {
	got, err := DiscoverAdapters(t.TempDir())
	if err != nil || got != nil {
		t.Fatalf("absent dir should be (nil, nil), got %v, %v", got, err)
	}
}

func TestScaffold(t *testing.T) {
	repo := t.TempDir()
	if err := Scaffold(repo, "my-repo"); err != nil {
		t.Fatal(err)
	}
	c, err := Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "my-repo" {
		t.Fatalf("scaffolded name = %q", c.Name)
	}
	// Template is inert — discovery must not arm it.
	got, _ := DiscoverAdapters(repo)
	if len(got) != 0 {
		t.Fatalf("template should be inert: %+v", got)
	}
	// Idempotent: re-scaffold never clobbers an edited config.
	c.Name = "edited"
	Save(repo, c)
	if err := Scaffold(repo, "my-repo"); err != nil {
		t.Fatal(err)
	}
	c2, _ := Load(repo)
	if c2.Name != "edited" {
		t.Fatal("scaffold overwrote existing config")
	}
}

func TestLoadGarbageFails(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, "{nope")
	if _, err := Load(dir); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestEnsureAgentPointer(t *testing.T) {
	dir := t.TempDir()
	both, err := PointerTargets("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# My repo\nrules here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, err := EnsureAgentPointer(dir, "mymod", 0, both)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 {
		t.Fatalf("expected CLAUDE.md+AGENTS.md written, got %v", written)
	}
	ag, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(string(ag), "rules here") || !strings.Contains(string(ag), "ctxoptimize/mymod") {
		t.Fatalf("AGENTS.md lost content or missing block:\n%s", ag)
	}
	// second run must be a no-op (idempotent)
	written2, err := EnsureAgentPointer(dir, "mymod", 0, both)
	if err != nil {
		t.Fatal(err)
	}
	if len(written2) != 0 {
		t.Fatalf("second run rewrote files: %v", written2)
	}
	// changed name → block replaced in place, exactly one block
	if _, err := EnsureAgentPointer(dir, "renamed", 0, both); err != nil {
		t.Fatal(err)
	}
	ag, _ = os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if strings.Count(string(ag), "ctx-optimize:begin") != 1 || !strings.Contains(string(ag), "ctxoptimize/renamed") {
		t.Fatalf("block not replaced in place:\n%s", ag)
	}
}

// agents.type narrows which instruction files init may touch; a typo must
// error, never silently write.
func TestPointerTargets(t *testing.T) {
	for in, want := range map[string]string{
		"":       "CLAUDE.md AGENTS.md",
		"both":   "CLAUDE.md AGENTS.md",
		"CLAUDE": "CLAUDE.md",
		"agents": "AGENTS.md",
		"all":    "CLAUDE.md AGENTS.md",
	} {
		got, err := PointerTargets(in)
		if err != nil || strings.Join(got, " ") != want {
			t.Fatalf("PointerTargets(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := PointerTargets("CURSOR"); err == nil {
		t.Fatal("unknown agents.type must be refused")
	}
	dir := t.TempDir()
	written, err := EnsureAgentPointer(dir, "m", 0, []string{"AGENTS.md"})
	if err != nil || len(written) != 1 || written[0] != "AGENTS.md" {
		t.Fatalf("targeted write: %v %v", written, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatal("CLAUDE.md must not be created when agents.type = AGENTS")
	}
}

func TestGlobalPointerLifecycle(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")
	// pre-existing user content that must survive install/uninstall.
	const user = "# My global instructions\n\nAlways answer in English.\n"
	if err := os.WriteFile(p, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}

	// install writes the block, appended after the user's content.
	written, err := EnsureGlobalPointer([]string{p})
	if err != nil || len(written) != 1 {
		t.Fatalf("EnsureGlobalPointer: %v written=%v", err, written)
	}
	got, _ := os.ReadFile(p)
	s := string(got)
	if !strings.Contains(s, user) {
		t.Fatal("user content lost")
	}
	if !strings.Contains(s, globalBegin) || !strings.Contains(s, "knowledge graph before grep") {
		t.Fatal("global block missing")
	}
	if !strings.Contains(s, "ctx-optimize init && ctx-optimize add .") {
		t.Fatal("create-config guidance missing from global block")
	}

	// idempotent: second call reports no change.
	if w, err := EnsureGlobalPointer([]string{p}); err != nil || len(w) != 0 {
		t.Fatalf("second install not idempotent: %v changed=%v", err, w)
	}

	// uninstall strips only the block, keeps the user's content.
	removed, err := RemoveGlobalPointer([]string{p})
	if err != nil || len(removed) != 1 {
		t.Fatalf("RemoveGlobalPointer: %v removed=%v", err, removed)
	}
	after, _ := os.ReadFile(p)
	if strings.Contains(string(after), globalBegin) {
		t.Fatal("global markers not removed")
	}
	if !strings.Contains(string(after), "Always answer in English.") {
		t.Fatalf("user content lost on uninstall:\n%s", after)
	}
}

func TestGlobalPointerCreatesMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "AGENTS.md") // dir does not exist
	written, err := EnsureGlobalPointer([]string{p})
	if err != nil || len(written) != 1 {
		t.Fatalf("EnsureGlobalPointer on missing file: %v written=%v", err, written)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), globalEnd) {
		t.Fatal("block not written to freshly created file")
	}
}

func TestRemoveGlobalPointerNoMarkersNoop(t *testing.T) {
	p := filepath.Join(t.TempDir(), "CLAUDE.md")
	os.WriteFile(p, []byte("# just user content\n"), 0o644)
	removed, err := RemoveGlobalPointer([]string{p})
	if err != nil || len(removed) != 0 {
		t.Fatalf("expected no-op, got removed=%v err=%v", removed, err)
	}
}
