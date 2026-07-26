package scan

import (
	"os"
	"path/filepath"
	"testing"
)

// Found by onboarding chromium (2026-07-26): scan reported 241 "modules", 217
// of them (90%) under third_party/ and one being out/Default, the GN build
// output. Vendored and generated trees are not modules — the same reason
// vendor/, dist/, build/ and target/ were already pruned; third_party and out
// are just the names Google-style repos use.
func TestVendoredAndBuildOutputAreNotModules(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"third_party/some_dep",            // top-level vendored
		"net/third_party/quiche/src/tool", // NESTED vendored — pruned by name, any depth
		"out/Default",                     // GN build output
		"tools/real",                      // a genuine module
	} {
		d := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Scan(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range res.Modules {
		got = append(got, m.Path)
	}
	if len(got) != 1 || got[0] != "tools/real" {
		t.Errorf("modules = %v, want only [tools/real] — vendored and build-output trees must not become modules", got)
	}
}
