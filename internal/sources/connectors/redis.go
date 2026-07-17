package connectors

// redis connector (schemes redis://, rediss:// → "redis"): NEVER a full
// keyspace walk — a bounded SCAN sample (COUNT batches, hard cap
// redisScanCap keys), summarized by key prefix before the first ":" (or the
// whole key when there is none). Each prefix becomes one node with an
// approximate count and an example TYPE (one TYPE call per prefix, on the
// first-seen key). DBSIZE and the server version are db-level facts; a hit
// cap is REPORTED as a fact, never silent. Node ids from the sanitized URL.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/sources"
	"github.com/redis/go-redis/v9"
)

// redisScanCap is the hard cap on sampled keys — the logical-shape rule.
const redisScanCap = 5000

// redisScanCount is the COUNT hint per SCAN round trip.
const redisScanCount = 500

type redisConnector struct{}

func init() { sources.Register(&redisConnector{}) }

func (c *redisConnector) Scheme() string { return "redis" }

func (c *redisConnector) Params() []sources.Param {
	return []sources.Param{
		{Name: "user:pass userinfo", Desc: "redis credentials (user optional — ACL user, else default); passwords with URL-special characters must be percent-encoded ('/' → %2F, '@' → %40)", Cred: true},
		{Name: "rediss://", Desc: "TLS scheme"},
		{Name: "/N path", Desc: "logical database number (default 0)"},
		{Name: "skip_verify", Desc: "skip_verify=true disables TLS certificate verification (rediss only)"},
	}
}

func (c *redisConnector) Example() string {
	return "rediss://:$REDIS_PASSWORD@cache.internal:6379/0"
}

func (c *redisConnector) Capture(ctx context.Context, url string) (*schema.Batch, error) {
	root := redisRootID(url)
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redis %s: parse: %w", root, err)
	}
	opts.MaxRetries = -1 // one source, one dial, one capture — no retry ladders
	client := redis.NewClient(opts)
	defer client.Close()

	version := ""
	if info, err := client.Info(ctx, "server").Result(); err == nil {
		for _, line := range strings.Split(info, "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "redis_version:"); ok {
				version = v
				break
			}
		}
	} // version is a nicety; INFO may be disabled — not fatal

	dbsize, err := client.DBSize(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("redis %s: dbsize: %w", root, err)
	}

	// Bounded SCAN sample: prefix → count, first-seen example key.
	prefixCount := map[string]int64{}
	prefixExample := map[string]string{}
	sampled := 0
	capped := false
	var cursor uint64
scan:
	for {
		keys, next, err := client.Scan(ctx, cursor, "", redisScanCount).Result()
		if err != nil {
			return nil, fmt.Errorf("redis %s: scan: %w", root, err)
		}
		for _, key := range keys {
			if sampled >= redisScanCap {
				capped = true
				break scan
			}
			sampled++
			prefix := key
			if i := strings.Index(key, ":"); i >= 0 {
				prefix = key[:i]
			}
			prefixCount[prefix]++
			if _, seen := prefixExample[prefix]; !seen {
				prefixExample[prefix] = key
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	if cursor != 0 {
		capped = true
	}

	dbID := fmt.Sprintf("%s/db%d", root, opts.DB)
	dbMeta := map[string]string{
		"keys":         strconv.FormatInt(dbsize, 10),
		"sampled_keys": strconv.Itoa(sampled),
	}
	if version != "" {
		dbMeta["redis_version"] = version
	}
	if capped {
		dbMeta["sample_capped"] = fmt.Sprintf("sample capped at %d of %d keys — prefix counts are approximate", redisScanCap, dbsize)
	}
	b := &schema.Batch{Nodes: []schema.Node{{
		ID: dbID, Label: fmt.Sprintf("db%d", opts.DB), Kind: "database",
		FileType: "schema", Source: dbID, Metadata: dbMeta,
	}}}

	prefixes := make([]string, 0, len(prefixCount))
	for p := range prefixCount {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	for _, p := range prefixes {
		keyType := ""
		if t, err := client.Type(ctx, prefixExample[p]).Result(); err == nil {
			keyType = t
		} else {
			return nil, fmt.Errorf("redis %s: type: %w", root, err)
		}
		pID := dbID + "/" + p
		b.Nodes = append(b.Nodes, schema.Node{
			ID: pID, Label: p + ":*", Kind: "key_prefix", FileType: "schema", Source: pID,
			Metadata: map[string]string{
				"approx_count": strconv.FormatInt(prefixCount[p], 10),
				"example_type": keyType,
			},
		})
		b.Edges = append(b.Edges, schema.Edge{
			Source: dbID, Target: pID, Relation: "contains", Confidence: schema.Extracted,
		})
	}
	return b, nil
}

// redisRootID derives "scheme://host" for ids and errors from the SANITIZED
// url — textual only, never net/url. The db number lives in the path and is
// re-attached as /dbN by Capture.
func redisRootID(raw string) string {
	s, ok := sources.Sanitize(raw)
	if !ok {
		return "redis://unparseable-host"
	}
	i := strings.Index(s, "://")
	if i < 0 {
		return "redis://unparseable-host"
	}
	rest := s[i+3:]
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		rest = rest[:end]
	}
	return s[:i+3] + rest
}
