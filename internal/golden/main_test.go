package golden

import (
	"os"
	"testing"
)

// TestMain isolates the MACHINE-GLOBAL grammar-pack directory for this whole
// package.
//
// Golden tests were hermetic in every way except one: they pass --store to a
// temp dir, but grammar packs are discovered from ~/ctxoptimize/grammars (or
// $CTX_OPTIMIZE_GRAMMARS) — a real directory on the developer's machine that no
// test isolated. So whatever packs a developer happens to have installed took
// part in every "hermetic" fixture gather, and a MALFORMED one failed the suite
// with an error that has nothing to do with the change under test.
//
// That is not hypothetical: a stray half-built pack in the real dir turned
// TestGoldenMultiModuleConfigRepo, TestGoldenDotnetSlnRepo and
// TestGoldenPythonRustDepsRepo red simultaneously with
// "grammar pack …/clojure.json: name, exts and decls are required".
//
// Pointing it at an empty temp dir makes pack discovery deterministic: the
// golden contract is now exactly what the EMBEDDED grammars plus a fixture's
// own repo-local .ctxoptimize/grammars produce. A fixture that wants a pack
// still ships one in its own tree.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ctx-golden-grammars-")
	if err != nil {
		os.Exit(1)
	}
	os.Setenv("CTX_OPTIMIZE_GRAMMARS", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
