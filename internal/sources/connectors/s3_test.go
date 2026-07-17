package connectors

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/sources"
)

const (
	s3TestKey    = "TESTKEY"
	s3TestSecret = "sv-FAKE-SECRET-77xq" // planted; must never appear in any output
)

var (
	s3AuthShapeRe = regexp.MustCompile(`^AWS4-HMAC-SHA256 Credential=` + s3TestKey +
		`/\d{8}/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date(;x-amz-security-token)?, Signature=[0-9a-f]{64}$`)
	s3AmzDateRe = regexp.MustCompile(`^\d{8}T\d{6}Z$`)
)

// checkSigV4Lite asserts the SigV4 request surface: Authorization shape,
// x-amz-date, and the empty-payload hash — verification-lite per the spec.
func checkSigV4Lite(t *testing.T, r *http.Request) {
	t.Helper()
	if auth := r.Header.Get("Authorization"); !s3AuthShapeRe.MatchString(auth) {
		t.Errorf("Authorization header shape wrong: %q", auth)
	}
	if d := r.Header.Get("x-amz-date"); !s3AmzDateRe.MatchString(d) {
		t.Errorf("x-amz-date = %q", d)
	}
	if h := r.Header.Get("x-amz-content-sha256"); h != s3EmptyPayloadSHA {
		t.Errorf("x-amz-content-sha256 = %q", h)
	}
}

func listingXML(keyCount int, truncated bool, nextToken string, prefixes ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><ListBucketResult>`)
	fmt.Fprintf(&b, "<KeyCount>%d</KeyCount><IsTruncated>%v</IsTruncated>", keyCount, truncated)
	if nextToken != "" {
		fmt.Fprintf(&b, "<NextContinuationToken>%s</NextContinuationToken>", nextToken)
	}
	for _, p := range prefixes {
		fmt.Fprintf(&b, "<CommonPrefixes><Prefix>%s</Prefix></CommonPrefixes>", p)
	}
	// An object key rides along — it must NEVER become a node.
	b.WriteString("<Contents><Key>a/planted-object-file.txt</Key></Contents>")
	b.WriteString("</ListBucketResult>")
	return b.String()
}

// s3EndpointURL builds an endpoint-form source URL against the test server,
// pinning region=us-east-1 so the auth-shape regex never depends on the
// machine's AWS_REGION env.
func s3EndpointURL(ts *httptest.Server, tail string) string {
	sep := "?"
	if strings.Contains(tail, "?") {
		sep = "&"
	}
	return "s3://" + s3TestKey + ":" + s3TestSecret + "@" +
		strings.TrimPrefix(ts.URL, "http://") + tail + sep + "region=us-east-1"
}

func TestS3ParseDisambiguation(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	awsEnv := map[string]string{
		"AWS_ACCESS_KEY_ID": "AKIAENVKEY", "AWS_SECRET_ACCESS_KEY": "env-secret-xyz",
		"AWS_SESSION_TOKEN": "env-token-abc", "AWS_REGION": "us-west-2",
	}
	awsGetenv := func(k string) string { return awsEnv[k] }

	t.Run("endpoint form, ported host", func(t *testing.T) {
		cfg, err := parseS3Source("s3://AK:SK@minio.internal:9000/docs/reports/q1?region=eu-central-1", getenv)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.endpoint != "http://minio.internal:9000" || !cfg.pathStyle {
			t.Errorf("cfg = %+v (want path-style http endpoint — the MinIO default)", cfg)
		}
		if cfg.bucket != "docs" || cfg.prefix != "reports/q1" || cfg.region != "eu-central-1" {
			t.Errorf("cfg = %+v", cfg)
		}
		if cfg.accessKey != "AK" || cfg.secretKey != "SK" {
			t.Errorf("creds not taken from userinfo")
		}
		if cfg.rootID != "s3://minio.internal:9000" {
			t.Errorf("rootID = %q (must carry no userinfo)", cfg.rootID)
		}
	})
	t.Run("endpoint form, dotted host + tls", func(t *testing.T) {
		cfg, err := parseS3Source("s3://AK:SK@storage.example.com/docs?tls=true", getenv)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.endpoint != "https://storage.example.com" {
			t.Errorf("endpoint = %q, want https with tls=true", cfg.endpoint)
		}
	})
	t.Run("userinfo + bare host is ambiguous", func(t *testing.T) {
		_, err := parseS3Source("s3://AK:SK@docs/x", getenv)
		if err == nil || !strings.Contains(err.Error(), "s3://bucket[/prefix]") {
			t.Fatalf("want hard error naming both forms, got %v", err)
		}
		if strings.Contains(err.Error(), "SK") && strings.Contains(err.Error(), "AK:") {
			t.Errorf("creds leaked into error: %v", err)
		}
	})
	t.Run("no userinfo + dotted host is ambiguous", func(t *testing.T) {
		_, err := parseS3Source("s3://my.bucket/x", awsGetenv)
		if err == nil || !strings.Contains(err.Error(), "s3://KEY:SECRET@endpoint:port") ||
			!strings.Contains(err.Error(), "s3://bucket[/prefix]") {
			t.Fatalf("want hard error naming both forms, got %v", err)
		}
	})
	t.Run("AWS form", func(t *testing.T) {
		cfg, err := parseS3Source("s3://docs/reports", awsGetenv)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.endpoint != "https://docs.s3.us-west-2.amazonaws.com" || cfg.pathStyle {
			t.Errorf("cfg = %+v (want virtual-host AWS endpoint)", cfg)
		}
		if cfg.accessKey != "AKIAENVKEY" || cfg.secretKey != "env-secret-xyz" || cfg.sessionToken != "env-token-abc" {
			t.Errorf("env-tier credential chain not applied: %+v", cfg)
		}
		if cfg.bucket != "docs" || cfg.prefix != "reports" || cfg.rootID != "s3://docs" {
			t.Errorf("cfg = %+v", cfg)
		}
	})
	t.Run("AWS form, region query beats env", func(t *testing.T) {
		cfg, err := parseS3Source("s3://docs?region=ap-south-1", awsGetenv)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.region != "ap-south-1" || cfg.endpoint != "https://docs.s3.ap-south-1.amazonaws.com" {
			t.Errorf("cfg = %+v", cfg)
		}
	})
	t.Run("AWS form without env creds", func(t *testing.T) {
		_, err := parseS3Source("s3://docs", getenv)
		if err == nil || !strings.Contains(err.Error(), "AWS_ACCESS_KEY_ID") ||
			!strings.Contains(err.Error(), "adapter-script lane") {
			t.Fatalf("want env-tier error naming the adapter lane, got %v", err)
		}
	})
}

func TestS3CaptureTwoLevels(t *testing.T) {
	var listCalls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkSigV4Lite(t, r)
		if r.URL.Query().Get("list-type") != "2" || r.URL.Query().Get("delimiter") != "/" {
			t.Errorf("not a delimited ListObjectsV2: %s", r.URL.String())
		}
		if r.URL.Path != "/docs" {
			t.Errorf("path-style request expected, got path %q", r.URL.Path)
		}
		listCalls++
		switch r.URL.Query().Get("prefix") {
		case "":
			fmt.Fprint(w, listingXML(5, false, "", "a/", "b/"))
		case "a/":
			fmt.Fprint(w, listingXML(3, false, "", "a/x/"))
		case "b/":
			fmt.Fprint(w, listingXML(1, false, ""))
		default:
			t.Errorf("unexpected descent past depth 2: prefix %q", r.URL.Query().Get("prefix"))
			fmt.Fprint(w, listingXML(0, false, ""))
		}
	}))
	defer ts.Close()

	start := time.Now()
	b, err := s3Connector{}.Capture(context.Background(), s3EndpointURL(ts, "/docs"))
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("hermetic capture took %v, want < 50ms", d)
	}
	if listCalls != 3 {
		t.Errorf("listCalls = %d, want 3 (root + two level-1 prefixes)", listCalls)
	}

	host := strings.TrimPrefix(ts.URL, "http://")
	rootID := "s3://" + host
	bucketID := rootID + "/docs"
	for _, id := range []string{rootID, bucketID, bucketID + "/a/", bucketID + "/b/", bucketID + "/a/x/"} {
		if nodeByID(b, id) == nil {
			t.Errorf("missing node %q; ids: %v", id, nodeIDs(b))
		}
	}
	if a := nodeByID(b, bucketID+"/a/"); a != nil && a.Metadata["key_count"] != "3" {
		t.Errorf("a/ key_count = %v, want 3 (approximate KeyCount)", a.Metadata)
	}
	if !hasEdge(b, bucketID, bucketID+"/a/", "contains") || !hasEdge(b, bucketID+"/a/", bucketID+"/a/x/", "contains") {
		t.Error("contains chain bucket→prefix→prefix missing")
	}

	out := batchJSON(t, b)
	if strings.Contains(out, "planted-object-file") {
		t.Error("an object key became a node — objects are NEVER nodes")
	}
	if strings.Contains(out, s3TestSecret) || strings.Contains(out, s3TestKey+":") {
		t.Error("credentials leaked into the batch")
	}
	if root := nodeByID(b, rootID); root.Metadata["capped"] != "" {
		t.Errorf("no cap was hit but capped = %q", root.Metadata["capped"])
	}
}

func TestS3CaptureListBuckets(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkSigV4Lite(t, r)
		if r.URL.Path == "/" && r.URL.Query().Get("list-type") == "" {
			fmt.Fprint(w, `<?xml version="1.0"?><ListAllMyBucketsResult><Buckets>`+
				`<Bucket><Name>beta</Name></Bucket><Bucket><Name>alpha</Name></Bucket>`+
				`</Buckets></ListAllMyBucketsResult>`)
			return
		}
		fmt.Fprint(w, listingXML(2, false, ""))
	}))
	defer ts.Close()

	b, err := s3Connector{}.Capture(context.Background(), s3EndpointURL(ts, ""))
	if err != nil {
		t.Fatal(err)
	}
	rootID := "s3://" + strings.TrimPrefix(ts.URL, "http://")
	root := nodeByID(b, rootID)
	if root == nil || root.Metadata["buckets"] != "2" {
		t.Fatalf("root = %+v; ids: %v", root, nodeIDs(b))
	}
	// Buckets sorted regardless of server order.
	if nodeByID(b, rootID+"/alpha") == nil || nodeByID(b, rootID+"/beta") == nil {
		t.Errorf("bucket nodes missing; ids: %v", nodeIDs(b))
	}
	if b.Nodes[1].Label != "alpha" {
		t.Errorf("buckets not sorted: first bucket = %q", b.Nodes[1].Label)
	}
}

func TestS3CaptureCapReported(t *testing.T) {
	prefixes := make([]string, 600)
	for i := range prefixes {
		prefixes[i] = fmt.Sprintf("p%03d/", i)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkSigV4Lite(t, r)
		if r.URL.Query().Get("prefix") == "" {
			fmt.Fprint(w, listingXML(600, false, "", prefixes...))
			return
		}
		fmt.Fprint(w, listingXML(0, false, ""))
	}))
	defer ts.Close()

	b, err := s3Connector{}.Capture(context.Background(), s3EndpointURL(ts, "/big"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, n := range b.Nodes {
		if n.Kind == "prefix" {
			count++
		}
	}
	if count != s3MaxPrefixes {
		t.Errorf("prefix nodes = %d, want capped at %d", count, s3MaxPrefixes)
	}
	root := nodeByID(b, "s3://"+strings.TrimPrefix(ts.URL, "http://"))
	if !strings.Contains(root.Metadata["capped"], "capped at 500") {
		t.Errorf("cap must be REPORTED in the summary fact, got metadata %v", root.Metadata)
	}
	if root.Metadata["prefixes"] != "500" {
		t.Errorf("prefixes fact = %q", root.Metadata["prefixes"])
	}
}

func TestS3ContinuationToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkSigV4Lite(t, r)
		switch {
		case r.URL.Query().Get("prefix") != "":
			fmt.Fprint(w, listingXML(0, false, ""))
		case r.URL.Query().Get("continuation-token") == "":
			fmt.Fprint(w, listingXML(2, true, "tok-1", "one/"))
		default:
			if got := r.URL.Query().Get("continuation-token"); got != "tok-1" {
				t.Errorf("continuation-token = %q", got)
			}
			fmt.Fprint(w, listingXML(2, false, "", "two/"))
		}
	}))
	defer ts.Close()

	b, err := s3Connector{}.Capture(context.Background(), s3EndpointURL(ts, "/docs"))
	if err != nil {
		t.Fatal(err)
	}
	bucketID := "s3://" + strings.TrimPrefix(ts.URL, "http://") + "/docs"
	if nodeByID(b, bucketID+"/one/") == nil || nodeByID(b, bucketID+"/two/") == nil {
		t.Errorf("paginated prefixes missing; ids: %v", nodeIDs(b))
	}
}

func TestS3SessionTokenSigned(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), "x-amz-security-token") {
			t.Errorf("session token not in SignedHeaders: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("x-amz-security-token") != "tok-session-1" {
			t.Errorf("x-amz-security-token = %q", r.Header.Get("x-amz-security-token"))
		}
		fmt.Fprint(w, listingXML(0, false, ""))
	}))
	defer ts.Close()

	cfg := &s3Config{
		endpoint: ts.URL, rootID: "s3://docs", bucket: "docs",
		accessKey: s3TestKey, secretKey: s3TestSecret, sessionToken: "tok-session-1",
		region: "us-east-1", pathStyle: true,
	}
	if _, err := s3Capture(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestS3ErrorsNameHostOnly(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	_, err := s3Connector{}.Capture(context.Background(), s3EndpointURL(ts, "/docs"))
	if err == nil {
		t.Fatal("expected HTTP 403 error")
	}
	msg := err.Error()
	if strings.Contains(msg, s3TestSecret) || strings.Contains(msg, s3TestKey) {
		t.Errorf("credentials leaked into error: %v", err)
	}
	host := strings.TrimPrefix(ts.URL, "http://")
	if !strings.Contains(msg, host) || !strings.Contains(msg, "403") {
		t.Errorf("error should name scheme+host and status: %v", err)
	}
}

func TestS3RouteAndContext(t *testing.T) {
	if name, err := sources.Route("s3://bucket/prefix"); err != nil || name != "s3" {
		t.Errorf("sources.Route(s3://...) = %q, %v", name, err)
	}
	// A cancelled context must abort the dial, error naming host only.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer ts.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s3Connector{}.Capture(ctx, s3EndpointURL(ts, "/docs"))
	if err == nil {
		t.Fatal("expected context-cancelled error")
	}
	if strings.Contains(err.Error(), s3TestSecret) {
		t.Errorf("secret in error: %v", err)
	}
}

var _ = schema.Batch{} // keep the import stable if assertions above change
