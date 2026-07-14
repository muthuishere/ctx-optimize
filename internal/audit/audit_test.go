package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendAndList(t *testing.T) {
	root := t.TempDir()
	if err := Append(root, Line{Actor: "cli", Action: "config.set", Target: "/x/config.json", BeforeHash: "aa", AfterHash: "bb"}); err != nil {
		t.Fatal(err)
	}
	if err := Append(root, Line{Actor: "dashboard", Action: "store.delete", Target: "myrepo"}); err != nil {
		t.Fatal(err)
	}
	lines, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	if lines[0].Actor != "cli" || lines[0].Action != "config.set" || lines[0].TS == "" {
		t.Fatalf("first line: %+v", lines[0])
	}
	if lines[1].Actor != "dashboard" || lines[1].Target != "myrepo" {
		t.Fatalf("second line: %+v", lines[1])
	}

	// Raw file: keys sorted (struct field order is alphabetical).
	raw, err := os.ReadFile(Path(root))
	if err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(string(raw), "\n", 2)[0]
	want := `{"action":"config.set","actor":"cli","after_hash":"bb","before_hash":"aa","target":"/x/config.json","ts":"`
	if !strings.HasPrefix(first, want) {
		t.Fatalf("line not sorted-field:\n%s", first)
	}
}

func TestListAbsentIsEmptyAndCreatesNothing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "never-created")
	lines, err := List(root)
	if err != nil || len(lines) != 0 {
		t.Fatalf("absent audit: %v %v", lines, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("List must not create the store root")
	}
}

func TestFileHash(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.json")
	if FileHash(p) != "" {
		t.Fatal("missing file must hash to empty")
	}
	os.WriteFile(p, []byte("hello"), 0o644)
	h := FileHash(p)
	if len(h) != 64 {
		t.Fatalf("want sha256 hex, got %q", h)
	}
}
