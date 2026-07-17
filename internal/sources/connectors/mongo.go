package connectors

// mongo connector (schemes mongodb://, mongodb+srv:// → "mongo"): captures the
// LOGICAL shape — databases → collections, with field facts from a CAPPED
// sample (~100 docs) — never a document walk. System databases (admin, local,
// config) and system.* collections are skipped. Multi-host URIs pass to the
// driver VERBATIM (never net/url on the authority); node ids come from the
// sanitized URL only. Bounded work, deterministic sorted output, caps reported
// as facts.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/sources"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// mongoSampleDocs caps how many documents field names are sampled from — the
// logical-shape rule: field facts come from a bounded sample, never a scan.
const mongoSampleDocs = 100

// mongoMaxFieldsReported caps how many field facts land on one collection
// node; the overflow is REPORTED as "+N more", never silently dropped.
const mongoMaxFieldsReported = 64

// mongoSystemDBs are never captured.
var mongoSystemDBs = map[string]bool{"admin": true, "local": true, "config": true}

// mongoLister is the narrow seam between Capture and the driver — hermetic
// tests fake it; dialMongo is the only real implementation.
type mongoLister interface {
	ListDatabaseNames(ctx context.Context) ([]string, error)
	ListCollectionNames(ctx context.Context, db string) ([]string, error)
	// SampleFields returns top-level field name → set of bson type names,
	// from a sample of at most limit documents.
	SampleFields(ctx context.Context, db, coll string, limit int) (map[string]map[string]bool, error)
	Disconnect(ctx context.Context) error
}

type mongoConnector struct {
	dial func(ctx context.Context, url string) (mongoLister, error)
}

func init() { sources.Register(&mongoConnector{dial: dialMongo}) }

func (c *mongoConnector) Scheme() string { return "mongo" }

func (c *mongoConnector) Params() []sources.Param {
	return []sources.Param{
		{Name: "user:pass userinfo", Desc: "MongoDB credentials; passwords with URL-special characters must be percent-encoded ('/' → %2F, '@' → %40)", Cred: true},
		{Name: "mongodb+srv://", Desc: "DNS-seedlist scheme; multi-host authorities (host1,host2) pass to the driver verbatim"},
		{Name: "authSource", Desc: "database to authenticate against (often admin)"},
		{Name: "replicaSet", Desc: "replica set name"},
		{Name: "tls", Desc: "tls=true enables TLS"},
		{Name: "tlsCAFile", Desc: "path to a CA certificate file (paths are not secrets)"},
		{Name: "tlsCertificateKeyFile", Desc: "path to a client cert+key file; stripped from stored ids"},
	}
}

func (c *mongoConnector) Example() string {
	return "mongodb://$MONGO_USER:$MONGO_PASS@host1:27017,host2:27017/app?authSource=admin"
}

func (c *mongoConnector) Capture(ctx context.Context, url string) (*schema.Batch, error) {
	root := mongoRootID(url)
	sess, err := c.dial(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("mongo %s: connect: %w", root, err)
	}
	defer sess.Disconnect(context.WithoutCancel(ctx))

	dbs, err := sess.ListDatabaseNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("mongo %s: list databases: %w", root, err)
	}
	sort.Strings(dbs)

	b := &schema.Batch{}
	for _, db := range dbs {
		if mongoSystemDBs[db] {
			continue
		}
		colls, err := sess.ListCollectionNames(ctx, db)
		if err != nil {
			return nil, fmt.Errorf("mongo %s: list collections in %s: %w", root, db, err)
		}
		sort.Strings(colls)
		var kept []string
		for _, coll := range colls {
			if strings.HasPrefix(coll, "system.") {
				continue
			}
			kept = append(kept, coll)
		}
		dbID := root + "/" + db
		b.Nodes = append(b.Nodes, schema.Node{
			ID: dbID, Label: db, Kind: "database", FileType: "schema", Source: dbID,
			Metadata: map[string]string{"collections": fmt.Sprintf("%d", len(kept))},
		})
		for _, coll := range kept {
			fields, err := sess.SampleFields(ctx, db, coll, mongoSampleDocs)
			if err != nil {
				return nil, fmt.Errorf("mongo %s: sample %s.%s: %w", root, db, coll, err)
			}
			collID := dbID + "/" + coll
			meta := map[string]string{
				"fields":     mongoFormatFields(fields),
				"fields_via": fmt.Sprintf("sample of up to %d docs", mongoSampleDocs),
			}
			b.Nodes = append(b.Nodes, schema.Node{
				ID: collID, Label: db + "." + coll, Kind: "collection",
				FileType: "schema", Source: collID, Metadata: meta,
			})
			b.Edges = append(b.Edges, schema.Edge{
				Source: dbID, Target: collID, Relation: "contains", Confidence: schema.Extracted,
			})
		}
	}
	return b, nil
}

// mongoFormatFields renders field facts sorted and CAPPED: "a (string), b
// (int32|string)"; overflow beyond mongoMaxFieldsReported is reported, never
// silent.
func mongoFormatFields(fields map[string]map[string]bool) string {
	names := make([]string, 0, len(fields))
	for f := range fields {
		names = append(names, f)
	}
	sort.Strings(names)
	shown := names
	extra := 0
	if len(shown) > mongoMaxFieldsReported {
		extra = len(shown) - mongoMaxFieldsReported
		shown = shown[:mongoMaxFieldsReported]
	}
	parts := make([]string, 0, len(shown))
	for _, f := range shown {
		types := make([]string, 0, len(fields[f]))
		for t := range fields[f] {
			types = append(types, t)
		}
		sort.Strings(types)
		parts = append(parts, fmt.Sprintf("%s (%s)", f, strings.Join(types, "|")))
	}
	out := strings.Join(parts, ", ")
	if extra > 0 {
		out += fmt.Sprintf(" (+%d more fields, capped at %d)", extra, mongoMaxFieldsReported)
	}
	return out
}

// mongoRootID derives "scheme://authority" for ids and errors from the
// SANITIZED url — textual only, never net/url (multi-host authorities and raw
// secrets both break it); the authority keeps its comma-separated hosts
// verbatim. Fail-closed: an unsanitizable value yields a host-free constant.
func mongoRootID(raw string) string {
	s, ok := sources.Sanitize(raw)
	if !ok {
		return "mongodb://unparseable-host"
	}
	i := strings.Index(s, "://")
	if i < 0 {
		return "mongodb://unparseable-host"
	}
	rest := s[i+3:]
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		rest = rest[:end]
	}
	return s[:i+3] + rest
}

// ---- real driver implementation ----

type mongoRealSession struct{ client *mongo.Client }

func dialMongo(ctx context.Context, url string) (mongoLister, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(url))
	if err != nil {
		return nil, err
	}
	return &mongoRealSession{client: client}, nil
}

func (s *mongoRealSession) ListDatabaseNames(ctx context.Context) ([]string, error) {
	return s.client.ListDatabaseNames(ctx, bson.D{})
}

func (s *mongoRealSession) ListCollectionNames(ctx context.Context, db string) ([]string, error) {
	return s.client.Database(db).ListCollectionNames(ctx, bson.D{})
}

func (s *mongoRealSession) SampleFields(ctx context.Context, db, coll string, limit int) (map[string]map[string]bool, error) {
	c := s.client.Database(db).Collection(coll)
	cur, err := c.Aggregate(ctx, mongo.Pipeline{
		bson.D{{Key: "$sample", Value: bson.D{{Key: "size", Value: limit}}}},
	})
	if err != nil {
		// $sample can be unavailable (old servers, views): bounded Find instead.
		cur, err = c.Find(ctx, bson.D{}, options.Find().SetLimit(int64(limit)))
		if err != nil {
			return nil, err
		}
	}
	defer cur.Close(ctx)
	fields := map[string]map[string]bool{}
	for cur.Next(ctx) {
		els, err := cur.Current.Elements()
		if err != nil {
			continue // one malformed doc must not sink the capture
		}
		for _, el := range els {
			key, err := el.KeyErr()
			if err != nil {
				continue
			}
			if fields[key] == nil {
				fields[key] = map[string]bool{}
			}
			fields[key][el.Value().Type.String()] = true
		}
	}
	return fields, cur.Err()
}

func (s *mongoRealSession) Disconnect(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}
