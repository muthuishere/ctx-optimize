package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/sources"
	"time"
)

func pgFakeCatalog() *pgCatalog {
	return &pgCatalog{
		Tables: []pgTable{
			{Schema: "public", Name: "users", Kind: "r"},
			{Schema: "public", Name: "orders", Kind: "r"},
			{Schema: "billing", Name: "invoices", Kind: "r"},
			{Schema: "public", Name: "order_totals", Kind: "v"},
		},
		Partitions: map[string]int{},
		Columns: []pgColumn{
			{Schema: "public", Table: "users", Name: "id", Type: "bigint", Nullable: false, Default: "nextval('users_id_seq')"},
			{Schema: "public", Table: "users", Name: "email", Type: "text", Nullable: false},
			{Schema: "public", Table: "orders", Name: "id", Type: "bigint", Nullable: false},
			{Schema: "public", Table: "orders", Name: "user_id", Type: "bigint", Nullable: true},
			{Schema: "billing", Table: "invoices", Name: "id", Type: "bigint", Nullable: false},
			{Schema: "billing", Table: "invoices", Name: "order_id", Type: "bigint", Nullable: false},
		},
		PKs: []pgPK{
			{Schema: "public", Table: "users", Column: "id"},
			{Schema: "public", Table: "orders", Column: "id"},
		},
		FKs: []pgFK{
			{Name: "orders_user_id_fkey", Schema: "public", Table: "orders", Column: "user_id",
				RefSchema: "public", RefTable: "users", RefColumn: "id"},
			{Name: "invoices_order_id_fkey", Schema: "billing", Table: "invoices", Column: "order_id",
				RefSchema: "public", RefTable: "orders", RefColumn: "id"},
		},
		Indexes: []pgIndex{
			{Schema: "public", Table: "users", Name: "users_pkey"},
			{Schema: "public", Table: "users", Name: "users_email_idx"},
		},
		Views: []pgView{
			{Schema: "public", Name: "order_totals", Definition: "SELECT user_id, sum(total) FROM orders GROUP BY 1"},
		},
		Comments: []pgComment{
			{Schema: "public", Table: "users", Column: "", Text: "registered accounts"},
			{Schema: "public", Table: "users", Column: "email", Text: "login identity"},
		},
	}
}

func TestPostgresBuildBatchNormal(t *testing.T) {
	b := buildPGBatch("db.example.com:5432", "appdb", nil, pgFakeCatalog())
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	base := "postgres://db.example.com:5432/appdb"
	nodes := map[string]int{}
	for i, n := range b.Nodes {
		nodes[n.ID] = i
	}
	for _, want := range []string{
		base,
		base + "/public", base + "/billing",
		base + "/public/users", base + "/public/orders", base + "/public/order_totals",
		base + "/billing/invoices",
		base + "/public/users/id", base + "/public/users/email",
	} {
		if _, ok := nodes[want]; !ok {
			t.Errorf("missing node %s", want)
		}
	}
	users := b.Nodes[nodes[base+"/public/users"]]
	if users.Label != "public.users" || users.Kind != "table" || users.FileType != "schema" {
		t.Errorf("users node = %q/%s/%s", users.Label, users.Kind, users.FileType)
	}
	if users.Metadata["comment"] != "registered accounts" {
		t.Errorf("users comment = %q", users.Metadata["comment"])
	}
	if users.Metadata["index_count"] != "2" || !strings.Contains(users.Metadata["indexes"], "users_email_idx") {
		t.Errorf("users indexes = %q/%q", users.Metadata["index_count"], users.Metadata["indexes"])
	}
	id := b.Nodes[nodes[base+"/public/users/id"]]
	if id.Label != "users.id" || id.Kind != "column" || id.Metadata["primary_key"] != "true" ||
		id.Metadata["type"] != "bigint" || id.Metadata["nullable"] != "false" ||
		id.Metadata["default"] == "" {
		t.Errorf("users.id column = %+v", id)
	}
	email := b.Nodes[nodes[base+"/public/users/email"]]
	if email.Metadata["comment"] != "login identity" || email.Metadata["primary_key"] != "" {
		t.Errorf("users.email column = %+v", email)
	}
	view := b.Nodes[nodes[base+"/public/order_totals"]]
	if view.Kind != "view" || !strings.Contains(view.Metadata["definition"], "sum(total)") {
		t.Errorf("view node = %+v", view)
	}
	// contains chain: db → schema → table → column
	hasEdge := func(src, dst, rel string) bool {
		for _, e := range b.Edges {
			if e.Source == src && e.Target == dst && e.Relation == rel {
				return true
			}
		}
		return false
	}
	if !hasEdge(base, base+"/public", "contains") ||
		!hasEdge(base+"/public", base+"/public/users", "contains") ||
		!hasEdge(base+"/public/users", base+"/public/users/email", "contains") {
		t.Errorf("contains chain broken")
	}
}

func TestPostgresPartitionTrap(t *testing.T) {
	cat := &pgCatalog{
		Tables: []pgTable{
			{Schema: "public", Name: "events", Kind: "p"},
		},
		Partitions: map[string]int{"public.events": 100},
		Columns: []pgColumn{
			{Schema: "public", Table: "events", Name: "id", Type: "bigint"},
		},
	}
	// simulate a careless query tier leaking children — the Go tier must drop them
	for i := 0; i < 100; i++ {
		cat.Tables = append(cat.Tables, pgTable{
			Schema: "public", Name: fmt.Sprintf("events_p%03d", i), Kind: "r", IsPartition: true,
		})
		cat.Columns = append(cat.Columns, pgColumn{
			Schema: "public", Table: fmt.Sprintf("events_p%03d", i), Name: "id", Type: "bigint",
		})
	}
	b := buildPGBatch("h", "db", nil, cat)
	tables := 0
	for _, n := range b.Nodes {
		if n.Kind == "table" {
			tables++
			if n.Metadata["partitions"] != "100" {
				t.Errorf("parent partitions fact = %q, want 100", n.Metadata["partitions"])
			}
		}
		if strings.Contains(n.ID, "events_p") {
			t.Errorf("partition child leaked as node: %s", n.ID)
		}
	}
	if tables != 1 {
		t.Errorf("tables = %d, want 1 (parent only)", tables)
	}
	cols := 0
	for _, n := range b.Nodes {
		if n.Kind == "column" {
			cols++
		}
	}
	if cols != 1 {
		t.Errorf("columns = %d, want 1 (children's columns dropped)", cols)
	}
}

func TestPostgresTimescaleAndSystemSchemasExcluded(t *testing.T) {
	cat := &pgCatalog{
		Tables: []pgTable{
			{Schema: "public", Name: "metrics", Kind: "r"},
			{Schema: "_timescaledb_internal", Name: "_hyper_1_1_chunk", Kind: "r"},
			{Schema: "_timescaledb_catalog", Name: "hypertable", Kind: "r"},
			{Schema: "pg_toast", Name: "pg_toast_1234", Kind: "r"},
			{Schema: "information_schema", Name: "tables", Kind: "v"},
		},
		Partitions: map[string]int{},
		Columns: []pgColumn{
			{Schema: "_timescaledb_internal", Table: "_hyper_1_1_chunk", Name: "ts", Type: "timestamptz"},
		},
	}
	b := buildPGBatch("h", "db", nil, cat)
	for _, n := range b.Nodes {
		for _, bad := range []string{"_timescaledb", "pg_toast", "information_schema"} {
			if strings.Contains(n.ID, bad) {
				t.Errorf("excluded schema leaked: %s", n.ID)
			}
		}
	}
	tables := 0
	for _, n := range b.Nodes {
		if n.Kind == "table" {
			tables++
		}
	}
	if tables != 1 {
		t.Errorf("tables = %d, want 1 (public.metrics only)", tables)
	}
}

func TestPostgresSchemasIncludeFilter(t *testing.T) {
	b := buildPGBatch("h", "db", []string{"billing"}, pgFakeCatalog())
	for _, n := range b.Nodes {
		if strings.Contains(n.ID, "/public/") || strings.HasSuffix(n.ID, "/public") {
			t.Errorf("filtered-out schema leaked: %s", n.ID)
		}
	}
	found := false
	for _, n := range b.Nodes {
		if n.Label == "billing.invoices" {
			found = true
		}
	}
	if !found {
		t.Errorf("included schema table missing")
	}
}

func TestPostgresFKEdges(t *testing.T) {
	b := buildPGBatch("h", "db", nil, pgFakeCatalog())
	base := "postgres://h/db"
	var fk []string
	for _, e := range b.Edges {
		if e.Relation == "references" {
			if e.Confidence != "EXTRACTED" {
				t.Errorf("fk confidence = %s", e.Confidence)
			}
			fk = append(fk, e.Source+" -> "+e.Target+" ["+e.Metadata["constraints"]+"]")
		}
	}
	if len(fk) != 2 {
		t.Fatalf("fk edges = %d, want 2: %v", len(fk), fk)
	}
	want := base + "/public/orders -> " + base + "/public/users [orders_user_id_fkey(user_id→id)]"
	if fk[1] != want && fk[0] != want {
		t.Errorf("fk edges = %v, want one = %q", fk, want)
	}
}

func TestPostgresSortedDeterminism(t *testing.T) {
	cat1 := pgFakeCatalog()
	cat2 := pgFakeCatalog()
	// reverse every input slice — output must not care
	for i, j := 0, len(cat2.Tables)-1; i < j; i, j = i+1, j-1 {
		cat2.Tables[i], cat2.Tables[j] = cat2.Tables[j], cat2.Tables[i]
	}
	for i, j := 0, len(cat2.Columns)-1; i < j; i, j = i+1, j-1 {
		cat2.Columns[i], cat2.Columns[j] = cat2.Columns[j], cat2.Columns[i]
	}
	for i, j := 0, len(cat2.FKs)-1; i < j; i, j = i+1, j-1 {
		cat2.FKs[i], cat2.FKs[j] = cat2.FKs[j], cat2.FKs[i]
	}
	b1, _ := json.Marshal(buildPGBatch("h", "db", nil, cat1))
	b2, _ := json.Marshal(buildPGBatch("h", "db", nil, cat2))
	if string(b1) != string(b2) {
		t.Errorf("output depends on input row order")
	}
	// and ids come out sorted
	b := buildPGBatch("h", "db", nil, cat1)
	for i := 1; i < len(b.Nodes); i++ {
		if b.Nodes[i-1].ID >= b.Nodes[i].ID {
			t.Errorf("nodes not sorted: %s >= %s", b.Nodes[i-1].ID, b.Nodes[i].ID)
		}
	}
}

func TestPostgresSanitizedIDs(t *testing.T) {
	const pw = "sup3rS3cretPW"
	raw := "postgres://app:" + pw + "@db.example.com:5432/appdb?sslmode=require"
	host, err := pgHost(raw)
	if err != nil {
		t.Fatalf("pgHost: %v", err)
	}
	if host != "db.example.com:5432" {
		t.Errorf("host = %q", host)
	}
	b := buildPGBatch(host, "appdb", nil, pgFakeCatalog())
	out, _ := json.Marshal(b)
	if strings.Contains(string(out), pw) {
		t.Fatalf("password leaked into batch output")
	}
	if strings.Contains(string(out), "app:") || strings.Contains(string(out), "app@") {
		t.Fatalf("userinfo leaked into batch output")
	}
	for _, n := range b.Nodes {
		if !strings.HasPrefix(n.ID, "postgres://db.example.com:5432/appdb") {
			t.Errorf("id not rooted at sanitized base: %s", n.ID)
		}
	}
}

func TestPostgresErrorWrapNeverLeaks(t *testing.T) {
	const pw = "sup3rS3cretPW"
	raw := "postgres://app:" + pw + "@db.example.com:5432/appdb"
	// a worst-case driver error echoing the full URL
	drv := fmt.Errorf("cannot parse %q: something broke for user app:%s", raw, pw)
	err := pgWrap("db.example.com:5432", "connect", drv, pgSecrets(raw))
	if strings.Contains(err.Error(), pw) || strings.Contains(err.Error(), raw) {
		t.Fatalf("wrapped error leaks secrets: %v", strings.ReplaceAll(err.Error(), pw, "<PW>"))
	}
	if !strings.Contains(err.Error(), "postgres db.example.com:5432: connect") {
		t.Errorf("wrap shape = %v", err)
	}
}

func TestPostgresSchemasParamPopped(t *testing.T) {
	inc, conn := popPGSchemasParam("postgres://h/db?sslmode=require&schemas=b,a&sslrootcert=/ca.pem")
	if len(inc) != 2 || inc[0] != "a" || inc[1] != "b" {
		t.Errorf("include = %v", inc)
	}
	if conn != "postgres://h/db?sslmode=require&sslrootcert=/ca.pem" {
		t.Errorf("connURL = %q (schemas must be stripped, rest verbatim)", conn)
	}
	inc, conn = popPGSchemasParam("postgres://h/db?schemas=only")
	if len(inc) != 1 || conn != "postgres://h/db" {
		t.Errorf("lone schemas param: %v / %q", inc, conn)
	}
	inc, conn = popPGSchemasParam("postgres://h/db")
	if inc != nil || conn != "postgres://h/db" {
		t.Errorf("no query: %v / %q", inc, conn)
	}
}

func TestPostgresRegisteredSchemes(t *testing.T) {
	for _, scheme := range []string{"postgres", "postgresql"} {
		name, err := sources.Route(scheme + "://h/db")
		if err != nil {
			t.Fatalf("sources.Route(%s): %v", scheme, err)
		}
		c, err := sources.Lookup(name)
		if err != nil {
			t.Fatalf("sources.Lookup(%s): %v", name, err)
		}
		if len(c.Params()) == 0 || c.Example() == "" {
			t.Errorf("%s connector missing params/example", scheme)
		}
	}
	card, err := sources.HelpCard("postgres")
	if err != nil {
		t.Fatalf("HelpCard: %v", err)
	}
	if !strings.Contains(card, "schemas") || !strings.Contains(card, "sslrootcert") {
		t.Errorf("help card missing declared params:\n%s", card)
	}
}

func TestPostgresPerf100Tables(t *testing.T) {
	cat := &pgCatalog{Partitions: map[string]int{}}
	for ti := 0; ti < 100; ti++ {
		name := fmt.Sprintf("t%03d", ti)
		cat.Tables = append(cat.Tables, pgTable{Schema: "public", Name: name, Kind: "r"})
		for ci := 0; ci < 13; ci++ {
			cat.Columns = append(cat.Columns, pgColumn{
				Schema: "public", Table: name, Name: fmt.Sprintf("c%02d", ci), Type: "text", Nullable: true,
			})
		}
		cat.PKs = append(cat.PKs, pgPK{Schema: "public", Table: name, Column: "c00"})
		cat.Indexes = append(cat.Indexes, pgIndex{Schema: "public", Table: name, Name: name + "_pkey"})
		if ti > 0 {
			cat.FKs = append(cat.FKs, pgFK{
				Name: name + "_fkey", Schema: "public", Table: name, Column: "c01",
				RefSchema: "public", RefTable: "t000", RefColumn: "c00",
			})
		}
	}
	start := time.Now()
	b := buildPGBatch("h:5432", "big", nil, cat)
	elapsed := time.Since(start)
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := len(b.Nodes); got != 1+1+100+1300 {
		t.Errorf("nodes = %d", got)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("mapping took %v, gate is 50ms", elapsed)
	}
}

func TestPostgresRealSmoke(t *testing.T) {
	url := os.Getenv("CTX_OPTIMIZE_TEST_PG")
	if url == "" {
		t.Skip("CTX_OPTIMIZE_TEST_PG not set — skipping real postgres smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	b, err := pgConnector{}.Capture(ctx, url)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Capture: %v", err) // pgWrap already scrubbed
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(b.Nodes) == 0 {
		t.Errorf("no nodes captured")
	}
	if elapsed > 2*time.Second {
		t.Errorf("capture took %v, gate is 2s", elapsed)
	}
	t.Logf("real capture: %d nodes, %d edges in %v", len(b.Nodes), len(b.Edges), elapsed)
}
