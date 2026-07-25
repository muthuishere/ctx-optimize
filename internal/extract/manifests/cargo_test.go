package manifests

import (
	"strings"
	"testing"
)

const fixtureCargo = `[package]
name = "demo"
version = "0.1.0"
description = """
Mentions [dependencies] and phantom = "1.0" inside prose.
"""

[dependencies]
anyhow = "1.0"
serde = { version = "1", features = ["derive", "rc"] }
localdep = { path = "../local" }
wsdep = { workspace = true }

[dependencies.tokio]
version = "1.36"
features = ["full"]

[dev-dependencies]
criterion = "0.5"

[build-dependencies]
cc = "1.0"

[target."cfg(windows)".dependencies]
winapi = "0.3"

[target.'cfg(unix)'.dev-dependencies]
nix = "0.28"

[workspace.dependencies]
shared = "2.0"
`

func TestCargoDependencies(t *testing.T) {
	b := extractFixture(t, map[string]string{"Cargo.toml": fixtureCargo})
	rel := "Cargo.toml"

	n := nodeByID(b, "dep:crates/anyhow")
	if n == nil {
		t.Fatal("missing dep:crates/anyhow")
	}
	if n.Kind != "dependency" || n.FileType != "manifest" || n.Label != "anyhow" ||
		n.Metadata["ecosystem"] != "crates" {
		t.Fatalf("crates dep node shape wrong: %+v", n)
	}

	mustDecl(t, b, rel, "dep:crates/anyhow|1.0|dependencies")
	mustDecl(t, b, rel, "dep:crates/serde|1|dependencies") // inline table
	mustDecl(t, b, rel, "dep:crates/localdep|path:../local|dependencies")
	mustDecl(t, b, rel, "dep:crates/wsdep|workspace|dependencies")
	mustDecl(t, b, rel, "dep:crates/tokio|1.36|dependencies") // sub-table form
	mustDecl(t, b, rel, "dep:crates/criterion|0.5|dev-dependencies")
	mustDecl(t, b, rel, "dep:crates/cc|1.0|build-dependencies")
	mustDecl(t, b, rel, "dep:crates/winapi|0.3|target-dependencies")
	mustDecl(t, b, rel, "dep:crates/nix|0.28|target-dev-dependencies")
	mustDecl(t, b, rel, "dep:crates/shared|2.0|workspace-dependencies")

	if got := len(declared(b, rel)); got != 10 {
		t.Errorf("declarations = %d, want 10:\n  %s", got, strings.Join(declared(b, rel), "\n  "))
	}
	// Neither the [package] table's own fields nor the prose block are deps.
	for _, bad := range []string{"dep:crates/name", "dep:crates/version",
		"dep:crates/description", "dep:crates/phantom", "dep:crates/features"} {
		if nodeByID(b, bad) != nil {
			t.Errorf("non-dependency became a dep node: %s", bad)
		}
	}
}

func TestCargoScopeClasses(t *testing.T) {
	for scope, want := range map[string]string{
		"dependencies":                 "runtime",
		"dev-dependencies":             "dev",
		"build-dependencies":           "build",
		"target-dependencies":          "runtime",
		"target-dev-dependencies":      "dev",
		"workspace-dependencies":       "runtime",
		"workspace-build-dependencies": "build",
	} {
		if got := scopeClass(scope); got != want {
			t.Errorf("scopeClass(%q) = %q, want %q", scope, got, want)
		}
	}
}

// Features arrays spanning lines inside an inline table are legal TOML and the
// nastiest real shape found in the corpus (crabbyavif) — it must not derail the
// following declarations.
func TestCargoInlineTableWithMultiLineFeatures(t *testing.T) {
	b := extractFixture(t, map[string]string{"Cargo.toml": `[dependencies]
image = { version = "0.24", default-features = false, features = [
    "png",
    "jpeg",
] }
after = "1.2"
`})
	mustDecl(t, b, "Cargo.toml", "dep:crates/image|0.24|dependencies")
	mustDecl(t, b, "Cargo.toml", "dep:crates/after|1.2|dependencies")
	if got := len(declared(b, "Cargo.toml")); got != 2 {
		t.Errorf("declarations = %d, want 2:\n  %s", got, strings.Join(declared(b, "Cargo.toml"), "\n  "))
	}
}

// The inline dependency-TABLE form for cargo (S7 requirement 6).
func TestCargoInlineDependencyTable(t *testing.T) {
	b := extractFixture(t, map[string]string{"Cargo.toml": "[workspace]\n" +
		`dependencies = { shared = "1.0", other = { version = "2.0" } }` + "\n"})
	mustDecl(t, b, "Cargo.toml", "dep:crates/shared|1.0|workspace-dependencies")
	mustDecl(t, b, "Cargo.toml", "dep:crates/other|2.0|workspace-dependencies")
}
