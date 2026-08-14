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
