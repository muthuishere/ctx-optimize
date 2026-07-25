package manifests

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// declared collects rel --declares--> dep edges as "<dep id>|<version>|<scope>".
func declared(b *schema.Batch, rel string) []string {
	var out []string
	for _, e := range b.Edges {
		if e.Relation == "declares" && e.Source == rel {
			out = append(out, e.Target+"|"+e.Metadata["version_spec"]+"|"+e.Metadata["scope"])
		}
	}
	return out
}

func hasDecl(b *schema.Batch, rel, want string) bool {
	for _, d := range declared(b, rel) {
		if d == want {
			return true
		}
	}
	return false
}

func mustDecl(t *testing.T, b *schema.Batch, rel, want string) {
	t.Helper()
	if !hasDecl(b, rel, want) {
		t.Errorf("missing declaration %q from %s; got:\n  %s", want, rel,
			strings.Join(declared(b, rel), "\n  "))
	}
}

const fixturePEP621 = `[project]
name = "demo"
description = """
A prose block that mentions dependencies = ["phantom-from-prose"] on purpose.
"""
dependencies = [
    "blinker>=1.9.0",
    "celery[redis]==5.2.7",
    "flask",
    "gha-update ; python_full_version >= '3.12'",
    "typing_extensions",
]

[project.optional-dependencies]
async = ["asgiref>=3.2"]

[dependency-groups]
dev = ["ruff"]
tests = ["pytest"]

[build-system]
requires = ["flit_core>=3.11,<4"]
build-backend = "flit_core.buildapi"

[tool.uv]
dev-dependencies = ["tox"]

[tool.tox.env.tests-min]
commands = [
    [
        "uv", "pip", "install",
        "blinker==1.9.0",
        "phantom-pkg==2.0.0",
    ],
]
`

func TestPEP621AndPEP735Dependencies(t *testing.T) {
	b := extractFixture(t, map[string]string{"pyproject.toml": fixturePEP621})
	rel := "pyproject.toml"

	n := nodeByID(b, "dep:pypi/blinker")
	if n == nil {
		t.Fatal("missing dep:pypi/blinker")
	}
	if n.Kind != "dependency" || n.FileType != "manifest" || n.Label != "blinker" ||
		n.Metadata["ecosystem"] != "pypi" {
		t.Fatalf("pypi dep node shape wrong: %+v", n)
	}

	mustDecl(t, b, rel, "dep:pypi/blinker|>=1.9.0|dependencies")
	mustDecl(t, b, rel, "dep:pypi/celery|==5.2.7|dependencies")     // extras stripped
	mustDecl(t, b, rel, "dep:pypi/flask||dependencies")             // no specifier
	mustDecl(t, b, rel, "dep:pypi/gha-update||dependencies")        // env marker cut
	mustDecl(t, b, rel, "dep:pypi/typing-extensions||dependencies") // PEP 503
	mustDecl(t, b, rel, "dep:pypi/asgiref|>=3.2|optional-dependencies:async")
	mustDecl(t, b, rel, "dep:pypi/ruff||dependency-groups:dev")
	mustDecl(t, b, rel, "dep:pypi/pytest||dependency-groups:tests")
	mustDecl(t, b, rel, "dep:pypi/flit-core|>=3.11,<4|build-system")
	mustDecl(t, b, rel, "dep:pypi/tox||dev-dependencies")

	if got := len(declared(b, rel)); got != 10 {
		t.Errorf("declarations = %d, want exactly 10 (no extras):\n  %s", got,
			strings.Join(declared(b, rel), "\n  "))
	}
}

// The adversarial case from spike-p2: `[tool.tox.env.tests-min] commands` is
// an array of arrays of PEP-508-looking strings. Table-anchored matching must
// see NOTHING there, and a `description = """…"""` block must not leak either.
func TestNoPhantomDepsFromToxCommandsOrProse(t *testing.T) {
	b := extractFixture(t, map[string]string{"pyproject.toml": fixturePEP621})
	for _, n := range b.Nodes {
		switch n.ID {
		case "dep:pypi/phantom-pkg", "dep:pypi/uv", "dep:pypi/pip",
			"dep:pypi/install", "dep:pypi/phantom-from-prose":
			t.Errorf("phantom dependency emitted: %s", n.ID)
		}
	}
	// blinker is declared once, by [project] — not a second time at ==1.9.0.
	for _, d := range declared(b, "pyproject.toml") {
		if strings.HasPrefix(d, "dep:pypi/blinker|==1.9.0") {
			t.Errorf("tox command string became a declaration: %s", d)
		}
	}
}

const fixturePoetry = `[tool.poetry]
name = "demo"

[tool.poetry.dependencies]
python = "^3.11"
requests = "^2.31"
pendulum = { version = "^2.1", extras = ["test"] }
localpkg = { path = "../local" }
vcspkg = { git = "https://example.invalid/x.git" }

[tool.poetry.dependencies.tabulate]
version = "0.9.0"
optional = true

[tool.poetry.dev-dependencies]
black = "^24.1"

[tool.poetry.group.docs.dependencies]
sphinx = "^7"

[tool.poetry.group.tests.dependencies]
pytest = "^8"
`

func TestPoetryDependencies(t *testing.T) {
	b := extractFixture(t, map[string]string{"pyproject.toml": fixturePoetry})
	rel := "pyproject.toml"

	mustDecl(t, b, rel, "dep:pypi/requests|^2.31|dependencies")
	mustDecl(t, b, rel, "dep:pypi/pendulum|^2.1|dependencies")
	mustDecl(t, b, rel, "dep:pypi/localpkg|path:../local|dependencies")
	mustDecl(t, b, rel, "dep:pypi/vcspkg|git:https://example.invalid/x.git|dependencies")
	mustDecl(t, b, rel, "dep:pypi/tabulate|0.9.0|dependencies") // sub-table form
	mustDecl(t, b, rel, "dep:pypi/black|^24.1|dev-dependencies")
	mustDecl(t, b, rel, "dep:pypi/sphinx|^7|group:docs")
	mustDecl(t, b, rel, "dep:pypi/pytest|^8|group:tests")

	if nodeByID(b, "dep:pypi/python") != nil {
		t.Error("poetry's `python` interpreter constraint is not a package")
	}
	if got := len(declared(b, rel)); got != 8 {
		t.Errorf("declarations = %d, want 8:\n  %s", got, strings.Join(declared(b, rel), "\n  "))
	}
	// Scope classes: the qualified poetry group scopes must classify.
	for _, e := range b.Edges {
		if e.Relation != "declares" {
			continue
		}
		switch e.Metadata["scope"] {
		case "group:docs":
			if e.Metadata["scope_class"] != "dev" {
				t.Errorf("group:docs class = %q, want dev", e.Metadata["scope_class"])
			}
		case "group:tests":
			if e.Metadata["scope_class"] != "test" {
				t.Errorf("group:tests class = %q, want test", e.Metadata["scope_class"])
			}
		}
	}
}

// S7 requirement 6: the inline dependency-TABLE form.
func TestPoetryInlineDependencyTable(t *testing.T) {
	b := extractFixture(t, map[string]string{"pyproject.toml": "[tool.poetry]\n" +
		`dependencies = { alpha = "^1", beta = { version = "^2" } }` + "\n" +
		`dev-dependencies = { gamma = "^3" }` + "\n"})
	mustDecl(t, b, "pyproject.toml", "dep:pypi/alpha|^1|dependencies")
	mustDecl(t, b, "pyproject.toml", "dep:pypi/beta|^2|dependencies")
	mustDecl(t, b, "pyproject.toml", "dep:pypi/gamma|^3|dev-dependencies")
}

func TestPEP503Normalization(t *testing.T) {
	for in, want := range map[string]string{
		"flit_core":         "flit-core",
		"poetry_core":       "poetry-core",
		"typing_extensions": "typing-extensions",
		"Zope.Interface":    "zope-interface",
		"a__b..c--d":        "a-b-c-d",
		"already-fine":      "already-fine",
		"":                  "",
		"https://x/y":       "",
		"../local":          "",
		"-leading":          "",
		"pkg!":              "",
	} {
		if got := normalizePEP503(in); got != want {
			t.Errorf("normalizePEP503(%q) = %q, want %q", in, got, want)
		}
	}
	// The measured collapse: two spellings, ONE dependency node.
	b := extractFixture(t, map[string]string{
		"pyproject.toml":   "[project]\ndependencies = [\"flit_core\"]\n",
		"a/pyproject.toml": "[project]\ndependencies = [\"flit-core\"]\n",
	})
	count := 0
	for _, n := range b.Nodes {
		if strings.HasPrefix(n.ID, "dep:pypi/flit") {
			count++
			if n.ID != "dep:pypi/flit-core" {
				t.Errorf("unnormalized node id %s", n.ID)
			}
		}
	}
	if count != 1 {
		t.Errorf("flit_core/flit-core produced %d nodes, want 1", count)
	}
}

func TestPEP508NameExtraction(t *testing.T) {
	cases := []struct{ req, name, spec string }{
		{"blinker>=1.9.0", "blinker", ">=1.9.0"},
		{"pkg[all,fast]>=2,<3", "pkg", ">=2,<3"},
		{"pkg ; python_version >= '3.11'", "pkg", ""},
		{"pkg @ https://example.invalid/p.whl", "pkg", "@ https://example.invalid/p.whl"},
		{"sphinx<9", "sphinx", "<9"},
		{"pkg (>=1)", "pkg", "(>=1)"},
		// Refused rather than guessed (spike-p2's known conservative gap).
		{"https://example.invalid/direct.whl", "", ""},
		{"git+https://example.invalid/x#egg=xpkg", "", ""},
		{"./local/path", "", ""},
	}
	for _, c := range cases {
		name, spec := pep508(c.req)
		if name != c.name || spec != c.spec {
			t.Errorf("pep508(%q) = (%q, %q), want (%q, %q)", c.req, name, spec, c.name, c.spec)
		}
	}
}
