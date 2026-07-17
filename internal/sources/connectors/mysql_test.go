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

	gomysql "github.com/go-sql-driver/mysql"
)

// ---- URL → driver-DSN conversion (its own table) ----

func TestMySQLURLToDSN(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		wantUser string
		wantPass string
		wantAddr string
		wantDB   string
		wantHost string // returned id base (credential-free)
	}{
		{"basic", "mysql://app:pw@db.internal:3306/billing",
			"app", "pw", "db.internal:3306", "billing", "db.internal:3306"},
		{"default port appended", "mysql://app:pw@db.internal/billing",
			"app", "pw", "db.internal:3306", "billing", "db.internal:3306"},
		{"percent-encoded password", "mysql://app:p%40ss%2Fw%3Ard@h:3307/d",
			"app", "p@ss/w:rd", "h:3307", "d", "h:3307"},
		{"no userinfo", "mysql://h:3306/d",
			"", "", "h:3306", "d", "h:3306"},
		{"no database", "mysql://u:p@h",
			"u", "p", "h:3306", "", "h:3306"},
		{"user only", "mysql://readonly@h/d",
			"readonly", "", "h:3306", "d", "h:3306"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dsn, hostport, dbname, err := mysqlURLToDSN(tc.url)
			if err != nil {
				t.Fatalf("mysqlURLToDSN(%q): %v", tc.url, err)
			}
			if hostport != tc.wantHost {
				t.Errorf("hostport = %q, want %q", hostport, tc.wantHost)
			}
			if dbname != tc.wantDB {
				t.Errorf("dbname = %q, want %q", dbname, tc.wantDB)
			}
			cfg, err := gomysql.ParseDSN(dsn)
			if err != nil {
				t.Fatalf("driver rejects our DSN: %v", err)
			}
			if cfg.User != tc.wantUser || cfg.Passwd != tc.wantPass {
				t.Errorf("user/pass = %q/%q, want %q/%q", cfg.User, cfg.Passwd, tc.wantUser, tc.wantPass)
			}
			if cfg.Addr != tc.wantAddr {
				t.Errorf("addr = %q, want %q", cfg.Addr, tc.wantAddr)
			}
			if cfg.DBName != tc.wantDB {
				t.Errorf("driver dbname = %q, want %q", cfg.DBName, tc.wantDB)
			}
		})
	}
}

func TestMySQLURLToDSNTLSParamPassthrough(t *testing.T) {
	dsn, _, _, err := mysqlURLToDSN("mysql://u:p@h/d?tls=skip-verify")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := gomysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("driver rejects our DSN: %v", err)
	}
	if cfg.TLSConfig != "skip-verify" {
		t.Errorf("TLSConfig = %q, want skip-verify", cfg.TLSConfig)
	}
}

func TestMySQLURLToDSNErrors(t *testing.T) {
	secret := "TOPSECRETPW"
	for _, u := range []string{
		"postgres://u:p@h/d",              // wrong scheme
		"mysql://",                        // missing host
		"mysql://u:" + secret + "%zz@h/d", // bad percent-encoding in password
	} {
		_, _, _, err := mysqlURLToDSN(u)
		if err == nil {
			t.Errorf("mysqlURLToDSN(%q): want error", u)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error for %q echoes the password: %s", u, err)
		}
	}
}

// ---- mapping seam ----

func mysqlFakeIntrospection() mysqlIntrospection {
	return mysqlIntrospection{
		Schemas: []string{"billing", "mysql", "sys", "performance_schema", "information_schema"},
		Tables: []mysqlTableRow{
			{Schema: "billing", Name: "invoices"},
			{Schema: "billing", Name: "customers"},
			{Schema: "billing", Name: "v_open_invoices", IsView: true},
			{Schema: "mysql", Name: "user"},
			{Schema: "performance_schema", Name: "events_waits_current"},
		},
		Columns: []mysqlColumnRow{
			{Schema: "billing", Table: "invoices", Name: "id", ColumnType: "bigint", Position: 1},
			{Schema: "billing", Table: "invoices", Name: "customer_id", ColumnType: "bigint", Position: 2},
			{Schema: "billing", Table: "invoices", Name: "note", ColumnType: "text", Nullable: true, Position: 3},
			{Schema: "billing", Table: "customers", Name: "id", ColumnType: "bigint", Position: 1},
			{Schema: "mysql", Table: "user", Name: "Host", ColumnType: "char(255)", Position: 1},
		},
		PrimaryKeys: []mysqlKeyRow{
			{Schema: "billing", Table: "invoices", Column: "id"},
			{Schema: "billing", Table: "customers", Column: "id"},
		},
		ForeignKeys: []mysqlFKRow{
			{Schema: "billing", Table: "invoices", Column: "customer_id",
				RefSchema: "billing", RefTable: "customers", RefColumn: "id", Constraint: "fk_inv_cust"},
		},
	}
}

func TestMySQLSystemSchemaExclusion(t *testing.T) {
	b := mysqlBuildBatch("h:3306", mysqlFakeIntrospection())
	for _, n := range b.Nodes {
		for _, sys := range []string{"/mysql", "/sys", "/performance_schema", "/information_schema"} {
			if strings.HasPrefix(n.ID, "mysql://h:3306"+sys+"/") || n.ID == "mysql://h:3306"+sys {
				t.Errorf("system-schema node leaked: %s", n.ID)
			}
		}
	}
	want := map[string]bool{
		"mysql://h:3306/billing":                 true,
		"mysql://h:3306/billing/invoices":        true,
		"mysql://h:3306/billing/customers":       true,
		"mysql://h:3306/billing/v_open_invoices": true,
	}
	got := map[string]bool{}
	for _, n := range b.Nodes {
		if n.Kind != "column" {
			got[n.ID] = true
		}
	}
	for id := range want {
		if !got[id] {
			t.Errorf("missing node %s", id)
		}
	}
	if len(got) != len(want) {
		t.Errorf("non-column nodes = %d, want %d: %v", len(got), len(want), got)
	}
}

func TestMySQLPartitionCollapse(t *testing.T) {
	in := mysqlFakeIntrospection()
	for i := 1; i <= 12; i++ {
		in.Partitions = append(in.Partitions, mysqlPartitionRow{
			Schema: "billing", Table: "invoices", Partition: fmt.Sprintf("p%02d", i),
		})
	}
	b := mysqlBuildBatch("h:3306", in)
	found := false
	for _, n := range b.Nodes {
		if strings.Contains(n.ID, "p01") || strings.Contains(n.Label, "p01") {
			t.Errorf("partition enumerated as a node: %s", n.ID)
		}
		if n.ID == "mysql://h:3306/billing/invoices" {
			found = true
			if n.Metadata["partitions"] != "12" {
				t.Errorf("partitions fact = %q, want 12", n.Metadata["partitions"])
			}
		}
	}
	if !found {
		t.Fatal("invoices table node missing")
	}
}

func TestMySQLFKEdges(t *testing.T) {
	in := mysqlFakeIntrospection()
	// Add a second column to the same constraint — must group to ONE edge.
	in.ForeignKeys = append(in.ForeignKeys, mysqlFKRow{
		Schema: "billing", Table: "invoices", Column: "customer_region",
		RefSchema: "billing", RefTable: "customers", RefColumn: "region", Constraint: "fk_inv_cust",
	})
	b := mysqlBuildBatch("h:3306", in)
	var refs []string
	for _, e := range b.Edges {
		if e.Relation == "references" {
			refs = append(refs, e.Source+"->"+e.Target+" "+e.Metadata["columns"])
			if e.Source != "mysql://h:3306/billing/invoices" || e.Target != "mysql://h:3306/billing/customers" {
				t.Errorf("unexpected FK edge %s -> %s", e.Source, e.Target)
			}
			if e.Confidence != "EXTRACTED" {
				t.Errorf("FK confidence = %s", e.Confidence)
			}
			if !strings.Contains(e.Metadata["columns"], "customer_id->id") ||
				!strings.Contains(e.Metadata["columns"], "customer_region->region") {
				t.Errorf("FK columns metadata = %q", e.Metadata["columns"])
			}
		}
	}
	if len(refs) != 1 {
		t.Fatalf("references edges = %d, want 1 (grouped by constraint): %v", len(refs), refs)
	}
}

func TestMySQLDeterminism(t *testing.T) {
	in1 := mysqlFakeIntrospection()
	in2 := mysqlFakeIntrospection()
	// Reverse every slice in in2 — same multiset, different order.
	for i, j := 0, len(in2.Tables)-1; i < j; i, j = i+1, j-1 {
		in2.Tables[i], in2.Tables[j] = in2.Tables[j], in2.Tables[i]
	}
	for i, j := 0, len(in2.Columns)-1; i < j; i, j = i+1, j-1 {
		in2.Columns[i], in2.Columns[j] = in2.Columns[j], in2.Columns[i]
	}
	for i, j := 0, len(in2.Schemas)-1; i < j; i, j = i+1, j-1 {
		in2.Schemas[i], in2.Schemas[j] = in2.Schemas[j], in2.Schemas[i]
	}
	j1, _ := json.Marshal(mysqlBuildBatch("h:3306", in1))
	j2, _ := json.Marshal(mysqlBuildBatch("h:3306", in2))
	if string(j1) != string(j2) {
		t.Errorf("batch not deterministic under input reordering:\n%s\n%s", j1, j2)
	}
}

func TestMySQLNoSecretInBatch(t *testing.T) {
	const fakePW = "Sup3rS3kr3tPW"
	dsn, hostport, dbname, err := mysqlURLToDSN("mysql://app:" + fakePW + "@db.internal:3306/billing")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, fakePW) {
		t.Fatal("DSN must carry the password for the dial")
	}
	if strings.Contains(hostport, fakePW) || strings.Contains(dbname, fakePW) {
		t.Fatal("id inputs carry the password")
	}
	b := mysqlBuildBatch(hostport, mysqlFakeIntrospection())
	out, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), fakePW) {
		t.Fatalf("password leaked into batch: %s", out)
	}
	for _, n := range b.Nodes {
		if strings.Contains(n.ID, "@") || strings.Contains(n.Source, "@") {
			t.Errorf("userinfo remnant in node %s (source %s)", n.ID, n.Source)
		}
	}
}

func TestMySQLMappingPerf100Tables(t *testing.T) {
	var in mysqlIntrospection
	in.Schemas = []string{"app"}
	for i := 0; i < 100; i++ {
		tn := fmt.Sprintf("table_%03d", i)
		in.Tables = append(in.Tables, mysqlTableRow{Schema: "app", Name: tn})
		for c := 0; c < 10; c++ {
			in.Columns = append(in.Columns, mysqlColumnRow{
				Schema: "app", Table: tn, Name: fmt.Sprintf("col_%02d", c),
				ColumnType: "bigint", Position: c + 1,
			})
		}
		in.PrimaryKeys = append(in.PrimaryKeys, mysqlKeyRow{Schema: "app", Table: tn, Column: "col_00"})
		if i > 0 {
			in.ForeignKeys = append(in.ForeignKeys, mysqlFKRow{
				Schema: "app", Table: tn, Column: "col_01",
				RefSchema: "app", RefTable: "table_000", RefColumn: "col_00",
				Constraint: fmt.Sprintf("fk_%03d", i),
			})
		}
	}
	start := time.Now()
	b := mysqlBuildBatch("h:3306", in)
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Errorf("100-table mapping took %v, budget 50ms", elapsed)
	}
	if got := len(b.Nodes); got != 1+100+1000 {
		t.Errorf("nodes = %d, want 1101", got)
	}
}

func TestMySQLConnectorRegistered(t *testing.T) {
	name, err := sources.Route("mysql://h:3306/db")
	if err != nil || name != "mysql" {
		t.Fatalf("Route = %q, %v", name, err)
	}
	c, err := sources.Lookup(name)
	if err != nil {
		t.Fatal(err)
	}
	if c.Example() == "" || len(c.Params()) == 0 {
		t.Error("Example/Params must be populated")
	}
	if _, err := sources.HelpCard("mysql"); err != nil {
		t.Errorf("sources.HelpCard(mysql): %v", err)
	}
}

// TestMySQLSmoke is the env-gated real-server smoke:
// CTX_OPTIMIZE_TEST_MYSQL=mysql://user:pass@host:3306/db go test ...
func TestMySQLSmoke(t *testing.T) {
	u := os.Getenv("CTX_OPTIMIZE_TEST_MYSQL")
	if u == "" {
		t.Skip("CTX_OPTIMIZE_TEST_MYSQL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	b, err := mysqlConnector{}.Capture(ctx, u)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("capture took %v, budget 2s", elapsed)
	}
	if len(b.Nodes) == 0 {
		t.Error("real capture produced zero nodes")
	}
	b.Producer = "smoke:mysql"
	if err := b.Validate(); err != nil {
		t.Errorf("batch invalid: %v", err)
	}
}
