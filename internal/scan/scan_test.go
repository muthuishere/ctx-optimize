package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func mk(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func paths(mods []Module) []string {
	var out []string
	for _, m := range mods {
		out = append(out, m.Path)
	}
	return out
}

func TestScanFindsAllMarkersMultiLevel(t *testing.T) {
	root := t.TempDir()
	mk(t, root,
		"go.mod", // root marker: root is never a module
		"services/api/go.mod",
		"services/worker/package.json",
		"libs/a/b/c/Cargo.toml", // depth 4
		"apps/mobile/android/settings.gradle",
	)
	res, err := Scan(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"apps/mobile/android", "libs/a/b/c", "services/api", "services/worker"}
	got := paths(res.Modules)
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if res.Clipped {
		t.Fatal("nothing at the boundary — should not be clipped")
	}
}

func TestScanDepthBoundAndClipped(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "a/b/c/d/e/f/go.mod") // dir depth 6 > default 5
	res, err := Scan(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Modules) != 0 {
		t.Fatalf("beyond depth must not be found: %v", paths(res.Modules))
	}
	if !res.Clipped {
		t.Fatal("marker just past the boundary must set Clipped")
	}
	res, err = Scan(root, Options{Depth: 6})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Modules) != 1 || res.Modules[0].Path != "a/b/c/d/e/f" {
		t.Fatalf("deeper pass should find it: %v", paths(res.Modules))
	}
}

func TestScanPrunesNoiseAndExcludes(t *testing.T) {
	root := t.TempDir()
	mk(t, root,
		"node_modules/dep/package.json",
		"examples/demo/go.mod",
		"real/go.mod",
	)
	res, err := Scan(root, Options{Exclude: []string{"examples"}})
	if err != nil {
		t.Fatal(err)
	}
	got := paths(res.Modules)
	if len(got) != 1 || got[0] != "real" {
		t.Fatalf("got %v want [real]", got)
	}
}

func TestScanIncludeAndCustomMarkers(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "zigproj/build.zig", "plain/data.txt")
	res, err := Scan(root, Options{Markers: []string{"build.zig"}, Include: []string{"plain"}})
	if err != nil {
		t.Fatal(err)
	}
	got := paths(res.Modules)
	if len(got) != 2 || got[0] != "plain" || got[1] != "zigproj" {
		t.Fatalf("got %v", got)
	}
}

func TestScanChildCtxoptimizeIsAModule(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "sub/.ctxoptimize/config.json")
	res, err := Scan(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Modules) != 1 || res.Modules[0].Path != "sub" || res.Modules[0].Marker != ".ctxoptimize" {
		t.Fatalf("got %+v", res.Modules)
	}
}

func TestExpandGlobsAndDedupe(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "libs/a/go.mod", "libs/b/go.mod", "svc/package.json")
	mods, err := Expand(root, []Module{
		{Path: "libs/*"},
		{Path: "svc", Name: "the-service"},
		{Path: "libs/a"}, // duplicate: first declaration wins
	})
	if err != nil {
		t.Fatal(err)
	}
	got := paths(mods)
	want := []string{"libs/a", "libs/b", "svc"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	for _, m := range mods {
		if m.Path == "svc" && m.Name != "the-service" {
			t.Fatalf("explicit name lost: %+v", m)
		}
		if m.Path == "libs/a" && m.Name != "libs-a" {
			t.Fatalf("glob name wrong: %+v", m)
		}
	}
}

func TestExpandMissingExplicitModuleFails(t *testing.T) {
	root := t.TempDir()
	if _, err := Expand(root, []Module{{Path: "gone"}}); err == nil {
		t.Fatal("declared non-glob module missing on disk must error")
	}
}
