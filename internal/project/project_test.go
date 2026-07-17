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

func TestRemoteCommands(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, `{"remote": {"push": "node .ctxoptimize/push.js", "pull": "sh .ctxoptimize/pull.sh"}}`)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.RemoteCommand("push") != "node .ctxoptimize/push.js" || c.RemoteCommand("pull") != "sh .ctxoptimize/pull.sh" {
		t.Fatalf("commands not parsed: %+v", c.Remote)
	}
	if c.Remote.Empty() {
		t.Fatal("declared remote must not be Empty")
	}
}

// Retired v0.3 forms (URL string / {type,url,credentials} object) still LOAD
// — an old committed config never breaks — but carry no commands.
func TestLegacyRemoteFormsLoadInert(t *testing.T) {
	for _, legacy := range []string{
		`{"remote": "s3://bucket/prefix"}`,
		`{"remote": {"type": "s3", "url": "s3://bucket/x", "credentials": {"access_key_id": "${KID}"}}}`,
	} {
		dir := t.TempDir()
		writeCfg(t, dir, legacy)
		c, err := Load(dir)
		if err != nil {
			t.Fatalf("legacy form must load: %v (%s)", err, legacy)
		}
		if c.RemoteCommand("push") != "" || c.RemoteCommand("pull") != "" {
			t.Fatalf("legacy form must be inert: %+v", c.Remote)
		}
	}
}

// An UNRELATED re-save of a config holding a legacy remote must drop the key
// cleanly — never emit a misleading "remote": {}.
func TestSaveDropsEmptyLegacyRemote(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, `{"name": "m", "remote": "s3://bucket/prefix"}`)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	c.Instructions = "CLAUDE" // the unrelated write
	if err := Save(dir, c); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(FileName)))
	if strings.Contains(string(data), "remote") {
		t.Fatalf("legacy remote must be dropped on re-save, got: %s", data)
	}
	if !strings.Contains(string(data), `"instructions": "CLAUDE"`) {
		t.Fatalf("unrelated write lost: %s", data)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := &Config{
		Name:     "my-module",
		Remote:   &Remote{Push: "node .ctxoptimize/push.js", Pull: "node .ctxoptimize/pull.js"},
		Adapters: []Adapter{{Name: "kafka", Run: "node hooks/kafka.js"}},
	}
	if err := Save(dir, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != in.Name || out.RemoteCommand("push") != in.Remote.Push ||
		out.RemoteCommand("pull") != in.Remote.Pull ||
		len(out.Adapters) != 1 || out.Adapters[0].Run != in.Adapters[0].Run {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(FileName)))
	if data[len(data)-1] != '\n' {
		t.Fatal("file not newline-terminated")
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
	// remote.example.md: ${NAME} is baked in; the env contract is documented.
	rt, err := os.ReadFile(filepath.Join(repo, Dir, "remote.example.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rt), "here: my-repo") {
		t.Fatal("remote.example.md missing ${NAME} substitution")
	}
	if !strings.Contains(string(rt), "CTX_DIRECTION") || !strings.Contains(string(rt), "CTX_STORE_DIR") {
		t.Fatal("remote.example.md must document the env contract")
	}
	// Transport samples land inert: present, and no remote declared yet.
	for _, fn := range []string{"push.js.sample", "pull.js.sample"} {
		if _, err := os.Stat(filepath.Join(repo, Dir, fn)); err != nil {
			t.Fatalf("%s not scaffolded: %v", fn, err)
		}
	}
	if c.RemoteCommand("push") != "" || c.RemoteCommand("pull") != "" {
		t.Fatal("scaffold must not declare remote commands")
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

func TestScaffoldWritesNoGitignore(t *testing.T) {
	repo := t.TempDir()
	if err := Scaffold(repo, "x"); err != nil {
		t.Fatal(err)
	}
	// Nothing secret lives in .ctxoptimize/ — the env ladder is repo-root
	// .env and ~/.config/ctx-optimize/.env, so no ignore file is scaffolded.
	if _, err := os.Stat(filepath.Join(repo, Dir, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf(".ctxoptimize/.gitignore must not be scaffolded: %v", err)
	}
	// A user's own .gitignore there is left alone.
	p := filepath.Join(repo, Dir, ".gitignore")
	if err := os.WriteFile(p, []byte("custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Scaffold(repo, "x"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); string(data) != "custom\n" {
		t.Fatalf("scaffold touched a user .gitignore: %q", data)
	}
}

// The literal-credential gate fires at Load — a committed password never
// survives to a verb, and the error carries the skeleton only.
func TestLoadRefusesLiteralCreds(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, `{"sources": ["OK_URL", "postgres://user:sekretpass@host/db"]}`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected literal-credential error")
	}
	if strings.Contains(err.Error(), "sekretpass") {
		t.Fatalf("error echoes the literal secret: %v", err)
	}
	if !strings.Contains(err.Error(), "sources[1]") {
		t.Fatalf("error should name the entry index: %v", err)
	}
	// Var-shaped entries load fine.
	writeCfg(t, dir, `{"sources": ["OK_URL", "postgres://$U:$P@host/db", "./api/spec.yaml"]}`)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Sources) != 3 {
		t.Fatalf("sources = %v", c.Sources)
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

func TestPointerTargetsForNarrowsToExistingFiles(t *testing.T) {
	dir := t.TempDir()
	// Neither file exists: default creates both.
	got, err := PointerTargetsFor(dir, "")
	if err != nil || strings.Join(got, " ") != "CLAUDE.md AGENTS.md" {
		t.Fatalf("empty repo: %v, %v", got, err)
	}
	// Only AGENTS.md exists: default must not drop a CLAUDE.md in.
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = PointerTargetsFor(dir, "ALL")
	if err != nil || strings.Join(got, " ") != "AGENTS.md" {
		t.Fatalf("AGENTS-only repo: %v, %v", got, err)
	}
	// Explicit setting overrides existence: the author asked by name.
	got, err = PointerTargetsFor(dir, "CLAUDE")
	if err != nil || strings.Join(got, " ") != "CLAUDE.md" {
		t.Fatalf("explicit CLAUDE: %v, %v", got, err)
	}
	// NONE stays nil.
	got, err = PointerTargetsFor(dir, "NONE")
	if err != nil || got != nil {
		t.Fatalf("NONE: %v, %v", got, err)
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
	if !strings.Contains(s, "ctx-optimize up") {
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
