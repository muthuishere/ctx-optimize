package boundaries

import (
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func edgeBetween(b *schema.Batch, src, tgt string) *schema.Edge {
	for i := range b.Edges {
		if b.Edges[i].Source == src && b.Edges[i].Target == tgt {
			return &b.Edges[i]
		}
	}
	return nil
}

func TestServiceDepTierInferredAndConfigHint(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, "package.json", `{"name":"x","dependencies":{"firebase":"^10.0.0"}}`)
	// firebase has only wildcard hosts → identifier falls back to the id.
	write(t, root, "app.js", "const k = process.env.VITE_FIREBASE_API_KEY\n")
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	p := find(b, "port:network.http:>firebase")
	if p == nil {
		t.Fatalf("dep-declared service port missing; nodes: %d", len(b.Nodes))
	}
	if p.Metadata["svc.id"] != "firebase" || p.Metadata["scope"] != "external" {
		t.Fatalf("service port metadata wrong: %+v", p.Metadata)
	}
	e := edgeBetween(b, "package.json", "port:network.http:>firebase")
	if e == nil || e.Confidence != schema.Inferred || e.Metadata["rule"] != "service:firebase" {
		t.Fatalf("manifest→service edge wrong: %+v", e)
	}
	// config_hint FIREBASE_ attaches the env port (substring — VITE_FIREBASE_* counts).
	ref := edgeBetween(b, "port:config.env:>VITE_FIREBASE_API_KEY", "port:network.http:>firebase")
	if ref == nil || ref.Relation != "references" {
		t.Fatalf("config_hint edge missing: %+v", ref)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("service batch rejected by the schema door: %v", err)
	}
}

func TestSDKSymbolIsExtractedWithEndpoint(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, "ai.py", "resp = client.chat.completions.create(model=\"gpt\")\n")
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	p := find(b, "port:network.http:>api.openai.com")
	if p == nil || p.Metadata["otel.server.address"] != "api.openai.com" {
		t.Fatalf("sdk-site service port wrong: %+v", p)
	}
	e := edgeBetween(b, "ai.py", "port:network.http:>api.openai.com")
	if e == nil || e.Confidence != schema.Extracted {
		t.Fatalf("sdk call site must be EXTRACTED: %+v", e)
	}
	if e.Metadata["otel.http.request.method"] != "POST" || e.Metadata["otel.url.path"] != "/v1/chat/completions" {
		t.Fatalf("endpoint resolution missing: %+v", e.Metadata)
	}
}

func TestSDKSitesInVendoredTreesExcluded(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, "benchmarks/corpus/ai.py", "client.chat.completions.create(x)\n")
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	if find(b, "port:network.http:>api.openai.com") != nil {
		t.Fatal("vendored corpus leaked into the services tier")
	}
}

func TestRepoServicesFileExtendsRegistry(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, ".ctxoptimize/services.json", `{"version":1,"services":{
	  "acme-platform":{"match":{"deps":["npm:@acme/client"],"hosts":["api.acme.internal"]},
	   "config_hint":"ACME_"}}}`)
	write(t, root, "package.json", `{"name":"x","dependencies":{"@acme/client":"^1.0.0"}}`)
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	p := find(b, "port:network.http:>api.acme.internal")
	if p == nil || p.Metadata["svc.id"] != "acme-platform" {
		t.Fatalf("repo-registered internal platform missing: %+v", p)
	}
}

func TestMalformedServicesFileFailsLoud(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, ".ctxoptimize/services.json", `{"version":1,"services":{
	  "bad":{"transport":"HTTP","match":{"deps":["npm:x"]}}}}`)
	if _, err := Extract(root); err == nil {
		t.Fatal("uppercase transport accepted silently")
	}
	write(t, root, ".ctxoptimize/services.json", `{"version":1,"services":{
	  "bad":{"match":{}}}}`)
	if _, err := Extract(root); err == nil {
		t.Fatal("service with no deps and no hosts accepted silently")
	}
}

func TestDepGlobMatching(t *testing.T) {
	cases := []struct {
		pat, key string
		want     bool
	}{
		{"npm:firebase", "npm/firebase", true},
		{"npm:firebase", "npm/firebase-admin", false},
		{"npm:@aws-sdk/*", "npm/@aws-sdk/client-s3", true},
		{"go:github.com/stripe/stripe-go*", "go/github.com/stripe/stripe-go/v76", true},
		{"pypi:google-cloud-*", "pypi/google-cloud-storage", true},
		{"pypi:google-cloud-*", "pypi/google-genai", false},
	}
	for _, c := range cases {
		if got := depMatches(c.pat, c.key); got != c.want {
			t.Fatalf("depMatches(%q,%q) = %v, want %v", c.pat, c.key, got, c.want)
		}
	}
}

func TestValidateServiceFileRejectsUnknownFields(t *testing.T) {
	if _, err := ValidateServiceFile([]byte(`{"version":1,"services":{"x":{"match":{"hosts":["a.b"]}}},"extra":1}`)); err == nil {
		t.Fatal("unknown top-level field accepted")
	}
	if _, err := ValidateServiceFile([]byte(`{"version":1,"services":{"x":{"match":{"deps":["noprefix"]}}}}`)); err == nil {
		t.Fatal("un-prefixed dep accepted")
	}
	if f, err := ValidateServiceFile([]byte(`{"version":1,"services":{"x":{"match":{"hosts":["api.x.io"]}}}}`)); err != nil || len(f.Services) != 1 {
		t.Fatalf("valid file rejected: %v", err)
	}
}
