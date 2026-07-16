package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// InstallDir is an exact replace: files a previous version shipped but the
// current bundle doesn't must be removed, not left as stale orphans an agent
// could still read.
func TestInstallDirExactReplace(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "skills", skillName)

	// Fresh install into an absent dir works and lands the skill.
	if err := InstallDir(dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md missing after fresh install: %v", err)
	}

	// Plant orphans a hypothetical older version left behind.
	orphan := filepath.Join(dst, "references", "old-doc.md")
	if err := os.WriteFile(orphan, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(dst, "SKILL.md")
	if err := os.WriteFile(edited, []byte("locally edited"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallDir(dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan from a previous version survived the reinstall")
	}
	data, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "locally edited" {
		t.Fatal("reinstall must restore the bundled SKILL.md over local edits")
	}
	// No stage dirs left behind next to the target.
	entries, err := os.ReadDir(filepath.Dir(dst))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != skillName {
		t.Fatalf("stage leftovers in parent dir: %v", entries)
	}
}
