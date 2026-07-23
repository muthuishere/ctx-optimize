package deplink

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func modNode(spec string) schema.Node {
	return schema.Node{ID: modulePrefix + spec, Label: spec, Kind: "module", FileType: "code", Source: modulePrefix + spec}
}

func depNode(id string) schema.Node {
	return schema.Node{ID: id, Label: id, Kind: "dependency", FileType: "manifest", Source: "dep://x"}
}

func linkTargets(t *testing.T, b *schema.Batch) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, e := range b.Edges {
		if e.Relation != "resolves_to" {
			t.Fatalf("unexpected relation %q", e.Relation)
		}
		if e.Confidence != schema.Inferred {
			t.Fatalf("edge %s->%s: confidence %q, want INFERRED", e.Source, e.Target, e.Confidence)
		}
		if e.Metadata["synthesized_by"] != ProducerName {
			t.Fatalf("edge %s->%s: missing synthesized_by", e.Source, e.Target)
		}
		out[e.Source] = e.Target
	}
	return out
}

func TestNpmSubpathScopedAndSkips(t *testing.T) {
	code := &schema.Batch{Producer: "code", Nodes: []schema.Node{
		modNode("react"),
		modNode("react-dom/client"),
		modNode("@testing-library/react"),
		modNode("@scope/pkg/deep/sub"),
		modNode("node:fs"),
		modNode("fs"),
		modNode("./local"),
		modNode("left-pad"), // external but undeclared
	}}
	man := &schema.Batch{Producer: "manifests", Nodes: []schema.Node{
		depNode("dep:npm/react"),
		depNode("dep:npm/react-dom"),
		depNode("dep:npm/@testing-library/react"),
		depNode("dep:npm/@scope/pkg"),
	}}
	got := linkTargets(t, Link(code, man, nil))
	want := map[string]string{
		modulePrefix + "react":                  "dep:npm/react",
		modulePrefix + "react-dom/client":       "dep:npm/react-dom",
		modulePrefix + "@testing-library/react": "dep:npm/@testing-library/react",
		modulePrefix + "@scope/pkg/deep/sub":    "dep:npm/@scope/pkg",
	}
	if len(got) != len(want) {
		t.Fatalf("links = %v, want %v", got, want)
	}
	for src, tgt := range want {
		if got[src] != tgt {
			t.Errorf("%s -> %q, want %q", src, got[src], tgt)
		}
	}
}

func TestGoLongestPrefixAndSelfSkip(t *testing.T) {
	code := &schema.Batch{Producer: "code", Nodes: []schema.Node{
		modNode("github.com/nats-io/nats.go/jetstream"),
		modNode("github.com/nats-io/nats.go"),
		modNode("github.com/me/myrepo/internal/app"), // self
		modNode("fmt"),           // stdlib
		modNode("encoding/json"), // stdlib
	}}
	man := &schema.Batch{Producer: "manifests", Nodes: []schema.Node{
		depNode("dep:go/github.com/nats-io/nats.go"),
	}}
	got := linkTargets(t, Link(code, man, []string{"github.com/me/myrepo"}))
	if len(got) != 2 {
		t.Fatalf("links = %v, want exactly the two nats specs", got)
	}
	for _, spec := range []string{"github.com/nats-io/nats.go/jetstream", "github.com/nats-io/nats.go"} {
		if got[modulePrefix+spec] != "dep:go/github.com/nats-io/nats.go" {
			t.Errorf("%s -> %q", spec, got[modulePrefix+spec])
		}
	}
}

func TestMavenUnambiguousOnly(t *testing.T) {
	code := &schema.Batch{Producer: "code", Nodes: []schema.Node{
		modNode("org.apache.kafka.clients.producer.KafkaProducer"),
		modNode("com.shared.util.Thing"), // two deps share groupId com.shared
	}}
	man := &schema.Batch{Producer: "manifests", Nodes: []schema.Node{
		depNode("dep:maven/org.apache.kafka:kafka-clients"),
		depNode("dep:maven/com.shared:lib-a"),
		depNode("dep:maven/com.shared:lib-b"),
	}}
	got := linkTargets(t, Link(code, man, nil))
	if len(got) != 1 {
		t.Fatalf("links = %v, want only the kafka link (ambiguous dropped)", got)
	}
	if got[modulePrefix+"org.apache.kafka.clients.producer.KafkaProducer"] != "dep:maven/org.apache.kafka:kafka-clients" {
		t.Errorf("kafka link missing: %v", got)
	}
}

func TestEmptyAndNilBatches(t *testing.T) {
	b := Link(nil, nil, nil)
	if b.Producer != ProducerName || len(b.Edges) != 0 {
		t.Fatalf("nil batches: %+v", b)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("empty batch must validate: %v", err)
	}
}

func TestGoModulePaths(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "go.mod"), []byte("module github.com/me/root\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(base, "svc")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "go.mod"), []byte("// header\nmodule github.com/me/svc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := GoModulePaths(base, []string{"svc", "missing"})
	if len(got) != 2 || got[0] != "github.com/me/root" || got[1] != "github.com/me/svc" {
		t.Fatalf("GoModulePaths = %v", got)
	}
}
