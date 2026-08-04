package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// ADR 2026-07-26-declared-resolutions, first cut: external_methods only.
//
// The whole point of shipping this key ALONE is that it cannot make the graph
// wrong — it retires a maybe, never resolves one. So the tests here mostly pin
// what it must NOT do.

func writeResolutions(t *testing.T, dir, body string) {
	t.Helper()
	d := filepath.Join(dir, ".ctxoptimize")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "resolutions.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The case that motivated the whole thing: 95 `err.Error()` shortlists on this
// repo, none of which target our type.
func TestExternalMethodRetiresTheShortlist(t *testing.T) {
	files := map[string]string{
		"a/err.go": "package a\n\ntype MyErr struct{}\n\nfunc (e *MyErr) Error() string { return \"x\" }\n",
		"b/use.go": "package b\n\nimport \"errors\"\n\nfunc Caller() string {\n\terr := errors.New(\"boom\")\n\treturn err.Error()\n}\n",
	}
	dir := writeRepo(t, files)
	const target = "a/err.go::MyErr.Error"

	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if edgeTo(t, b, schema.Ambiguous, target) == nil {
		t.Fatal("precondition: without a declaration this is an AMBIGUOUS shortlist")
	}

	writeResolutions(t, dir, `{"external_methods": ["Error"]}`)
	b, err = Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e := edgeTo(t, b, schema.Ambiguous, target); e != nil {
		t.Error("the declaration did not retire the shortlist")
	}
	if e := edgeTo(t, b, "INFERRED", target); e != nil {
		t.Error("a declaration must never CREATE an edge — external_methods only ever removes maybes")
	}
}

// The safety property, stated as a test: a declaration is checked only on the
// abstention path, so an exact tie still wins. `MyErr.Error()` names our type
// outright; a declaration about the bare name must not delete that fact.
func TestDeclarationNeverRemovesAResolvedEdge(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"a/err.go": "package a\n\ntype MyErr struct{}\n\nfunc (e *MyErr) Error() string { return \"x\" }\n",
		"b/use.go": "package b\n\nimport \"x/a\"\n\nfunc Caller() string {\n\treturn a.MyErr.Error()\n}\n",
	})
	writeResolutions(t, dir, `{"external_methods": ["Error"]}`)
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if edgeTo(t, b, "INFERRED", "a/err.go::MyErr.Error") == nil {
		t.Fatal("MyErr.Error() names its own receiver — an external_methods line must not delete a resolved edge")
	}
}

// An unqualified `Error()` is a plain function call and may well be ours. The
// declaration is about METHOD names, so it must not reach that call site.
func TestDeclarationDoesNotTouchUnqualifiedCalls(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"a/a.go":   "package a\n\nfunc Error() string { return \"x\" }\n",
		"b/use.go": "package b\n\nfunc Caller() string {\n\treturn Error()\n}\n",
	})
	writeResolutions(t, dir, `{"external_methods": ["Error"]}`)
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if edgeTo(t, b, "INFERRED", "a/a.go::Error") == nil {
		t.Fatal("an unqualified call to a free function is not a method call — the declaration must not suppress it")
	}
}

// Silently ignoring a broken declaration is the worst outcome: the author will
// believe it is in force. Every one of these must be a hard error.
func TestMalformedDeclarationsFailLoudly(t *testing.T) {
	for name, body := range map[string]string{
		"bad json":       `{"external_methods": [`,
		"unknown key":    `{"externalmethods": ["Error"]}`,
		"future key":     `{"external_methods": ["Error"], "receiver_types": {"s": "Store"}}`,
		"qualified name": `{"external_methods": ["MyErr.Error"]}`,
		"with parens":    `{"external_methods": ["Error()"]}`,
		"empty name":     `{"external_methods": [" "]}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeRepo(t, map[string]string{"a/a.go": "package a\n\nfunc F() {}\n"})
			writeResolutions(t, dir, body)
			if _, err := Extract(dir); err == nil {
				t.Error("a malformed declaration must fail the gather, not be ignored")
			}
		})
	}
}

func TestNoDeclarationFileChangesNothing(t *testing.T) {
	if r, err := LoadResolutions(t.TempDir()); err != nil || len(r.ExternalMethods) != 0 {
		t.Errorf("absent file must be the zero value, got %+v / %v", r, err)
	}
}

// A stale line (renamed type, deleted call site) must be reported. A
// declaration file nobody prunes decays into confident-looking lies.
func TestStaleDeclarationIsReported(t *testing.T) {
	set := newExternalSet(&Resolutions{ExternalMethods: []string{"Error", "Gone"}})
	if !set.suppress(callSite{callee: "Error", recv: "err"}) {
		t.Fatal("Error should have suppressed")
	}
	stale := set.unused()
	if len(stale) != 1 || stale[0] != "Gone" {
		t.Errorf("unused() = %v, want [Gone]", stale)
	}
}

func TestUnknownKeyErrorNamesWhatIsSupported(t *testing.T) {
	dir := t.TempDir()
	writeResolutions(t, dir, `{"receiver_types": {"s": "Store"}}`)
	_, err := LoadResolutions(dir)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "external_methods") {
		t.Errorf("the error must say which keys ARE supported, got: %v", err)
	}
}
