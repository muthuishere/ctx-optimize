package ignore

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitignoreMatcher(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte("generated/\n*.secret.yaml\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "generated", "deep"), 0o755)
	os.WriteFile(filepath.Join(root, "generated", "deep", "x.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "prod.secret.yaml"), []byte("k: v"), 0o644)
	os.WriteFile(filepath.Join(root, "kept.md"), []byte("# ok"), 0o644)

	m := New(root)
	if m == nil {
		t.Fatal("expected a matcher in a git repo with ignored files")
	}
	if !m("generated/deep/x.md") || !m("prod.secret.yaml") {
		t.Fatal("gitignored paths not matched")
	}
	if m("kept.md") {
		t.Fatal("tracked-eligible file wrongly ignored")
	}
	if New(t.TempDir()) != nil {
		t.Fatal("non-repo must return nil matcher")
	}
}
