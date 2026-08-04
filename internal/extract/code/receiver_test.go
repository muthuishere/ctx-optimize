package code

import (
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// ADR 2026-07-25-method-call-resolution. Call resolution keys on the callee's
// BARE name, so a method name that happens to be unique in the repo used to
// absorb every call site written with that name — `err.Error()` on a stdlib
// error resolved, INFERRED, to whichever type of ours declared Error. That is
// worse than ambiguity: the shortlist never fired, because from the matcher's
// point of view there was nothing to be ambiguous about.
//
// The graph holds only OUR declarations. It cannot see `error`, `io.Closer` or
// any dependency type, so a unique name is not evidence about the receiver.
// These tests pin the gate: attribute when the receiver is actually tied,
// abstain out loud otherwise.

func edgeTo(t *testing.T, b *schema.Batch, conf, target string) *schema.Edge {
	t.Helper()
	for i, e := range b.Edges {
		if e.Relation == "calls" && e.Confidence == conf && e.Target == target {
			return &b.Edges[i]
		}
	}
	return nil
}

// The defect itself: a method call on a receiver we never typed must NOT be
// attributed to the one declaration that shares the name.
func TestUntypedReceiverIsNotAttributed(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"a/err.go": "package a\n\ntype MyErr struct{}\n\nfunc (e *MyErr) Error() string { return \"x\" }\n",
		"b/use.go": "package b\n\nimport \"errors\"\n\nfunc Caller() string {\n\terr := errors.New(\"boom\")\n\treturn err.Error()\n}\n",
	})
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	const target = "a/err.go::MyErr.Error"
	if e := edgeTo(t, b, "INFERRED", target); e != nil {
		t.Fatalf("err.Error() on a stdlib error was attributed to %s — a unique name is not evidence about the receiver", target)
	}
	e := edgeTo(t, b, schema.Ambiguous, target)
	if e == nil {
		t.Fatal("the call site vanished; abstaining must be LOUD — an AMBIGUOUS shortlist, not silence")
	}
	if got := e.Metadata["ambiguous_reason"]; got != schema.AmbiguousUnresolvedReceiver {
		t.Errorf("ambiguous_reason = %q, want %q — the reason decides which grep settles it", got, schema.AmbiguousUnresolvedReceiver)
	}
}

// The tie that keeps test→source edges alive: the receiver's type is NAMED in
// the same declaration as the call. This is the case the golden net caught
// when the gate first landed without it.
func TestReceiverTypeNamedInScopeResolves(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"src/engine.go":     "package src\n\ntype Engine struct{}\n\nfunc (e *Engine) Charge() {}\n",
		"tests/eng_test.go": "package tests\n\nimport \"x/src\"\n\nfunc TestCharge() {\n\te := &src.Engine{}\n\te.Charge()\n}\n",
	})
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if edgeTo(t, b, "INFERRED", "src/engine.go::Engine.Charge") == nil {
		t.Fatal("Engine is constructed in the calling scope — that is evidence, and dropping it costs the 'which tests cover X' answer")
	}
}

// A call written on the type itself needs no inference at all.
func TestReceiverEqualToOwnerResolves(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"a/c.py": "class Engine:\n    @classmethod\n    def build(cls):\n        pass\n",
		"b/u.py": "def caller():\n    Engine.build()\n",
	})
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if edgeTo(t, b, "INFERRED", "a/c.py::Engine.build") == nil {
		t.Fatal("Engine.build() names its own receiver — nothing to abstain about")
	}
}

// self./this. calls from inside the type resolve; the enclosing declaration IS
// the receiver.
func TestSelfCallResolves(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"a/c.py": "class Engine:\n    def helper(self):\n        pass\n\n    def run(self):\n        self.helper()\n",
	})
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if edgeTo(t, b, "INFERRED", "a/c.py::Engine.helper") == nil {
		t.Fatal("self.helper() inside Engine resolves to Engine.helper")
	}
}

// Free functions are not gated: the gate is about receivers, and a plain call
// has none. Guards against the gate quietly eating ordinary call edges.
func TestFreeFunctionCallsAreUnaffected(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"a/a.go":   "package a\n\nfunc Helper() {}\n",
		"b/use.go": "package b\n\nfunc Caller() {\n\tHelper()\n}\n",
	})
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if edgeTo(t, b, "INFERRED", "a/a.go::Helper") == nil {
		t.Fatal("a free function call must still resolve module-wide by unique name")
	}
}

// typeShaped is a CONVENTION, and conventions are only admissible here because
// this one can lose evidence but never manufacture it: a missed type means an
// abstention, never a wrong edge.
func TestTypeShapedAdmitsOnlyCamelCase(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"Engine", true}, {"HttpClient", true}, {"E", false},
		{"engine", false}, {"MAX_SIZE", false}, {"ENGINE", false}, {"", false},
	} {
		if got := typeShaped(tc.in); got != tc.want {
			t.Errorf("typeShaped(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestOwnerOfTakesTheImmediateQualifier(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Merge", ""},
		{"Store.Merge", "Store"},
		{"Outer.Inner.run", "Inner"},
	} {
		if got := ownerOf(tc.in); got != tc.want {
			t.Errorf("ownerOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// With the gate off, the old behaviour returns — the trade stays a measurement
// rather than a belief.
func TestGateOffRestoresBareNameMatch(t *testing.T) {
	orig := receiverGate
	defer func() { receiverGate = orig }()
	receiverGate = false

	dir := writeRepo(t, map[string]string{
		"a/err.go": "package a\n\ntype MyErr struct{}\n\nfunc (e *MyErr) Error() string { return \"x\" }\n",
		"b/use.go": "package b\n\nimport \"errors\"\n\nfunc Caller() string {\n\terr := errors.New(\"boom\")\n\treturn err.Error()\n}\n",
	})
	b, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if edgeTo(t, b, "INFERRED", "a/err.go::MyErr.Error") == nil {
		t.Fatal("gate off must reproduce the pre-ADR attribution")
	}
}
