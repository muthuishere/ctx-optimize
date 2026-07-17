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

// ---- URL parsing (driver-native URLs; scheme rewrite + database param) ----

func TestMSSQLParseURL(t *testing.T) {
	cases := []struct {
		name       string
		url        string
		wantDial   string
		wantScheme string
		wantHost   string
		wantDB     string
	}{
		{"sqlserver native", "sqlserver://sa:pw@db.internal:1433?database=appdb",
			"sqlserver://sa:pw@db.internal:1433?database=appdb", "sqlserver", "db.internal:1433", "appdb"},
		{"mssql rewritten", "mssql://sa:pw@db.internal:1433?database=appdb",
			"sqlserver://sa:pw@db.internal:1433?database=appdb", "mssql", "db.internal:1433", "appdb"},
		{"default port appended to id", "sqlserver://sa:pw@db.internal?database=appdb",
			"sqlserver://sa:pw@db.internal?database=appdb", "sqlserver", "db.internal:1433", "appdb"},
		{"no database param", "sqlserver://sa:pw@h:1433",
			"sqlserver://sa:pw@h:1433", "sqlserver", "h:1433", ""},
		{"instance path", "sqlserver://sa:pw@h/SQLEXPRESS?database=d",
			"sqlserver://sa:pw@h/SQLEXPRESS?database=d", "sqlserver", "h/SQLEXPRESS", "d"},
		{"encoded database param", "sqlserver://sa:pw@h:1433?database=app%20db",
			"sqlserver://sa:pw@h:1433?database=app%20db", "sqlserver", "h:1433", "app db"},
		{"no userinfo", "sqlserver://h:1433?database=d",
			"sqlserver://h:1433?database=d", "sqlserver", "h:1433", "d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dial, scheme, host, db, err := mssqlParseURL(tc.url)
			if err != nil {
				t.Fatalf("mssqlParseURL(%q): %v", tc.url, err)
			}
			if dial != tc.wantDial {
				t.Errorf("dial = %q, want %q", dial, tc.wantDial)
			}
			if scheme != tc.wantScheme {
				t.Errorf("scheme = %q, want %q", scheme, tc.wantScheme)
			}
			if host != tc.wantHost {
				t.Errorf("host = %q, want %q", host, tc.wantHost)
			}
			if db != tc.wantDB {
				t.Errorf("database = %q, want %q", db, tc.wantDB)
			}
		})
	}
}

func TestMSSQLParseURLErrors(t *testing.T) {
	secret := "TOPSECRETPW"
	for _, u := range []string{
		"mysql://u:" + secret + "@h/d", // wrong scheme
		"mssql://",                     // missing host
	} {
		_, _, _, _, err := mssqlParseURL(u)
		if err == nil {
			t.Errorf("mssqlParseURL(%q): want error", u)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error for %q echoes the password: %s", u, err)
		}
	}
}

// ---- mapping seam ----

func mssqlFakeIntrospection() mssqlIntrospection {
	return mssqlIntrospection{
		Schemas: []string{"dbo", "sales", "sys", "INFORMATION_SCHEMA", "guest", "db_owner", "db_datareader"},
		Tables: []mssqlTableRow{
			{Schema: "dbo", Name: "Orders"},
			{Schema: "dbo", Name: "Customers"},
			{Schema: "sales", Name: "Regions"},
			{Schema: "dbo", Name: "vOpenOrders", IsView: true},
			{Schema: "sys", Name: "objects"},
			{Schema: "db_owner", Name: "junk"},
		},
		Columns: []mssqlColumnRow{
			{Schema: "dbo", Table: "Orders", Name: "OrderID", DataType: "bigint", Position: 1},
			{Schema: "dbo", Table: "Orders", Name: "CustomerID", DataType: "bigint", Position: 2},
			{Schema: "dbo", Table: "Orders", Name: "Note", DataType: "nvarchar", Nullable: true, Position: 3},
			{Schema: "dbo", Table: "Customers", Name: "CustomerID", DataType: "bigint", Position: 1},
			{Schema: "sales", Table: "Regions", Name: "RegionID", DataType: "int", Position: 1},
			{Schema: "sys", Table: "objects", Name: "object_id", DataType: "int", Position: 1},
		},
		PrimaryKeys: []mssqlKeyRow{
			{Schema: "dbo", Table: "Orders", Column: "OrderID"},
			{Schema: "dbo", Table: "Customers", Column: "CustomerID"},
		},
		Indexes: []mssqlIndexRow{
			{Schema: "dbo", Table: "Orders", Count: 3},
		},
		ForeignKeys: []mssqlFKRow{
			{Schema: "dbo", Table: "Orders", Column: "CustomerID",
				RefSchema: "dbo", RefTable: "Customers", RefColumn: "CustomerID",
				Constraint: "FK_Orders_Customers"},
		},
	}
}

func TestMSSQLSystemSchemaExclusion(t *testing.T) {
	b := mssqlBuildBatch("sqlserver", "h:1433", "appdb", mssqlFakeIntrospection())
	for _, n := range b.Nodes {
		for _, sys := range []string{"/sys", "/INFORMATION_SCHEMA", "/guest", "/db_owner", "/db_datareader"} {
			if strings.HasPrefix(n.ID, "sqlserver://h:1433/appdb"+sys+"/") || n.ID == "sqlserver://h:1433/appdb"+sys {
				t.Errorf("system-schema node leaked: %s", n.ID)
			}
		}
	}
	want := map[string]bool{
		"sqlserver://h:1433/appdb":                 true,
		"sqlserver://h:1433/appdb/dbo":             true,
		"sqlserver://h:1433/appdb/sales":           true,
		"sqlserver://h:1433/appdb/dbo/Orders":      true,
		"sqlserver://h:1433/appdb/dbo/Customers":   true,
		"sqlserver://h:1433/appdb/dbo/vOpenOrders": true,
		"sqlserver://h:1433/appdb/sales/Regions":   true,
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

func TestMSSQLPartitionCollapse(t *testing.T) {
	in := mssqlFakeIntrospection()
	// sys.partitions style: rows repeat per index; distinct partition_number
	// is what counts. 8 distinct partitions, each seen twice (2 indexes).
	for idx := 0; idx < 2; idx++ {
		for p := 1; p <= 8; p++ {
			in.Partitions = append(in.Partitions, mssqlPartitionRow{
				Schema: "dbo", Table: "Orders", PartitionNumber: p,
			})
		}
	}
	// Unpartitioned table: the universal single partition 1 → NO fact.
	in.Partitions = append(in.Partitions, mssqlPartitionRow{
		Schema: "dbo", Table: "Customers", PartitionNumber: 1,
	})
	b := mssqlBuildBatch("sqlserver", "h:1433", "appdb", in)
	for _, n := range b.Nodes {
		switch n.ID {
		case "sqlserver://h:1433/appdb/dbo/Orders":
			if n.Metadata["partitions"] != "8" {
				t.Errorf("Orders partitions fact = %q, want 8 (distinct collapse)", n.Metadata["partitions"])
			}
		case "sqlserver://h:1433/appdb/dbo/Customers":
			if _, ok := n.Metadata["partitions"]; ok {
				t.Error("single-partition table must not carry a partitions fact")
			}
		}
		if n.Kind == "partition" {
			t.Errorf("partition enumerated as a node: %s", n.ID)
		}
	}
}

func TestMSSQLFKEdges(t *testing.T) {
	in := mssqlFakeIntrospection()
	in.ForeignKeys = append(in.ForeignKeys, mssqlFKRow{
		Schema: "dbo", Table: "Orders", Column: "CustomerRegion",
		RefSchema: "dbo", RefTable: "Customers", RefColumn: "Region",
		Constraint: "FK_Orders_Customers",
	})
	b := mssqlBuildBatch("sqlserver", "h:1433", "appdb", in)
	count := 0
	for _, e := range b.Edges {
		if e.Relation != "references" {
			continue
		}
		count++
		if e.Source != "sqlserver://h:1433/appdb/dbo/Orders" ||
			e.Target != "sqlserver://h:1433/appdb/dbo/Customers" {
			t.Errorf("unexpected FK edge %s -> %s", e.Source, e.Target)
		}
		if e.Confidence != "EXTRACTED" {
			t.Errorf("FK confidence = %s", e.Confidence)
		}
		if !strings.Contains(e.Metadata["columns"], "CustomerID->CustomerID") ||
			!strings.Contains(e.Metadata["columns"], "CustomerRegion->Region") {
			t.Errorf("FK columns metadata = %q", e.Metadata["columns"])
		}
	}
	if count != 1 {
		t.Fatalf("references edges = %d, want 1 (grouped by constraint)", count)
	}
}

func TestMSSQLDeterminism(t *testing.T) {
	in1 := mssqlFakeIntrospection()
	in2 := mssqlFakeIntrospection()
	for i, j := 0, len(in2.Tables)-1; i < j; i, j = i+1, j-1 {
		in2.Tables[i], in2.Tables[j] = in2.Tables[j], in2.Tables[i]
	}
	for i, j := 0, len(in2.Columns)-1; i < j; i, j = i+1, j-1 {
		in2.Columns[i], in2.Columns[j] = in2.Columns[j], in2.Columns[i]
	}
	for i, j := 0, len(in2.Schemas)-1; i < j; i, j = i+1, j-1 {
		in2.Schemas[i], in2.Schemas[j] = in2.Schemas[j], in2.Schemas[i]
	}
	j1, _ := json.Marshal(mssqlBuildBatch("sqlserver", "h:1433", "appdb", in1))
	j2, _ := json.Marshal(mssqlBuildBatch("sqlserver", "h:1433", "appdb", in2))
	if string(j1) != string(j2) {
		t.Errorf("batch not deterministic under input reordering:\n%s\n%s", j1, j2)
	}
}

func TestMSSQLNoSecretInBatch(t *testing.T) {
	const fakePW = "Sup3rS3kr3tPW"
	dial, scheme, host, db, err := mssqlParseURL("mssql://sa:" + fakePW + "@db.internal:1433?database=appdb")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dial, fakePW) {
		t.Fatal("dial URL must carry the password for the driver")
	}
	for what, s := range map[string]string{"scheme": scheme, "host": host, "db": db} {
		if strings.Contains(s, fakePW) {
			t.Fatalf("id input %s carries the password: %q", what, s)
		}
	}
	b := mssqlBuildBatch(scheme, host, db, mssqlFakeIntrospection())
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

func TestMSSQLMappingPerf100Tables(t *testing.T) {
	var in mssqlIntrospection
	in.Schemas = []string{"dbo"}
	for i := 0; i < 100; i++ {
		tn := fmt.Sprintf("Table_%03d", i)
		in.Tables = append(in.Tables, mssqlTableRow{Schema: "dbo", Name: tn})
		for c := 0; c < 10; c++ {
			in.Columns = append(in.Columns, mssqlColumnRow{
				Schema: "dbo", Table: tn, Name: fmt.Sprintf("Col_%02d", c),
				DataType: "bigint", Position: c + 1,
			})
		}
		in.PrimaryKeys = append(in.PrimaryKeys, mssqlKeyRow{Schema: "dbo", Table: tn, Column: "Col_00"})
		in.Partitions = append(in.Partitions, mssqlPartitionRow{Schema: "dbo", Table: tn, PartitionNumber: 1})
		if i > 0 {
			in.ForeignKeys = append(in.ForeignKeys, mssqlFKRow{
				Schema: "dbo", Table: tn, Column: "Col_01",
				RefSchema: "dbo", RefTable: "Table_000", RefColumn: "Col_00",
				Constraint: fmt.Sprintf("FK_%03d", i),
			})
		}
	}
	start := time.Now()
	b := mssqlBuildBatch("sqlserver", "h:1433", "appdb", in)
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Errorf("100-table mapping took %v, budget 50ms", elapsed)
	}
	if got := len(b.Nodes); got != 1+1+100+1000 { // db + schema + tables + columns
		t.Errorf("nodes = %d, want 1102", got)
	}
}

func TestMSSQLConnectorRegistered(t *testing.T) {
	for scheme, u := range map[string]string{
		"mssql":     "mssql://h:1433?database=d",
		"sqlserver": "sqlserver://h:1433?database=d",
	} {
		name, err := sources.Route(u)
		if err != nil || name != scheme {
			t.Fatalf("sources.Route(%q) = %q, %v", u, name, err)
		}
		c, err := sources.Lookup(name)
		if err != nil {
			t.Fatal(err)
		}
		if c.Scheme() != scheme {
			t.Errorf("sources.Lookup(%q).Scheme() = %q", scheme, c.Scheme())
		}
		if c.Example() == "" || len(c.Params()) == 0 {
			t.Error("Example/Params must be populated")
		}
	}
	if _, err := sources.HelpCard("mssql"); err != nil {
		t.Errorf("sources.HelpCard(mssql): %v", err)
	}
}

// TestMSSQLSmoke is the env-gated real-server smoke:
// CTX_OPTIMIZE_TEST_MSSQL=sqlserver://user:pass@host:1433?database=db go test ...
func TestMSSQLSmoke(t *testing.T) {
	u := os.Getenv("CTX_OPTIMIZE_TEST_MSSQL")
	if u == "" {
		t.Skip("CTX_OPTIMIZE_TEST_MSSQL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	b, err := mssqlConnector{scheme: "sqlserver"}.Capture(ctx, u)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("capture took %v, budget 2s", elapsed)
	}
	if len(b.Nodes) == 0 {
		t.Error("real capture produced zero nodes")
	}
	b.Producer = "smoke:mssql"
	if err := b.Validate(); err != nil {
		t.Errorf("batch invalid: %v", err)
	}
}
