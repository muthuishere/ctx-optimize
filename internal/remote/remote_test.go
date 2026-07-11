package remote

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir(), "mod")
	if err != nil {
		t.Fatal(err)
	}
	b := &schema.Batch{Producer: "test", Nodes: []schema.Node{
		{ID: "a", Label: "a", Kind: "function", FileType: "code", Source: "a.go"},
	}}
	if _, _, err := s.Merge(b); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPushPullRoundtripFile(t *testing.T) {
	src := testStore(t)
	remoteDir := t.TempDir()
	b, err := Open("file://" + remoteDir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Push(src, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transferred) == 0 {
		t.Fatal("first push transferred nothing")
	}

	// Second push moves nothing — incremental by manifest hash.
	res, err = Push(src, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transferred) != 0 {
		t.Fatalf("unchanged push should transfer 0, got %v", res.Transferred)
	}

	// Pull into a fresh store reproduces the graph.
	dst, err := store.Open(t.TempDir(), "mod")
	if err != nil {
		t.Fatal(err)
	}
	res, err = Pull(dst, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transferred) == 0 {
		t.Fatal("pull transferred nothing")
	}
	nodes, err := dst.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].ID != "a" {
		t.Fatalf("pulled graph wrong: %+v", nodes)
	}

	// Second pull is a no-op.
	res, err = Pull(dst, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transferred) != 0 {
		t.Fatalf("unchanged pull should transfer 0, got %v", res.Transferred)
	}
}

func TestOpenRejectsUnknownScheme(t *testing.T) {
	if _, err := Open("ftp://nope"); err == nil {
		t.Fatal("unknown scheme accepted")
	}
}

// TestS3SigV4Shape drives the s3 backend against a local httptest server and
// asserts the SigV4 envelope is present and well-formed. Full AWS conformance
// is covered by the integration test below (env-gated).
func TestS3SigV4Shape(t *testing.T) {
	var got *http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)
		gotBody = buf
		w.WriteHeader(200)
	}))
	defer srv.Close()

	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("AWS_ENDPOINT_URL", srv.URL)
	t.Setenv("AWS_REGION", "eu-central-1")

	b, err := newS3Backend("bucket", "prefix")
	if err != nil {
		t.Fatal(err)
	}
	b.now = func() time.Time { return time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC) }
	if err := b.Put("graph/nodes.ndjson", []byte("data")); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("server saw no request")
	}
	if got.URL.Path != "/bucket/prefix/graph/nodes.ndjson" {
		t.Fatalf("path-style URL wrong: %s", got.URL.Path)
	}
	auth := got.Header.Get("Authorization")
	for _, want := range []string{"AWS4-HMAC-SHA256", "Credential=test-key/20260711/eu-central-1/s3/aws4_request", "SignedHeaders=", "Signature="} {
		if !strings.Contains(auth, want) {
			t.Fatalf("auth header missing %q: %s", want, auth)
		}
	}
	if got.Header.Get("x-amz-content-sha256") == "" {
		t.Fatal("payload hash header missing")
	}
	if string(gotBody) != "data" {
		t.Fatalf("body corrupted: %q", gotBody)
	}
}

// Integration: real S3-compatible endpoint. Opt-in via env, runtime skip
// otherwise (house style: no build tags).
func TestS3Integration(t *testing.T) {
	target := os.Getenv("CTX_OPTIMIZE_TEST_S3") // e.g. s3://bucket/ctx-optimize-test
	if target == "" {
		t.Skip("set CTX_OPTIMIZE_TEST_S3=s3://bucket/prefix (+ AWS_* env) to run")
	}
	b, err := Open(target)
	if err != nil {
		t.Fatal(err)
	}
	src := testStore(t)
	if _, err := Push(src, b); err != nil {
		t.Fatal(err)
	}
	dst, err := store.Open(t.TempDir(), "mod")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Pull(dst, b); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dst.Dir, "graph", "nodes.ndjson"))
	if err != nil || len(data) == 0 {
		t.Fatalf("pulled store empty: %v", err)
	}
}
