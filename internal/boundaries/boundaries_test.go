package boundaries

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Hermetic: point the machine dirs somewhere empty so a developer's real
// ~/ctxoptimize/boundaries and ~/ctxoptimize/services never leak into a test.
func hermetic(t *testing.T) {
	t.Helper()
	t.Setenv("CTX_OPTIMIZE_BOUNDARIES", t.TempDir())
	t.Setenv("CTX_OPTIMIZE_SERVICES", t.TempDir())
}

func find(b *schema.Batch, id string) *schema.Node {
	for i := range b.Nodes {
		if b.Nodes[i].ID == id {
			return &b.Nodes[i]
		}
	}
	return nil
}

func TestRepoRuleOverridesEmbeddedById(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	// Narrow the embedded env-go rule to nothing: same ID, impossible ext.
	write(t, root, ".ctxoptimize/boundaries.json", `{"version":1,"boundaries":[
	  {"id":"env-go","transport":"config.env","direction":"consumes","scan":"raw",
	   "when":{"ext":[".nope"]},"scan":"raw",
	   "match":[{"re":"never([A-Z]+)","identifier":1}]}]}`)
	write(t, root, "main.go", `package main
func f() { _ = os.Getenv("SHOULD_NOT_APPEAR") }`)
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if find(b, "port:config.env:>SHOULD_NOT_APPEAR") != nil {
		t.Fatal("embedded rule ran despite repo override with the same id")
	}
}

func TestMalformedConfigFailsLoud(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, ".ctxoptimize/boundaries.json", `{"version":1,"boundaries":[
	  {"id":"bad","transport":"network.http","direction":"sideways","scan":"raw","scan":"raw",
	   "match":[{"re":"x","identifier":0}]}]}`)
	if _, err := Extract(root); err == nil {
		t.Fatal("invalid direction accepted silently")
	}
}

// A rule's `metadata` is applied to the port node AFTER the engine fills the
// reserved fields, so an engine-owned key there REPLACED a computed fact rather
// than adding one. Measured before the fix: a rule declaring
// `"direction": "consumes"` with `"metadata": {"direction": "provides"}` loaded
// clean and its port was listed under PROVIDES — the headline split of the
// `boundaries` verb, and the scope join's input, reporting the opposite of the
// truth. `flag.set` writes into the same map and had the identical hole.
// Rejected at LOAD now, beside the other malformed shapes (ADR
// 2026-08-15-authoring-loop-unenforced, D1).
func TestReservedMetadataKeyRejectedAtLoad(t *testing.T) {
	for _, tc := range []struct{ name, extra, want string }{
		{"metadata inverts direction", `"metadata":{"direction":"provides"}`, `metadata key "direction"`},
		{"metadata steals identifier", `"metadata":{"identifier":"nope"}`, `metadata key "identifier"`},
		{"metadata forges producer", `"metadata":{"producer":"code"}`, `metadata key "producer"`},
		{"flag.set inverts direction", `"flag":{"when_identifier_matches":".","set":{"direction":"provides"}}`, `flag.set key "direction"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hermetic(t)
			root := t.TempDir()
			write(t, root, ".ctxoptimize/boundaries.json", `{"version":1,"boundaries":[
			  {"id":"reserved-key","transport":"network.http","direction":"consumes","scan":"raw",
			   "when":{"ext":[".txt"]},"match":[{"re":"CALLOUT ([a-z.]+)","identifier":1}],`+tc.extra+`}]}`)
			write(t, root, "thing.txt", "CALLOUT api.example.com\n")
			_, err := Extract(root)
			if err == nil {
				t.Fatal("a rule overwriting an engine-owned port key was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error must name the offending key, got: %v", err)
			}
			if !strings.Contains(err.Error(), `"reserved-key"`) {
				t.Fatalf("error must name the rule id, got: %v", err)
			}
		})
	}
}

// The fix must close the HOLE, not the door: namespaced metadata is the open
// vocabulary the schema door already blesses (`otel.*`/`pack.*`/`org.*`), and
// `sensitive` is the one reserved key authors own — the three shipped env rules
// set it through `flag.set`, and the engine never computes it. Both must still
// reach the node, and the declared direction must survive.
func TestNamespacedAndAuthorOwnedMetadataStillWork(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, ".ctxoptimize/boundaries.json", `{"version":1,"boundaries":[
	  {"id":"open-meta","transport":"network.http","direction":"consumes","scan":"raw",
	   "when":{"ext":[".txt"]},"match":[{"re":"CALLOUT ([a-z.]+)","identifier":1}],
	   "metadata":{"otel.server.address":"$identifier"},
	   "flag":{"when_identifier_matches":"example","set":{"sensitive":"true"}}}]}`)
	write(t, root, "thing.txt", "CALLOUT api.example.com\n")
	b, err := Extract(root)
	if err != nil {
		t.Fatalf("a legitimate namespaced/author-owned rule was rejected: %v", err)
	}
	n := find(b, "port:network.http:>api.example.com")
	if n == nil {
		t.Fatalf("port not emitted; nodes: %+v", b.Nodes)
	}
	if got := n.Metadata["otel.server.address"]; got != "api.example.com" {
		t.Fatalf("namespaced metadata lost: %q", got)
	}
	if n.Metadata["sensitive"] != "true" {
		t.Fatalf("flag.set sensitive lost: %+v", n.Metadata)
	}
	if n.Metadata["direction"] != "consumes" {
		t.Fatalf("engine direction not preserved: %q", n.Metadata["direction"])
	}
}

// The routes-* rules are the PROVIDES side of D3 — additive port coverage.
// The AST recognizers in internal/extract/code remain the EXTRACTED route
// truth (kind=route + handles edges); these rules ship INFERRED and never
// touch that surface (pinned by the byte-match evidence in the ADR).
