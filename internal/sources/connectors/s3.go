// The s3 connector — stdlib-only bucket/prefix capture (ADR
// 2026-07-17-bundled-adapter-templates, single-binary autopsy: minio-go is
// BANNED — its transitive rs/xid init cost killed it; SigV4 + two XML
// listings are all capture needs).
//
// Two URL forms (M1 disambiguation, hard error when ambiguous):
//
//	s3://KEY:SECRET@endpoint[:port]/bucket[/prefix]?region=…[&tls=true]
//	    userinfo = credentials, host MUST be dotted or ported (an endpoint —
//	    MinIO etc.); path-style requests; plain http unless tls=true.
//	s3://bucket[/prefix]
//	    no userinfo, host without dot or port ⇒ AWS convention: endpoint
//	    s3.<region>.amazonaws.com, virtual-host style, credentials from
//	    AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN — the
//	    standard chain's ENV tier only. Profiles / IAM roles / SSO need the
//	    adapter-script lane (a script resolves creds, exports them, calls
//	    capture back).
//
// The logical-shape rule: ListObjectsV2 with delimiter=/ — PREFIXES only,
// depth capped at 2 levels, at most 500 prefixes total; objects are NEVER
// nodes. Any cap that truncates is reported in root-node metadata ("capped
// at 500 prefixes"), never silent. Errors name scheme + host only.
package connectors

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/sources"
)

const (
	s3Producer    = "s3"
	s3Timeout     = 10 * time.Second
	s3MaxBody     = 8 << 20
	s3MaxPrefixes = 500 // total across all buckets and levels
	s3MaxDepth    = 2   // bucket root prefixes + one level inside each
	s3MaxPages    = 8   // continuation pages per listing — bounded work
	// sha256 of the empty string — the payload hash of every GET.
	s3EmptyPayloadSHA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

type s3Connector struct{}

func init() { sources.Register(s3Connector{}) }

func (s3Connector) Scheme() string { return "s3" }

func (s3Connector) Example() string {
	return "s3://$MINIO_KEY:$MINIO_SECRET@minio.internal:9000/docs?region=us-east-1"
}

func (s3Connector) Params() []sources.Param {
	return []sources.Param{
		{Name: "KEY:SECRET userinfo", Desc: "access key + secret for a custom endpoint (MinIO etc.); endpoint host must be dotted or ported", Cred: true},
		{Name: "region", Desc: "signing region (default: AWS_REGION / AWS_DEFAULT_REGION env, else us-east-1)"},
		{Name: "tls", Desc: "tls=true dials a custom endpoint over https (default http — the MinIO default)"},
		{Name: "(AWS form)", Desc: "s3://bucket[/prefix] — bare bucket host, credentials from AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY[/AWS_SESSION_TOKEN] env only; profiles/IAM need the adapter-script lane"},
	}
}

func (s3Connector) Capture(ctx context.Context, value string) (*schema.Batch, error) {
	cfg, err := parseS3Source(value, os.Getenv)
	if err != nil {
		return nil, err
	}
	return s3Capture(ctx, cfg)
}

// s3Config is one parsed source. endpoint is the dial base
// ("http://host:port" or "https://bucket.s3.region.amazonaws.com");
// rootID is the sanitized store identity ("s3://host:port" / "s3://bucket").
type s3Config struct {
	endpoint     string
	rootID       string
	bucket       string // "" = list all buckets (endpoint form only)
	prefix       string
	accessKey    string
	secretKey    string
	sessionToken string
	region       string
	pathStyle    bool
}

const s3BothForms = "s3://KEY:SECRET@endpoint:port/bucket[/prefix] (userinfo + dotted-or-ported endpoint host) or s3://bucket[/prefix] (no userinfo, bare bucket host, AWS env credentials)"

// parseS3Source applies the M1 disambiguation rule. getenv is injected so
// the table is unit-testable without touching the process env.
func parseS3Source(raw string, getenv func(string) string) (*s3Config, error) {
	rest := strings.TrimPrefix(raw, "s3://")
	query := ""
	if q := strings.Index(rest, "?"); q >= 0 {
		rest, query = rest[:q], rest[q+1:]
	}
	authority, path := rest, ""
	if sl := strings.Index(rest, "/"); sl >= 0 {
		authority, path = rest[:sl], strings.Trim(rest[sl+1:], "/")
	}
	userinfo, hasUser := "", false
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		userinfo, authority, hasUser = authority[:at], authority[at+1:], true
	}
	if authority == "" {
		return nil, fmt.Errorf("s3: empty host — use %s", s3BothForms)
	}
	params := map[string]string{}
	for _, kv := range strings.Split(query, "&") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			params[strings.ToLower(k)] = v
		}
	}
	region := params["region"]
	if region == "" {
		region = getenv("AWS_REGION")
	}
	if region == "" {
		region = getenv("AWS_DEFAULT_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}
	endpointHost := strings.Contains(authority, ".") || strings.Contains(authority, ":")

	if hasUser {
		if !endpointHost {
			return nil, fmt.Errorf("s3: ambiguous URL — host %q has no dot or port, but userinfo means the endpoint form; use %s", authority, s3BothForms)
		}
		key, secret, ok := strings.Cut(userinfo, ":")
		if !ok || key == "" || secret == "" {
			return nil, fmt.Errorf("s3 %s: the endpoint form needs KEY:SECRET userinfo (both parts)", authority)
		}
		scheme := "http"
		if params["tls"] == "true" {
			scheme = "https"
		}
		bucket, prefix, _ := strings.Cut(path, "/")
		return &s3Config{
			endpoint: scheme + "://" + authority, rootID: "s3://" + authority,
			bucket: bucket, prefix: prefix,
			accessKey: key, secretKey: secret, region: region, pathStyle: true,
		}, nil
	}
	if endpointHost {
		return nil, fmt.Errorf("s3: ambiguous URL — host %q could be an endpoint or a dotted bucket name; use %s", authority, s3BothForms)
	}
	// AWS form: the host IS the bucket; env-tier credentials only.
	key, secret := getenv("AWS_ACCESS_KEY_ID"), getenv("AWS_SECRET_ACCESS_KEY")
	if key == "" || secret == "" {
		return nil, fmt.Errorf("s3 %s: the AWS form needs AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY in the environment (the credential chain's env tier only — profiles/IAM roles need the adapter-script lane, .ctxoptimize/adapters/)", authority)
	}
	bucket, prefix := authority, path
	return &s3Config{
		endpoint: "https://" + bucket + ".s3." + region + ".amazonaws.com",
		rootID:   "s3://" + bucket, bucket: bucket, prefix: prefix,
		accessKey: key, secretKey: secret, sessionToken: getenv("AWS_SESSION_TOKEN"),
		region: region, pathStyle: false,
	}, nil
}

// s3Capture walks bucket(s) → prefixes (≤ s3MaxDepth levels, ≤ s3MaxPrefixes
// total) and emits the batch. Deterministic: buckets and prefixes sorted;
// caps reported in root metadata.
func s3Capture(ctx context.Context, cfg *s3Config) (*schema.Batch, error) {
	client := &s3Client{cfg: cfg, http: &http.Client{Timeout: s3Timeout}}
	b := &schema.Batch{Producer: s3Producer}

	var buckets []string
	rootMeta := map[string]string{"endpoint_style": styleName(cfg.pathStyle), "region": cfg.region}
	if cfg.bucket != "" {
		buckets = []string{cfg.bucket}
	} else {
		names, err := client.listBuckets(ctx)
		if err != nil {
			return nil, err
		}
		sort.Strings(names)
		buckets = names
		rootMeta["buckets"] = strconv.Itoa(len(names))
	}

	// Root node: the endpoint (endpoint form) or the bucket itself (AWS form
	// / endpoint form scoped to one bucket still gets an endpoint root).
	rootKind, rootLabel := "s3", strings.TrimPrefix(cfg.rootID, "s3://")
	if !cfg.pathStyle {
		rootKind = "bucket" // AWS form: the root IS the bucket
	}
	root := schema.Node{ID: cfg.rootID, Label: rootLabel, Kind: rootKind, FileType: "infra", Source: cfg.rootID, Metadata: rootMeta}
	b.Nodes = append(b.Nodes, root)

	total, capped := 0, false
	for _, bucket := range buckets {
		bucketID := cfg.rootID
		if cfg.pathStyle {
			bucketID = cfg.rootID + "/" + bucket
			b.Nodes = append(b.Nodes, schema.Node{
				ID: bucketID, Label: bucket, Kind: "bucket", FileType: "infra", Source: cfg.rootID,
			})
			b.Edges = append(b.Edges, containsEdge(cfg.rootID, bucketID))
		}
		basePrefix := cfg.prefix
		if basePrefix != "" && !strings.HasSuffix(basePrefix, "/") {
			basePrefix += "/"
		}
		level1, keyCount, trunc, err := client.listPrefixes(ctx, bucket, basePrefix, s3MaxPrefixes-total)
		if err != nil {
			return nil, err
		}
		capped = capped || trunc
		setMeta(&b.Nodes[len(b.Nodes)-1], "key_count", strconv.Itoa(keyCount))
		for _, p := range level1 {
			total++
			pid := bucketID + "/" + p
			node := schema.Node{ID: pid, Label: p, Kind: "prefix", FileType: "infra", Source: cfg.rootID}
			if s3MaxDepth < 2 || total >= s3MaxPrefixes {
				// No budget (or no depth) left to descend — the deeper
				// structure is not enumerated: that is a reported cap.
				if s3MaxDepth >= 2 {
					capped = true
				}
				b.Nodes = append(b.Nodes, node)
				b.Edges = append(b.Edges, containsEdge(bucketID, pid))
				continue
			}
			// Descend one level: the sub-listing yields this prefix's
			// approximate object count (KeyCount) + its child prefixes.
			level2, kc, trunc2, err := client.listPrefixes(ctx, bucket, p, s3MaxPrefixes-total)
			if err != nil {
				return nil, err
			}
			capped = capped || trunc2
			node.Metadata = map[string]string{"key_count": strconv.Itoa(kc)}
			b.Nodes = append(b.Nodes, node)
			b.Edges = append(b.Edges, containsEdge(bucketID, pid))
			for _, p2 := range level2 {
				if total >= s3MaxPrefixes {
					capped = true
					break
				}
				total++
				cid := bucketID + "/" + p2
				b.Nodes = append(b.Nodes, schema.Node{
					ID: cid, Label: p2, Kind: "prefix", FileType: "infra", Source: cfg.rootID,
				})
				b.Edges = append(b.Edges, containsEdge(pid, cid))
			}
		}
	}
	// Caps REPORTED, never silent (the logical-shape rule): the summary fact
	// rides the root node so it survives into the store.
	b.Nodes[0].Metadata["prefixes"] = strconv.Itoa(total)
	b.Nodes[0].Metadata["depth_limit"] = strconv.Itoa(s3MaxDepth)
	if capped {
		b.Nodes[0].Metadata["capped"] = fmt.Sprintf("capped at %d prefixes (depth ≤%d) — deeper structure not enumerated", s3MaxPrefixes, s3MaxDepth)
	}
	return b, nil
}

func styleName(pathStyle bool) string {
	if pathStyle {
		return "path"
	}
	return "virtual-host"
}

func setMeta(n *schema.Node, k, v string) {
	if n.Metadata == nil {
		n.Metadata = map[string]string{}
	}
	n.Metadata[k] = v
}

// ---- minimal S3 REST client (GET-only, SigV4) ----

type s3Client struct {
	cfg  *s3Config
	http *http.Client
}

type s3CommonPrefix struct {
	Prefix string `xml:"Prefix"`
}

type s3ListBucketResult struct {
	XMLName               xml.Name         `xml:"ListBucketResult"`
	KeyCount              int              `xml:"KeyCount"`
	IsTruncated           bool             `xml:"IsTruncated"`
	NextContinuationToken string           `xml:"NextContinuationToken"`
	CommonPrefixes        []s3CommonPrefix `xml:"CommonPrefixes"`
}

type s3ListAllMyBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	Buckets []struct {
		Name string `xml:"Name"`
	} `xml:"Buckets>Bucket"`
}

func (c *s3Client) listBuckets(ctx context.Context) ([]string, error) {
	body, err := c.doGET(ctx, "/", nil)
	if err != nil {
		return nil, err
	}
	var res s3ListAllMyBucketsResult
	if err := xml.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("s3 %s: ListBuckets response is not valid XML", c.host())
	}
	names := make([]string, 0, len(res.Buckets))
	for _, b := range res.Buckets {
		names = append(names, b.Name)
	}
	return names, nil
}

// listPrefixes runs ListObjectsV2 with delimiter=/ under prefix, following
// continuation tokens (bounded by s3MaxPages and the caller's remaining
// prefix budget). Returns sorted common prefixes, the accumulated KeyCount
// (approximate object count for this prefix), and whether truncation hit.
func (c *s3Client) listPrefixes(ctx context.Context, bucket, prefix string, budget int) (prefixes []string, keyCount int, capped bool, err error) {
	if budget < 0 {
		budget = 0
	}
	path := "/"
	if c.cfg.pathStyle {
		path = "/" + bucket
	}
	token := ""
	for page := 0; page < s3MaxPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, false, fmt.Errorf("s3 %s: %v", c.host(), err)
		}
		query := map[string]string{"list-type": "2", "delimiter": "/"}
		if prefix != "" {
			query["prefix"] = prefix
		}
		if token != "" {
			query["continuation-token"] = token
		}
		body, err := c.doGET(ctx, path, query)
		if err != nil {
			return nil, 0, false, err
		}
		var res s3ListBucketResult
		if err := xml.Unmarshal(body, &res); err != nil {
			return nil, 0, false, fmt.Errorf("s3 %s: ListObjectsV2 response is not valid XML", c.host())
		}
		keyCount += res.KeyCount
		for _, p := range res.CommonPrefixes {
			if len(prefixes) >= budget {
				capped = true
				break
			}
			prefixes = append(prefixes, p.Prefix)
		}
		if capped || !res.IsTruncated || res.NextContinuationToken == "" {
			if !capped && res.IsTruncated && res.NextContinuationToken == "" {
				capped = true // server says more, but gave no token — report, don't loop
			}
			sort.Strings(prefixes)
			return prefixes, keyCount, capped, nil
		}
		token = res.NextContinuationToken
	}
	sort.Strings(prefixes)
	return prefixes, keyCount, true, nil // page cap hit — reported, never silent
}

func (c *s3Client) host() string { return strings.TrimPrefix(c.cfg.rootID, "s3://") }

// doGET signs and executes one GET. Every error names scheme+host (and the
// request path — bucket/prefix, never a credential).
func (c *s3Client) doGET(ctx context.Context, path string, query map[string]string) ([]byte, error) {
	canonicalQuery := s3CanonicalQuery(query)
	u := c.cfg.endpoint + s3URIEncodePath(path)
	if canonicalQuery != "" {
		u += "?" + canonicalQuery
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("s3 %s: invalid request URL", c.host())
	}
	s3SignV4(req, s3URIEncodePath(path), canonicalQuery, c.cfg, time.Now())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3 %s: request failed for %s", c.host(), path)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, s3MaxBody))
	if err != nil {
		return nil, fmt.Errorf("s3 %s: read response for %s", c.host(), path)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("s3 %s: HTTP %d for %s", c.host(), resp.StatusCode, path)
	}
	return body, nil
}

// s3SignV4 signs one GET request with AWS Signature Version 4 (empty
// payload). canonicalURI and canonicalQuery are the exact encoded strings the
// request was built from — signing reuses them rather than re-deriving from
// req.URL (net/url normalization must never diverge from what was signed).
func s3SignV4(req *http.Request, canonicalURI, canonicalQuery string, cfg *s3Config, now time.Time) {
	amzDate := now.UTC().Format("20060102T150405Z")
	dateStamp := amzDate[:8]
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", s3EmptyPayloadSHA)
	headers := map[string]string{
		"host":                 req.URL.Host,
		"x-amz-content-sha256": s3EmptyPayloadSHA,
		"x-amz-date":           amzDate,
	}
	if cfg.sessionToken != "" {
		req.Header.Set("x-amz-security-token", cfg.sessionToken)
		headers["x-amz-security-token"] = cfg.sessionToken
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, k := range names {
		canonicalHeaders.WriteString(k + ":" + headers[k] + "\n")
	}
	signedHeaders := strings.Join(names, ";")
	canonicalRequest := strings.Join([]string{
		http.MethodGet, canonicalURI, canonicalQuery,
		canonicalHeaders.String(), signedHeaders, s3EmptyPayloadSHA,
	}, "\n")
	scope := dateStamp + "/" + cfg.region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", amzDate, scope, hexSHA256([]byte(canonicalRequest)),
	}, "\n")
	key := hmacSHA256([]byte("AWS4"+cfg.secretKey), dateStamp)
	key = hmacSHA256(key, cfg.region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))
	req.Header.Set("Authorization",
		"AWS4-HMAC-SHA256 Credential="+cfg.accessKey+"/"+scope+
			", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// s3CanonicalQuery renders query params in SigV4 canonical form: URI-encoded
// keys and values, sorted by encoded key, joined with '&'. The SAME string is
// used for the request URL — one encoding, no divergence.
func s3CanonicalQuery(query map[string]string) string {
	if len(query) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(query))
	for k, v := range query {
		pairs = append(pairs, s3URIEncode(k, true)+"="+s3URIEncode(v, true))
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

// s3URIEncodePath encodes a path per the SigV4 rules, keeping '/'.
func s3URIEncodePath(p string) string { return s3URIEncode(p, false) }

// s3URIEncode is the AWS SigV4 URI encoding: unreserved = A-Z a-z 0-9 - _ . ~,
// '/' kept unless encodeSlash, everything else %XX uppercase.
func s3URIEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			b.WriteString(fmt.Sprintf("%%%02X", c))
		}
	}
	return b.String()
}
