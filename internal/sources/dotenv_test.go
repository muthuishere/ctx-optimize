package sources

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseDotenvTable(t *testing.T) {
	cases := []struct {
		line    string
		wantKey string
		wantVal string
		skip    bool // line should produce nothing
	}{
		{`KEY="postgres://u:p@h/db?a=b#c"`, "KEY", "postgres://u:p@h/db?a=b#c", false},
		{`KEY='single'`, "KEY", "single", false},
		{`export FOO=bar`, "FOO", "bar", false},
		{`KEY=val#comment`, "KEY", "val#comment", false}, // no space → value
		{`KEY=val #comment`, "KEY", "val", false},        // space → comment
		{`KEY = spaced `, "KEY", "spaced", false},        // ws around = and value
		{"KEY=crlf\r", "KEY", "crlf", false},             // CRLF
		{"\uFEFFBOM=first", "BOM", "first", false},       // UTF-8 BOM
		{`EQ=a=b=c`, "EQ", "a=b=c", false},               // = in value
		{`URL=http://h/x?a=1&b=2`, "URL", "http://h/x?a=1&b=2", false},
		{`EMPTY=`, "EMPTY", "", false},
		{`# whole line comment`, "", "", true},
		{`   `, "", "", true},
		{`NOEQUALS`, "", "", true},
		{`Q="he said 'hi'"`, "Q", "he said 'hi'", false}, // nested other quote
		{`HALF="unclosed`, "HALF", `"unclosed`, false},   // unmatched quote kept literal
	}
	for _, c := range cases {
		got := ParseDotenv(c.line)
		if c.skip {
			if len(got) != 0 {
				t.Errorf("%q: expected skip, got %v", c.line, got)
			}
			continue
		}
		if v, ok := got[c.wantKey]; !ok || v != c.wantVal {
			t.Errorf("%q: got %v, want %s=%q", c.line, got, c.wantKey, c.wantVal)
		}
	}
}

func TestParseDotenvMultiline(t *testing.T) {
	body := "\uFEFFA=1\r\n# c\r\nexport B = \"two\" \r\n\r\nC=x #y\n"
	got := ParseDotenv(body)
	want := map[string]string{"A": "1", "B": "two", "C": "x"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("extra keys: %v", got)
	}
}

// The ladder: process env → .ctxoptimize/.env → root .env, specific over
// general, with the origin reported name-only.
func TestResolverLadder(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".ctxoptimize"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, ".ctxoptimize", ".env"), []byte("OURS=from-ours\nSHADOWED=from-ours\nENV_WINS=from-ours\n"), 0o600)
	os.WriteFile(filepath.Join(repo, ".env"), []byte("ROOTS=from-root\nSHADOWED=from-root\nENV_WINS=from-root\n"), 0o600)
	t.Setenv("ENV_WINS", "from-env")

	r := NewResolver(repo)
	cases := []struct{ name, wantVal, wantOrigin string }{
		{"ENV_WINS", "from-env", OriginEnv},     // real env always wins
		{"OURS", "from-ours", OriginOurEnv},     // ours over root
		{"SHADOWED", "from-ours", OriginOurEnv}, // specific over general
		{"ROOTS", "from-root", OriginRootEnv},   // root .env zero-setup lane
	}
	for _, c := range cases {
		v, origin, ok := r.Lookup(c.name)
		if !ok || v != c.wantVal || origin != c.wantOrigin {
			t.Errorf("Lookup(%s) = (%q, %q, %v), want (%q, %q, true)", c.name, v, origin, ok, c.wantVal, c.wantOrigin)
		}
	}
	if _, _, ok := r.Lookup("NOPE_NOT_SET"); ok {
		t.Error("Lookup(NOPE_NOT_SET) = ok, want miss")
	}
}

// The gitignore trap (spike, confirmed with real git): a .env committed
// BEFORE the scaffolded ignore stays tracked — ls-files --error-unmatch is
// the detection. Not a repo / not tracked / no git ⇒ silent no-op.
func TestTrackedEnvFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(repo, ".env"), []byte("X=1\n"), 0o600)
	if got := TrackedEnvFiles(repo); len(got) != 0 {
		t.Errorf("untracked .env reported tracked: %v", got)
	}
	git("add", ".env")
	git("commit", "-q", "-m", "oops: commit the env")
	got := TrackedEnvFiles(repo)
	if len(got) != 1 || got[0] != ".env" {
		t.Errorf("TrackedEnvFiles = %v, want [.env]", got)
	}
	// Non-repo: silent no-op even with the file present.
	plain := t.TempDir()
	os.WriteFile(filepath.Join(plain, ".env"), []byte("X=1\n"), 0o600)
	if got := TrackedEnvFiles(plain); len(got) != 0 {
		t.Errorf("non-repo reported tracked: %v", got)
	}
}
