package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/sources"
	"time"
)

const mongoFakePW = "sup3rS3cretMongoPW"

type fakeMongo struct {
	dbs    []string
	colls  map[string][]string
	fields map[string]map[string]map[string]bool // "db.coll" → field → types
}

func (f *fakeMongo) ListDatabaseNames(ctx context.Context) ([]string, error) {
	return append([]string(nil), f.dbs...), nil
}

func (f *fakeMongo) ListCollectionNames(ctx context.Context, db string) ([]string, error) {
	return append([]string(nil), f.colls[db]...), nil
}

func (f *fakeMongo) SampleFields(ctx context.Context, db, coll string, limit int) (map[string]map[string]bool, error) {
	if limit != mongoSampleDocs {
		return nil, fmt.Errorf("expected sample cap %d, got %d", mongoSampleDocs, limit)
	}
	return f.fields[db+"."+coll], nil
}

func (f *fakeMongo) Disconnect(ctx context.Context) error { return nil }

func newFakeMongo() *fakeMongo {
	return &fakeMongo{
		// Deliberately unsorted, with system dbs mixed in.
		dbs: []string{"local", "orders", "admin", "app", "config"},
		colls: map[string][]string{
			"app":    {"users", "system.views", "accounts"},
			"orders": {"invoices"},
		},
		fields: map[string]map[string]map[string]bool{
			"app.users": {
				"name": {"string": true},
				"_id":  {"objectID": true},
				"age":  {"int32": true, "string": true},
			},
			"app.accounts":    {"_id": {"objectID": true}},
			"orders.invoices": {"total": {"double": true}},
		},
	}
}

func mongoTestConnector(f *fakeMongo) *mongoConnector {
	return &mongoConnector{dial: func(ctx context.Context, url string) (mongoLister, error) {
		return f, nil
	}}
}

func TestMongoCaptureHermetic(t *testing.T) {
	c := mongoTestConnector(newFakeMongo())
	url := "mongodb://root:" + mongoFakePW + "@h1.internal:27017,h2.internal:27017/app?authSource=admin"

	start := time.Now()
	b, err := c.Capture(context.Background(), url)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("hermetic capture took %v, want < 50ms", d)
	}

	// System dbs and system.* collections skipped; multi-host id preserved.
	var ids []string
	for _, n := range b.Nodes {
		ids = append(ids, n.ID)
	}
	want := []string{
		"mongodb://h1.internal:27017,h2.internal:27017/app",
		"mongodb://h1.internal:27017,h2.internal:27017/app/accounts",
		"mongodb://h1.internal:27017,h2.internal:27017/app/users",
		"mongodb://h1.internal:27017,h2.internal:27017/orders",
		"mongodb://h1.internal:27017,h2.internal:27017/orders/invoices",
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("node ids = %v, want %v", ids, want)
	}

	// Field facts: sorted union of names with sorted types; cap reported.
	for _, n := range b.Nodes {
		if strings.HasSuffix(n.ID, "/app/users") {
			if got := n.Metadata["fields"]; got != "_id (objectID), age (int32|string), name (string)" {
				t.Fatalf("users fields = %q", got)
			}
			if via := n.Metadata["fields_via"]; !strings.Contains(via, "100 docs") {
				t.Fatalf("sample cap not reported: %q", via)
			}
		}
	}

	// Sanitized ids and output: fake password appears nowhere.
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), mongoFakePW) {
		t.Fatalf("fake password leaked into batch: %s", raw)
	}
	if strings.Contains(string(raw), "@") {
		t.Fatalf("userinfo remnant in batch: %s", raw)
	}

	// Determinism: a second capture is byte-identical.
	b2, err := c.Capture(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	raw2, _ := json.Marshal(b2)
	if string(raw) != string(raw2) {
		t.Fatal("captures differ between runs")
	}
	if err := func() error { b.Producer = "test"; return b.Validate() }(); err != nil {
		t.Fatalf("batch invalid: %v", err)
	}
}

func TestMongoFieldCapReported(t *testing.T) {
	f := newFakeMongo()
	many := map[string]map[string]bool{}
	for i := 0; i < mongoMaxFieldsReported+6; i++ {
		many[fmt.Sprintf("field%03d", i)] = map[string]bool{"string": true}
	}
	f.fields["app.users"] = many
	b, err := mongoTestConnector(f).Capture(context.Background(), "mongodb://h:27017")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range b.Nodes {
		if strings.HasSuffix(n.ID, "/app/users") {
			got := n.Metadata["fields"]
			if !strings.Contains(got, "+6 more fields") || !strings.Contains(got, fmt.Sprintf("capped at %d", mongoMaxFieldsReported)) {
				t.Fatalf("field cap not reported: %q", got)
			}
			return
		}
	}
	t.Fatal("users collection node missing")
}

func TestMongoErrorNamesHostOnly(t *testing.T) {
	c := &mongoConnector{dial: func(ctx context.Context, url string) (mongoLister, error) {
		return nil, fmt.Errorf("dial tcp: connection refused")
	}}
	_, err := c.Capture(context.Background(), "mongodb://u:"+mongoFakePW+"@db.internal:27017/x")
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), mongoFakePW) || strings.Contains(err.Error(), "@") {
		t.Fatalf("error leaks credentials: %v", err)
	}
	if !strings.Contains(err.Error(), "mongodb://db.internal:27017") {
		t.Fatalf("error should name scheme+host: %v", err)
	}
}

func TestMongoParams(t *testing.T) {
	c := &mongoConnector{}
	if !strings.Contains(c.Example(), "$MONGO_") {
		t.Fatalf("example should use $VAR placeholders: %q", c.Example())
	}
	credSeen, pctHint := false, false
	for _, p := range c.Params() {
		if p.Cred {
			credSeen = true
			if strings.Contains(p.Desc, "%2F") {
				pctHint = true
			}
		}
	}
	if !credSeen {
		t.Fatal("no credential-class param declared")
	}
	if !pctHint {
		t.Fatal("credential param should carry the percent-encoding hint")
	}
}

func TestMongoSmokeReal(t *testing.T) {
	url := os.Getenv("CTX_OPTIMIZE_TEST_MONGO")
	if url == "" {
		t.Skip("CTX_OPTIMIZE_TEST_MONGO not set")
	}
	c, err := sources.Lookup("mongo")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	b, err := c.Capture(ctx, url)
	if err != nil {
		t.Fatalf("real capture: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("real capture took %v, want < 2s", d)
	}
	if len(b.Nodes) == 0 {
		t.Fatal("real capture returned zero nodes")
	}
}
