package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func mkModule(t *testing.T, root, rel string) {
	t.Helper()
	d := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitInit(t *testing.T, root, gitignore string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
}

func modulePaths(t *testing.T, root string) []string {
	t.Helper()
	res, err := Scan(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range res.Modules {
		got = append(got, m.Path)
	}
	return got
}

// Vendored trees are pruned by NAME because they are usually checked in, so
// .gitignore cannot help: chromium's third_party/ is tracked, and it produced
// 217 of the 241 "modules" the first chromium onboarding reported. Same reason
// `vendor` was already on the list.
func TestVendoredTreesAreNotModules(t *testing.T) {
	root := t.TempDir()
	mkModule(t, root, "third_party/some_dep")
	mkModule(t, root, "net/third_party/quiche/src/tool") // nested — pruned by name at any depth
	mkModule(t, root, "tools/real")

	got := modulePaths(t, root)
	if len(got) != 1 || got[0] != "tools/real" {
		t.Errorf("modules = %v, want only [tools/real]", got)
	}
}

// Build output is pruned because the REPO SAID SO, not because of its name.
// chromium's out/Default was proposed as a module purely because scan did not
// consult .gitignore while extraction did — two subsystems disagreeing about
// what is even in the repo. Hard-coding "out" would have hidden that, and would
// have broken any repo that keeps source in out/ (see the next test).
func TestGitignoredTreesAreNotModules(t *testing.T) {
	root := t.TempDir()
	mkModule(t, root, "out/Default")
	mkModule(t, root, "tools/real")
	gitInit(t, root, "/out/\n")

	got := modulePaths(t, root)
	if len(got) != 1 || got[0] != "tools/real" {
		t.Errorf("modules = %v, want only [tools/real] — a gitignored tree is not source", got)
	}
}

// The regression a hard-coded `out` prune would have caused: a repo that keeps
// real source in out/ and does NOT gitignore it must still have it discovered.
// This is why fixing the cause beat adding the name.
func TestNonIgnoredOutDirIsStillAModule(t *testing.T) {
	root := t.TempDir()
	mkModule(t, root, "out/service")
	gitInit(t, root, "node_modules/\n") // out/ deliberately NOT ignored

	got := modulePaths(t, root)
	if len(got) != 1 || got[0] != "out/service" {
		t.Errorf("modules = %v, want [out/service] — only the repo gets to say out/ is not source", got)
	}
}

// No git, or nothing ignored: the walk must still work. ignore.New returns nil
// there and that means "no extra filtering", never "filter everything".
func TestScanWorksWithoutGit(t *testing.T) {
	root := t.TempDir()
	mkModule(t, root, "tools/real")
	mkModule(t, root, "out/thing") // not gitignored because there is no git

	got := modulePaths(t, root)
	if len(got) != 2 {
		t.Errorf("modules = %v, want both (no git ⇒ no gitignore filtering)", got)
	}
}
