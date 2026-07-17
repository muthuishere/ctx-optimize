package connectors

// kafka connector (scheme kafka:// → "kafka"): topics with partition and
// replication COUNTS as facts (partitions are never nodes) plus consumer
// groups by name only — metadata round trips, bounded by construction.
// Internal topics (__consumer_offsets, __transaction_state, _schemas, any
// leading underscore, broker-flagged internals) are skipped. Comma-separated
// brokers pass to the driver VERBATIM as seeds (never net/url on the
// authority); node ids come from the sanitized URL.
//
// kadm (github.com/twmb/franz-go/pkg/kadm) is a SEPARATE module not in
// go.sum, so metadata rides kgo's raw request API with kmsg (part of the
// pinned franz-go dependency set) instead.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/sources"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/sasl/plain"
)

// kafkaInternalTopics are always skipped, along with any leading-underscore
// topic and anything the broker flags internal.
var kafkaInternalTopics = map[string]bool{
	"__consumer_offsets":  true,
	"__transaction_state": true,
	"_schemas":            true,
}

func kafkaIsInternalTopic(name string, brokerFlagged bool) bool {
	return brokerFlagged || kafkaInternalTopics[name] || strings.HasPrefix(name, "_")
}

// kafkaTopicMeta is one topic as the seam reports it (pre-filter).
type kafkaTopicMeta struct {
	Name        string
	Partitions  int
	Replication int
	Internal    bool // broker-flagged
}

// kafkaClusterMeta is everything one capture needs — a single seam call.
type kafkaClusterMeta struct {
	Brokers int
	Topics  []kafkaTopicMeta
	Groups  []string
}

// kafkaAdmin is the narrow seam between Capture and the driver — hermetic
// tests fake it; dialKafka is the only real implementation.
type kafkaAdmin interface {
	Meta(ctx context.Context) (kafkaClusterMeta, error)
	Close()
}

// kafkaTarget is the textually-parsed URL (no net/url — multi-host
// authorities and raw secrets both break it).
type kafkaTarget struct {
	Brokers []string // passed verbatim as seeds
	User    string
	Pass    string
	SASL    string // "plain" or ""
	TLS     bool
	TLSCA   string // CA file path
}

type kafkaConnector struct {
	dial func(ctx context.Context, t kafkaTarget) (kafkaAdmin, error)
}

func init() { sources.Register(&kafkaConnector{dial: dialKafka}) }

func (c *kafkaConnector) Scheme() string { return "kafka" }

func (c *kafkaConnector) Params() []sources.Param {
	return []sources.Param{
		{Name: "user:pass userinfo", Desc: "SASL credentials (with ?sasl=plain); passwords with URL-special characters must be percent-encoded ('/' → %2F, '@' → %40)", Cred: true},
		{Name: "host1:9092,host2:9092", Desc: "comma-separated brokers, passed verbatim as seeds"},
		{Name: "sasl", Desc: "sasl=plain enables SASL/PLAIN from the userinfo"},
		{Name: "tls", Desc: "tls=true enables TLS"},
		{Name: "tls_ca", Desc: "path to a CA certificate file (paths are not secrets)"},
	}
}

func (c *kafkaConnector) Example() string {
	return "kafka://$KAFKA_USER:$KAFKA_PASS@broker1:9092,broker2:9092?sasl=plain&tls=true"
}

func (c *kafkaConnector) Capture(ctx context.Context, url string) (*schema.Batch, error) {
	root := kafkaRootID(url)
	target, err := kafkaParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("kafka %s: %w", root, err)
	}
	admin, err := c.dial(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("kafka %s: connect: %w", root, err)
	}
	defer admin.Close()

	meta, err := admin.Meta(ctx)
	if err != nil {
		return nil, fmt.Errorf("kafka %s: metadata: %w", root, err)
	}

	topics := make([]kafkaTopicMeta, 0, len(meta.Topics))
	for _, t := range meta.Topics {
		if kafkaIsInternalTopic(t.Name, t.Internal) {
			continue
		}
		topics = append(topics, t)
	}
	sort.Slice(topics, func(i, j int) bool { return topics[i].Name < topics[j].Name })
	groups := append([]string(nil), meta.Groups...)
	sort.Strings(groups)

	b := &schema.Batch{Nodes: []schema.Node{{
		ID: root, Label: root, Kind: "cluster", FileType: "schema", Source: root,
		Metadata: map[string]string{
			"brokers":         strconv.Itoa(meta.Brokers),
			"topics":          strconv.Itoa(len(topics)),
			"consumer_groups": strconv.Itoa(len(groups)),
		},
	}}}
	for _, t := range topics {
		id := root + "/" + t.Name
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: t.Name, Kind: "topic", FileType: "schema", Source: id,
			Metadata: map[string]string{
				"partitions":  strconv.Itoa(t.Partitions),
				"replication": strconv.Itoa(t.Replication),
			},
		})
		b.Edges = append(b.Edges, schema.Edge{
			Source: root, Target: id, Relation: "contains", Confidence: schema.Extracted,
		})
	}
	for _, g := range groups {
		id := root + "/groups/" + g
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: g, Kind: "consumer_group", FileType: "schema", Source: id,
		})
		b.Edges = append(b.Edges, schema.Edge{
			Source: root, Target: id, Relation: "contains", Confidence: schema.Extracted,
		})
	}
	return b, nil
}

// kafkaParseURL splits kafka://[user:pass@]b1:9092,b2:9092[?params]
// textually. The broker list stays verbatim (split on commas only for the
// seed slice); secrets never touch net/url.
func kafkaParseURL(raw string) (kafkaTarget, error) {
	var t kafkaTarget
	rest, ok := strings.CutPrefix(raw, "kafka://")
	if !ok {
		return t, fmt.Errorf("not a kafka:// URL")
	}
	query := ""
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		rest, query = rest[:i], rest[i:]
	}
	if i := strings.Index(rest, "/"); i >= 0 {
		rest = rest[:i] // no path component in the kafka form
	}
	authority := rest
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		userinfo := authority[:at]
		authority = authority[at+1:]
		t.User, t.Pass, _ = strings.Cut(userinfo, ":")
	}
	if authority == "" {
		return t, fmt.Errorf("no brokers in URL")
	}
	t.Brokers = strings.Split(authority, ",")
	if strings.HasPrefix(query, "?") {
		for _, kv := range strings.Split(query[1:], "&") {
			k, v, _ := strings.Cut(kv, "=")
			switch strings.ToLower(k) {
			case "sasl":
				t.SASL = strings.ToLower(v)
			case "tls":
				t.TLS = strings.EqualFold(v, "true") || v == "1"
			case "tls_ca":
				t.TLSCA = v
				t.TLS = true
			}
		}
	}
	return t, nil
}

// kafkaRootID derives "kafka://authority" for ids and errors from the
// SANITIZED url — textual only, never net/url; the comma-separated broker
// list stays verbatim in the id.
func kafkaRootID(raw string) string {
	s, ok := sources.Sanitize(raw)
	if !ok {
		return "kafka://unparseable-host"
	}
	i := strings.Index(s, "://")
	if i < 0 {
		return "kafka://unparseable-host"
	}
	rest := s[i+3:]
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		rest = rest[:end]
	}
	return s[:i+3] + rest
}

// ---- real driver implementation (kgo + raw kmsg requests) ----

type kafkaRealAdmin struct{ client *kgo.Client }

func dialKafka(ctx context.Context, t kafkaTarget) (kafkaAdmin, error) {
	opts := []kgo.Opt{kgo.SeedBrokers(t.Brokers...)}
	if t.SASL == "plain" && t.User != "" {
		opts = append(opts, kgo.SASL(plain.Auth{User: t.User, Pass: t.Pass}.AsMechanism()))
	}
	if t.TLS {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if t.TLSCA != "" {
			pem, err := os.ReadFile(t.TLSCA)
			if err != nil {
				return nil, fmt.Errorf("read tls_ca: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("tls_ca: no certificates found in file")
			}
			cfg.RootCAs = pool
		}
		opts = append(opts, kgo.DialTLSConfig(cfg))
	}
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx); err != nil {
		client.Close()
		return nil, err
	}
	return &kafkaRealAdmin{client: client}, nil
}

func (a *kafkaRealAdmin) Meta(ctx context.Context) (kafkaClusterMeta, error) {
	var out kafkaClusterMeta

	// Metadata: all topics (nil topic list = everything).
	resp, err := a.client.Request(ctx, kmsg.NewPtrMetadataRequest())
	if err != nil {
		return out, err
	}
	md, ok := resp.(*kmsg.MetadataResponse)
	if !ok {
		return out, fmt.Errorf("unexpected metadata response type %T", resp)
	}
	out.Brokers = len(md.Brokers)
	for _, t := range md.Topics {
		if t.Topic == nil || t.ErrorCode != 0 {
			continue
		}
		tm := kafkaTopicMeta{Name: *t.Topic, Partitions: len(t.Partitions), Internal: t.IsInternal}
		if len(t.Partitions) > 0 {
			tm.Replication = len(t.Partitions[0].Replicas)
		}
		out.Topics = append(out.Topics, tm)
	}

	// Consumer groups live per coordinator — ask every broker and merge.
	shards := a.client.RequestSharded(ctx, kmsg.NewPtrListGroupsRequest())
	seen := map[string]bool{}
	var shardErr error
	okShards := 0
	for _, sh := range shards {
		if sh.Err != nil {
			if shardErr == nil {
				shardErr = sh.Err
			}
			continue
		}
		lg, ok := sh.Resp.(*kmsg.ListGroupsResponse)
		if !ok {
			continue
		}
		okShards++
		for _, g := range lg.Groups {
			if !seen[g.Group] {
				seen[g.Group] = true
				out.Groups = append(out.Groups, g.Group)
			}
		}
	}
	if okShards == 0 && shardErr != nil {
		return out, fmt.Errorf("list groups: %w", shardErr)
	}
	return out, nil
}

func (a *kafkaRealAdmin) Close() { a.client.Close() }
