package connectors

// nats connector (scheme nats:// → "nats"): JetStream streams as the logical
// shape — one node per stream with its subject list, consumer NAMES, and
// message count as facts; no per-message anything. $SYS-prefixed streams are
// skipped; KV/ObjectStore backing streams ARE logical and are kept with a
// kind fact (kv / objectstore). Plain core NATS with JetStream disabled
// yields the server info node only, with the disablement REPORTED as a fact.
// Node ids from the sanitized URL; the authority passes to the driver
// verbatim.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/sources"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// natsStreamMeta is one JetStream stream as the seam reports it (pre-filter).
type natsStreamMeta struct {
	Name      string
	Subjects  []string
	Messages  uint64
	Consumers []string // names only
}

// natsErrNoJetStream is the seam's sentinel for "server reachable, JetStream
// disabled" — Capture turns it into a reported fact, not a failure.
var natsErrNoJetStream = errors.New("jetstream disabled")

// natsServer is the narrow seam between Capture and the driver — hermetic
// tests fake it; dialNATS is the only real implementation.
type natsServer interface {
	Version() string
	Streams(ctx context.Context) ([]natsStreamMeta, error) // natsErrNoJetStream when disabled
	Close()
}

type natsConnector struct {
	dial func(ctx context.Context, url string) (natsServer, error)
}

func init() { sources.Register(&natsConnector{dial: dialNATS}) }

func (c *natsConnector) Scheme() string { return "nats" }

func (c *natsConnector) Params() []sources.Param {
	return []sources.Param{
		{Name: "user:pass userinfo", Desc: "NATS credentials — user:pass or a bare token (nats://$NATS_TOKEN@host); secrets with URL-special characters must be percent-encoded ('/' → %2F, '@' → %40)", Cred: true},
		{Name: "tls_ca", Desc: "path to a CA certificate file (paths are not secrets)"},
	}
}

func (c *natsConnector) Example() string {
	return "nats://$NATS_USER:$NATS_PASS@nats.internal:4222"
}

func (c *natsConnector) Capture(ctx context.Context, url string) (*schema.Batch, error) {
	root := natsRootID(url)
	srv, err := c.dial(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("nats %s: connect: %w", root, err)
	}
	defer srv.Close()

	serverMeta := map[string]string{"jetstream": "enabled"}
	if v := srv.Version(); v != "" {
		serverMeta["server_version"] = v
	}

	streams, err := srv.Streams(ctx)
	switch {
	case errors.Is(err, natsErrNoJetStream):
		serverMeta["jetstream"] = "disabled — server info only" // reported, never silent
		streams = nil
	case err != nil:
		return nil, fmt.Errorf("nats %s: list streams: %w", root, err)
	}

	kept := make([]natsStreamMeta, 0, len(streams))
	for _, s := range streams {
		if strings.HasPrefix(s.Name, "$SYS") {
			continue
		}
		kept = append(kept, s)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Name < kept[j].Name })
	serverMeta["streams"] = strconv.Itoa(len(kept))

	b := &schema.Batch{Nodes: []schema.Node{{
		ID: root, Label: root, Kind: "server", FileType: "schema", Source: root,
		Metadata: serverMeta,
	}}}
	for _, s := range kept {
		subjects := append([]string(nil), s.Subjects...)
		sort.Strings(subjects)
		consumers := append([]string(nil), s.Consumers...)
		sort.Strings(consumers)
		id := root + "/" + s.Name
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: s.Name, Kind: "stream", FileType: "schema", Source: id,
			Metadata: map[string]string{
				"kind":      natsStreamKind(s.Name),
				"subjects":  strings.Join(subjects, ", "),
				"messages":  strconv.FormatUint(s.Messages, 10),
				"consumers": strings.Join(consumers, ", "),
			},
		})
		b.Edges = append(b.Edges, schema.Edge{
			Source: root, Target: id, Relation: "contains", Confidence: schema.Extracted,
		})
	}
	return b, nil
}

// natsStreamKind labels the stream's logical role: KV_/OBJ_ backing streams
// are logical and kept — with the kind made explicit.
func natsStreamKind(name string) string {
	switch {
	case strings.HasPrefix(name, "KV_"):
		return "kv"
	case strings.HasPrefix(name, "OBJ_"):
		return "objectstore"
	default:
		return "stream"
	}
}

// natsRootID derives "nats://authority" for ids and errors from the
// SANITIZED url — textual only, never net/url.
func natsRootID(raw string) string {
	s, ok := sources.Sanitize(raw)
	if !ok {
		return "nats://unparseable-host"
	}
	i := strings.Index(s, "://")
	if i < 0 {
		return "nats://unparseable-host"
	}
	rest := s[i+3:]
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		rest = rest[:end]
	}
	return s[:i+3] + rest
}

// ---- real driver implementation ----

type natsRealServer struct{ nc *nats.Conn }

// dialNATS strips our custom query params (tls_ca) textually before handing
// the URL to nats.Connect — the authority (incl. userinfo and comma-separated
// servers) passes verbatim.
func dialNATS(ctx context.Context, url string) (natsServer, error) {
	dialURL := url
	tlsCA := ""
	if q := strings.Index(dialURL, "?"); q >= 0 {
		for _, kv := range strings.Split(dialURL[q+1:], "&") {
			if k, v, _ := strings.Cut(kv, "="); strings.EqualFold(k, "tls_ca") {
				tlsCA = v
			}
		}
		dialURL = dialURL[:q]
	}
	var opts []nats.Option
	if tlsCA != "" {
		opts = append(opts, nats.RootCAs(tlsCA))
	}
	nc, err := nats.Connect(dialURL, opts...)
	if err != nil {
		return nil, err
	}
	return &natsRealServer{nc: nc}, nil
}

func (s *natsRealServer) Version() string { return s.nc.ConnectedServerVersion() }

func (s *natsRealServer) Streams(ctx context.Context) ([]natsStreamMeta, error) {
	js, err := jetstream.New(s.nc)
	if err != nil {
		return nil, err
	}
	lister := js.ListStreams(ctx)
	var out []natsStreamMeta
	for info := range lister.Info() {
		out = append(out, natsStreamMeta{
			Name:     info.Config.Name,
			Subjects: info.Config.Subjects,
			Messages: info.State.Msgs,
		})
	}
	if err := lister.Err(); err != nil {
		if errors.Is(err, jetstream.ErrJetStreamNotEnabled) ||
			errors.Is(err, jetstream.ErrJetStreamNotEnabledForAccount) {
			return nil, natsErrNoJetStream
		}
		return nil, err
	}
	for i := range out {
		stream, err := js.Stream(ctx, out[i].Name)
		if err != nil {
			return nil, err
		}
		names := stream.ConsumerNames(ctx)
		for name := range names.Name() {
			out[i].Consumers = append(out[i].Consumers, name)
		}
		if err := names.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *natsRealServer) Close() { s.nc.Close() }
