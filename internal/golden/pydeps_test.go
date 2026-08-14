package golden

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/store"
)

// TestGoldenPythonRustDepsRepo pins the S7 dependency contract (ADR
// 2026-07-25-structured-formats) on a committed fixture carrying every shape
// the change claims: a PEP 621 pyproject with optional-dependencies, PEP 735
// dependency-groups, a build-system requires, a poetry group, an adversarial
// `[tool.tox…] commands` array of requirement-looking strings and a multi-line
// prose block; a hand-written requirements.txt; a pip-compile LOCK that must
// contribute nothing; and a Cargo.toml with an inline table, a dependency
// sub-table and a `[target."cfg(…)"]` section.
func TestGoldenPythonRustDepsRepo(t *testing.T) {
	repo := t.TempDir()
	copyTree(t, filepath.Join("testdata", "repos", "pydeps"), repo)
	storeRoot := t.TempDir()

	gatherWithin(t, 3*time.Second, repo, storeRoot)

	snap := snapshot(t, storeRoot)

	// 1. pypi namespace + PEP 503 normalization (typing_extensions collapses,
	//    and flit_core in pyproject shares ONE node with flit-core in
	//    requirements.txt).
	for _, id := range []string{
		"dep:pypi/blinker", "dep:pypi/celery", "dep:pypi/flask",
		"dep:pypi/typing-extensions", "dep:pypi/asgiref", "dep:pypi/ruff",
		"dep:pypi/pytest", "dep:pypi/flit-core", "dep:pypi/sphinx",
		"dep:pypi/gunicorn",
	} {
		mustContain(t, snap, "pypi dependency node", "N "+id+" | dependency")
	}
	if strings.Contains(snap, "dep:pypi/typing_extensions") {
		t.Error("PEP 503 normalization regressed: unnormalized node id present")
	}
	if n := strings.Count(snap, "N dep:pypi/flit-core | dependency"); n != 1 {
		t.Errorf("flit_core/flit-core must share ONE node, got %d", n)
	}

	// 2. crates namespace, incl. inline table, sub-table and target section.
	for _, id := range []string{
		"dep:crates/anyhow", "dep:crates/serde", "dep:crates/tokio",
		"dep:crates/criterion", "dep:crates/winapi",
	} {
		mustContain(t, snap, "crates dependency node", "N "+id+" | dependency")
	}

	// 3. declares edges are anchored on the manifest that holds them.
	mustContain(t, snap, "pyproject declares", "E pyproject.toml -declares-> dep:pypi/blinker")
	mustContain(t, snap, "requirements declares", "E requirements.txt -declares-> dep:pypi/gunicorn")
	mustContain(t, snap, "cargo declares", "E rustcrate/Cargo.toml -declares-> dep:crates/winapi")

	// 4. THE HONESTY LANDMARK: the pip-compile lock declares nothing at all.
	if strings.Contains(snap, "requirements-lock.txt -declares->") {
		t.Error("pip-compile lock produced declares edges — S7 requirement 4 regressed")
	}
	for _, phantom := range []string{
		"dep:pypi/transitive-only", // only the lock mentions it
		"dep:pypi/amqp",            // only the lock mentions it
		"dep:pypi/phantom-pkg",     // only the tox command array mentions it
		"dep:pypi/uv", "dep:pypi/pip", "dep:pypi/install",
		"dep:pypi/phantom-from-prose", // only the multi-line prose mentions it
		"dep:pypi/python",             // poetry's interpreter constraint
	} {
		if strings.Contains(snap, phantom+" |") {
			t.Errorf("phantom dependency in the graph: %s", phantom)
		}
	}

	// 5. Scopes (the union mirrored onto the node) are filterable.
	scopes := depScopes(t, storeRoot)
	for id, want := range map[string]string{
		"dep:pypi/blinker":     "runtime",
		"dep:pypi/asgiref":     "optional",
		"dep:pypi/ruff":        "dev",
		"dep:pypi/pytest":      "test", // dependency-groups:tests, the fixed rule
		"dep:pypi/sphinx":      "dev",  // poetry group:docs
		"dep:pypi/flit-core":   "build,runtime",
		"dep:crates/criterion": "dev",
		"dep:crates/winapi":    "runtime", // target-dependencies keeps the base class
	} {
		if scopes[id] != want {
			t.Errorf("scope of %s = %q, want %q", id, scopes[id], want)
		}
	}

	// The exact snapshot is the contract.
	checkGolden(t, "pydeps", snap)

	// Retrieval: a dependency is findable by name.
	top := queryTop(t, storeRoot, repo, "blinker", 3)
	checkGolden(t, "pydeps-queries", "blinker -> "+strings.Join(top, ", ")+"\n")
}

// depScopes reads the aggregated Scope field off every dependency node — the
// snapshot renders id/kind/source/location only, so the scope union needs its
// own probe.
func depScopes(t *testing.T, storeRoot string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, key := range storeKeys(t, storeRoot) {
		st, err := store.Open(storeRoot, key)
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := st.Nodes()
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range nodes {
			if n.Kind == "dependency" {
				out[n.ID] = n.Scope
			}
		}
	}
	return out
}
