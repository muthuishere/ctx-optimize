package app

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/query"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// The leak this closes, measured on the shipped binary before the fix: the
// store never held a secret VALUE, but hydration re-reads the cited line off
// disk, so a config_key node anchored at `spring.datasource.password=…` printed
// the credential straight into agent context — via plain `card`, no
// --include-content required. Confirmed on .properties, .ini, .toml and compose.
func TestRedactSensitiveLines(t *testing.T) {
	cases := []struct {
		name, in, wantAbsent, wantPresent string
	}{
		{"properties password", "spring.datasource.password=PROPS_SECRET", "PROPS_SECRET", "spring.datasource.password"},
		{"ini client_secret", "client_secret=INI_SECRET", "INI_SECRET", "client_secret"},
		{"toml quoted", `password = "TOML_SECRET"`, "TOML_SECRET", "password"},
		{"yaml indented", "      POSTGRES_PASSWORD: hunter2", "hunter2", "POSTGRES_PASSWORD"},
		{"api token", "api.token=tok_live_ABC", "tok_live_ABC", "api.token"},
		{"api_key underscore", "api_key: AKIAIOSFODNN7", "AKIAIOSFODNN7", "api_key"},
		{"access-key dash", "access-key: abc123", "abc123", "access-key"},
		{"private_key", "private_key: MIIEvQ", "MIIEvQ", "private_key"},
		{"connection string", "ConnectionString: Server=db;Password=pw", "Password=pw", "ConnectionString"},
		// Innocent key, credential embedded in the VALUE — the case that got
		// past a key-only check and needed URL masking.
		{"url userinfo", "DB_URL: postgres://user:topsecret@db:5432/app", "topsecret", "postgres://user:***@db"},
		{"url in properties", "url=mysql://root:hunter2@localhost/app", "hunter2", "root:***@localhost"},
	}
	for _, c := range cases {
		got := redactSensitiveLines(c.in)
		if strings.Contains(got, c.wantAbsent) {
			t.Errorf("%s: secret survived redaction\n in: %q\nout: %q", c.name, c.in, got)
		}
		if !strings.Contains(got, c.wantPresent) {
			t.Errorf("%s: lost the citable key\n in: %q\nout: %q\nwant to still contain %q",
				c.name, c.in, got, c.wantPresent)
		}
	}
}

// Redaction must not eat ordinary config — the citation stays useful.
func TestRedactLeavesNonSecretsAlone(t *testing.T) {
	for _, in := range []string{
		"spring.datasource.url=jdbc:postgresql://db:5432/app", // no userinfo
		"server:\n  port: 8080",
		"image: postgres:16",
		"func Charge(id int) error {",
		"passwordless: true",   // "password" not on a word boundary
		"# see the token docs", // a comment, not an assignment
		"http://example.com/a:b",
	} {
		if got := redactSensitiveLines(in); got != in {
			t.Errorf("non-secret line was altered\n in: %q\nout: %q", in, got)
		}
	}
}

// Over-redaction is the DELIBERATE failure mode: a leaked value cannot be
// pulled back out of a model's context, an over-redacted one costs a file read.
// Pinned so the tradeoff reads as chosen, not accidental.
func TestRedactPrefersOverRedaction(t *testing.T) {
	for _, in := range []string{"token_count: 42", "secret_ttl: 30"} {
		if !strings.Contains(redactSensitiveLines(in), "redacted") {
			t.Errorf("expected deliberate over-redaction for %q", in)
		}
	}
}

// bodyHead is the card path; assert the gate is wired there, not just in the
// helper — a fix that lives only in the helper is a fix nobody benefits from.
func TestBodyHeadRedacts(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"app.properties": "name=svc\nadmin.password=SUPERSECRET\n"})
	n := schema.Node{Source: "app.properties", Location: "L2"}
	got := bodyHead(dir, n)
	if strings.Contains(got, "SUPERSECRET") {
		t.Fatalf("card body leaked the credential: %q", got)
	}
	if !strings.Contains(got, "admin.password") {
		t.Fatalf("card body lost the key: %q", got)
	}
}

// hydrateHits is the `query --include-content` path — the second choke point.
func TestHydrateHitsRedacts(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"compose.yaml": "services:\n  db:\n    environment:\n      DB_URL: postgres://u:leaked@db/app\n"})
	hits := []query.Hit{{Node: schema.Node{Source: "compose.yaml", Location: "L4"}}}
	hydrateHits(hits, dir)
	if strings.Contains(hits[0].Content, "leaked") {
		t.Fatalf("query content leaked the credential: %q", hits[0].Content)
	}
	if !strings.Contains(hits[0].Content, "u:***@db") {
		t.Fatalf("expected masked userinfo, got %q", hits[0].Content)
	}
}
