// S3-compatible backend over plain net/http with hand-rolled SigV4 — no AWS
// SDK. ~150 lines buys: AWS S3, Cloudflare R2, Hetzner, MinIO, anything
// S3-compatible, with zero dependencies and a binary that stays small.
//
// Credentials are read from the standard env vars AT CALL TIME and are never
// stored, printed, or logged. Endpoint override via AWS_ENDPOINT_URL (R2,
// MinIO, Hetzner all need it); region via AWS_REGION (default us-east-1).
package remote

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type s3Backend struct {
	bucket   string
	prefix   string
	endpoint string // scheme://host, path-style addressing
	region   string
	creds    Options // explicit credentials; empty fields fall back to env
	client   *http.Client
	now      func() time.Time // injected for testability
}

func newS3Backend(bucket, prefix string, opts Options) (*s3Backend, error) {
	region := opts.Region
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}
	endpoint := opts.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("AWS_ENDPOINT_URL")
	}
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://s3.%s.amazonaws.com", region)
	}
	return &s3Backend{
		bucket:   bucket,
		prefix:   prefix,
		endpoint: strings.TrimRight(endpoint, "/"),
		region:   region,
		creds:    opts,
		client:   &http.Client{Timeout: 60 * time.Second},
		now:      time.Now,
	}, nil
}

func (s *s3Backend) objectURL(key string) string {
	full := key
	if s.prefix != "" {
		full = s.prefix + "/" + key
	}
	// Path-style: works on every S3-compatible endpoint including MinIO.
	return fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, escapePath(full))
}

func (s *s3Backend) Put(key string, data []byte) error {
	resp, err := s.do("PUT", key, data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("s3 put %s: %s: %s", key, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *s3Backend) Get(key string) ([]byte, error) {
	resp, err := s.do("GET", key, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("s3 get %s: %s: %s", key, resp.Status, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func (s *s3Backend) do(method, key string, body []byte) (*http.Response, error) {
	accessKey := firstOf(s.creds.AccessKeyID, os.Getenv("AWS_ACCESS_KEY_ID"))
	secretKey := firstOf(s.creds.SecretAccessKey, os.Getenv("AWS_SECRET_ACCESS_KEY"))
	if accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("s3 remote needs credentials — in ctx-optimize.json (${VAR} placeholders) or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY in the environment")
	}
	req, err := http.NewRequest(method, s.objectURL(key), strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	s.sign(req, body, accessKey, secretKey, firstOf(s.creds.SessionToken, os.Getenv("AWS_SESSION_TOKEN")))
	return s.client.Do(req)
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// sign implements AWS Signature Version 4 for a single request.
func (s *s3Backend) sign(req *http.Request, body []byte, accessKey, secretKey, sessionToken string) {
	t := s.now().UTC()
	amzDate := t.Format("20060102T150405Z")
	dateStamp := t.Format("20060102")

	payloadHash := sha256hex(body)
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("host", req.URL.Host)
	if sessionToken != "" {
		req.Header.Set("x-amz-security-token", sessionToken)
	}

	// Canonical request over the signed headers, sorted.
	var headerNames []string
	canonical := map[string]string{}
	for name, vals := range req.Header {
		lower := strings.ToLower(name)
		headerNames = append(headerNames, lower)
		canonical[lower] = strings.TrimSpace(strings.Join(vals, ","))
	}
	sort.Strings(headerNames)
	var canonicalHeaders, signedHeaders strings.Builder
	for i, name := range headerNames {
		canonicalHeaders.WriteString(name + ":" + canonical[name] + "\n")
		if i > 0 {
			signedHeaders.WriteString(";")
		}
		signedHeaders.WriteString(name)
	}

	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonicalHeaders.String(),
		signedHeaders.String(),
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{dateStamp, s.region, "s3", "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256hex([]byte(canonicalRequest)),
	}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, s.region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, scope, signedHeaders.String(), signature,
	))
	// Host header must be set on the request itself, not just for signing.
	req.Host = req.URL.Host
	req.Header.Del("host")
}

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

// escapePath encodes each path segment per SigV4's canonical URI rules:
// strict RFC-3986 — everything but unreserved chars is percent-encoded
// (url.PathEscape leaves sub-delims like '+' and '=' alone, which makes S3
// reject the signature).
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = escapeSegment(part)
	}
	return strings.Join(parts, "/")
}

func escapeSegment(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			sb.WriteByte(c)
		} else {
			fmt.Fprintf(&sb, "%%%02X", c)
		}
	}
	return sb.String()
}
