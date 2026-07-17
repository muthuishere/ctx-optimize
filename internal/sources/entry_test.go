package sources

import (
	"strings"
	"testing"
)

func TestIsEnvName(t *testing.T) {
	yes := []string{"DATABASE_URL", "_X", "A1", "BILLING_DB_URL"}
	no := []string{"", "1X", "db_url", "Database", "A-B", "A.B", "$A", "postgres://h", "./spec.yaml"}
	for _, s := range yes {
		if !IsEnvName(s) {
			t.Errorf("IsEnvName(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if IsEnvName(s) {
			t.Errorf("IsEnvName(%q) = true, want false", s)
		}
	}
}

func testLookup(t *testing.T) func(string) (string, bool) {
	env := map[string]string{
		"PG_USER":      "alice",
		"PG_PASS":      "p$ss&w=rd@x", // value with $ & = @ — must not re-expand
		"DATABASE_URL": "postgres://u:v@h/db",
		"MINIO_KEY":    "AKIAXXXX",
		"MINIO_SECRET": "wJalrXUtnFEMI/K7MDENG+bPxRfiCY",
	}
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}

func TestExpand(t *testing.T) {
	lookup := testLookup(t)
	cases := []struct {
		entry       string
		want        string
		wantMissing []string
	}{
		{"postgres://host/db?password=$PG_PASS", "postgres://host/db?password=p$ss&w=rd@x", nil},
		{"postgres://$PG_USER:$PG_PASS@host/db", "postgres://alice:p$ss&w=rd@x@host/db", nil},
		{"$PG_PASS", "p$ss&w=rd@x", nil}, // single-pass: value's $ss is NOT a var
		{"DATABASE_URL", "postgres://u:v@h/db", nil},
		{"$DATABASE_URL", "postgres://u:v@h/db", nil},
		{"postgres://$PG_USER:$UNSET_PASS@host/db", "postgres://alice:@host/db", []string{"UNSET_PASS"}},
		{"redis://h?a=${PG_USER}b", "redis://h?a=aliceb", nil},
		{"NOT_SET_BARE", "", []string{"NOT_SET_BARE"}},
		{"plain-path/spec.yaml", "plain-path/spec.yaml", nil},
		{"cost is $5", "cost is $5", nil}, // lone $ digit — literal
	}
	for _, c := range cases {
		got, missing := Expand(c.entry, lookup)
		if got != c.want {
			t.Errorf("Expand(%q) = %q, want %q", c.entry, got, c.want)
		}
		if strings.Join(missing, ",") != strings.Join(c.wantMissing, ",") {
			t.Errorf("Expand(%q) missing = %v, want %v", c.entry, missing, c.wantMissing)
		}
	}
}

func TestExpandBareNameValueGetsLenientTemplatePass(t *testing.T) {
	env := map[string]string{
		"FOLDED_S3_URL": "s3://$MINIO_KEY:$MINIO_SECRET@localhost:9009/docs?region=us-east-1",
		"MINIO_KEY":     "miniouser",
		"MINIO_SECRET":  "se$MINIO_KEY", // value with a resolvable-looking ref — must stay literal
		"DOLLAR_PW_URL": "postgres://u:pa$sword@h/db",
	}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	// The documented folded shape expands one level; substituted values are
	// not rescanned ($MINIO_KEY inside MINIO_SECRET's value stays literal).
	got, missing := Expand("FOLDED_S3_URL", lookup)
	if want := "s3://miniouser:se$MINIO_KEY@localhost:9009/docs?region=us-east-1"; got != want || missing != nil {
		t.Fatalf("folded: %q missing=%v", got, missing)
	}
	// An unresolvable $token in the value is a literal, never missing —
	// provider-issued passwords containing '$' keep working.
	got, missing = Expand("DOLLAR_PW_URL", lookup)
	if want := "postgres://u:pa$sword@h/db"; got != want || missing != nil {
		t.Fatalf("dollar pw: %q missing=%v", got, missing)
	}
}

func TestDetectLiteralCreds(t *testing.T) {
	cases := []struct {
		entry   string
		wantErr bool
	}{
		{"postgres://user:realpass@host/db", true},    // literal password
		{"postgres://host/db?password=literal", true}, // literal secret param
		{"postgres://$U:$P@host/db", false},
		{"postgres://user@host", false}, // username only — allowed (L3)
		{"s3://$K:$S@host", false},
		{"DATABASE_URL", false},
		{"$ORDERS_DB_URL", false},
		{"postgres://user:$P@host", false},                               // literal user + var pass
		{"mongodb://h/db?tlsCertificateKeyFile=/path/client.pem", false}, // key PATH allowed
		{"redis://h?token=abc123", true},
		{"kafka://u:$P@b1:9092,b2:9092?sasl_password=oops", true},
		{"./api/spec.yaml", false},
		{"redis://h?password=$P&db=0", false},
	}
	for _, c := range cases {
		err := DetectLiteralCreds(c.entry)
		if (err != nil) != c.wantErr {
			t.Errorf("DetectLiteralCreds(%q) = %v, wantErr %v", c.entry, err, c.wantErr)
		}
		// The error must carry the skeleton only — never the literal secret.
		if err != nil {
			for _, leak := range []string{"realpass", "abc123", "oops"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("DetectLiteralCreds(%q) error leaks %q: %v", c.entry, leak, err)
				}
			}
		}
	}
}

func TestRoute(t *testing.T) {
	cases := []struct {
		value   string
		want    string
		wantErr bool
	}{
		{"postgres://h/db", "postgres", false},
		{"postgresql://h/db", "postgres", false},
		{"mysql://h/db", "mysql", false},
		{"mongodb://h/db", "mongo", false},
		{"mongodb+srv://c.mongodb.net/db", "mongo", false},
		{"redis://h", "redis", false},
		{"rediss://h", "redis", false},
		{"kafka://b1:9092,b2:9092/x", "kafka", false},
		{"nats://h", "nats", false},
		{"s3://bucket/prefix", "s3", false},
		{"http://h/spec.json", "openapi", false},
		{"https://api.example.com/v3/openapi.json", "openapi", false},
		{"./api/spec.yaml", "openapi", false},
		{"/abs/spec.yaml", "openapi", false},
		{`C:\specs\api.json`, "openapi", false}, // single-letter drive = path
		{"c://weird/but/path", "openapi", false},
		{"POSTGRES://h/db", "postgres", false}, // lowercased
		{"", "", true},
		{"ftp://h/x", "", true},
	}
	for _, c := range cases {
		got, err := Route(c.value)
		if (err != nil) != c.wantErr {
			t.Errorf("Route(%q) err = %v, wantErr %v", c.value, err, c.wantErr)
			continue
		}
		if !c.wantErr && got != c.want {
			t.Errorf("Route(%q) = %q, want %q", c.value, got, c.want)
		}
		if c.wantErr && err != nil && c.value != "" && !strings.Contains(err.Error(), "postgres") {
			t.Errorf("Route(%q) error should list supported schemes: %v", c.value, err)
		}
	}
}

func TestSanitize(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"kafka://u:p@b1:9092,b2:9092/x", "kafka://b1:9092,b2:9092/x"}, // multi-host verbatim
		// AWS secret with unencoded '/' — the defensive last-@ tier.
		{"s3://AKIAXXXX:wJalrXUtnFEMI/K7MDENG+bPxRfiCY@minio.local:9000/bucket", "s3://minio.local:9000/bucket"},
		{"postgres://alice:p@ss:w@rd@host/db", "postgres://host/db"}, // password containing @ and :
		{"mongodb+srv://u:p@cluster0.mongodb.net/db?tlsCertificateKeyFile=/p/k.pem&retryWrites=true",
			"mongodb+srv://cluster0.mongodb.net/db?retryWrites=true"},
		{"postgres://u:p@h/db?sslmode=verify-full&sslkey=/p/k.pem&sslrootcert=/p/ca.pem",
			"postgres://h/db?sslmode=verify-full&sslrootcert=/p/ca.pem"},
		{"redis://:secretonly@h:6379/0", "redis://h:6379/0"},
		// http(s): ALL query params stripped (M2) — the ?token vocab is unbounded.
		{"https://user:tok@api.example.com/spec?token=abc&v=1#frag", "https://api.example.com/spec"},
		{"http://h/x?sig=zzz", "http://h/x"},
		{"postgres://$PG_USER:$PG_PASS@db.internal:5432/billing", "postgres://db.internal:5432/billing"}, // raw template
		{"./api/spec.yaml", "./api/spec.yaml"},
		{"nats://tokenvar@h", "nats://h"}, // token userinfo form
	}
	for _, c := range cases {
		got, ok := Sanitize(c.in)
		if !ok {
			t.Errorf("Sanitize(%q) failed closed unexpectedly", c.in)
			continue
		}
		if got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSourceID(t *testing.T) {
	cases := []struct{ entry, want string }{
		{"DATABASE_URL", "DATABASE_URL"},
		{"$ORDERS_DB_URL", "ORDERS_DB_URL"},
		{"postgres://$PG_USER:$PG_PASS@db.internal:5432/billing", "postgres://db.internal:5432/billing"},
		{"./api/spec.yaml", "./api/spec.yaml"},
	}
	for _, c := range cases {
		if got := SourceID(c.entry); got != c.want {
			t.Errorf("SourceID(%q) = %q, want %q", c.entry, got, c.want)
		}
	}
}

func TestVarNames(t *testing.T) {
	got := VarNames("postgres://$PG_USER:$PG_PASS@host/db?a=${EXTRA}")
	want := "PG_USER,PG_PASS,EXTRA"
	if strings.Join(got, ",") != want {
		t.Errorf("VarNames = %v, want %s", got, want)
	}
	if got := VarNames("DATABASE_URL"); len(got) != 1 || got[0] != "DATABASE_URL" {
		t.Errorf("VarNames(bare) = %v", got)
	}
}
