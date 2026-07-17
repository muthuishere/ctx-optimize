// mssql.go — the mssql:// / sqlserver:// native-source connector (ADR
// 2026-07-17-bundled-adapter-templates, Stage 2). One dial, one Batch,
// deterministic. IMPORTANT: imports ONLY the minimal
// github.com/microsoft/go-mssqldb path — the azure auth subpackages
// (azuread/azkeys/azidentity) must never enter the binary's build graph.
// Introspects the sys.* catalog views for the LOGICAL shape only: system
// schemas (sys, INFORMATION_SCHEMA, guest, db_*) are skipped, and
// sys.partitions rows collapse to a COUNT fact on the table node (distinct
// partition_number > 1) — partitions are never enumerated as nodes.
package connectors

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	_ "github.com/microsoft/go-mssqldb" // registers the "sqlserver" driver; minimal path only

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/sources"
)

func init() {
	sources.Register(mssqlConnector{scheme: "mssql"})
	sources.Register(mssqlConnector{scheme: "sqlserver"})
}

// mssqlConnector serves both spellings; the driver's native URL form is
// sqlserver://, so mssql:// is rewritten textually before the dial.
type mssqlConnector struct{ scheme string }

func (c mssqlConnector) Scheme() string { return c.scheme }

func (c mssqlConnector) Example() string {
	return c.scheme + "://$MSSQL_USER:$MSSQL_PASS@db.internal:1433?database=appdb"
}

func (c mssqlConnector) Params() []sources.Param {
	return []sources.Param{
		{Name: "user:pass userinfo", Desc: "SQL Server credentials; percent-encode URL-special characters ('/' → %2F, '@' → %40, ':' → %3A)", Cred: true},
		{Name: "host:port", Desc: "server address; port defaults to 1433 (instance form host/instance also accepted)"},
		{Name: "database", Desc: "database to capture (query param); omit to capture the login's default database"},
		{Name: "encrypt", Desc: "driver TLS mode, passed through: true | false | strict"},
		{Name: "trustservercertificate", Desc: "true skips server certificate verification (passed through to the driver)"},
		{Name: "certificate", Desc: "PATH to a CA certificate PEM the server certificate must chain to (passed through)"},
	}
}

// Capture dials once (driver-native URL), introspects sys.*, and emits the
// logical shape. Error text carries scheme+host only — never userinfo (and
// the caller scrubs regardless).
func (c mssqlConnector) Capture(ctx context.Context, rawURL string) (*schema.Batch, error) {
	dialURL, scheme, hostbase, dbname, err := mssqlParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlserver", dialURL)
	if err != nil {
		return nil, mssqlErr(scheme, hostbase, "open", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	if err := db.PingContext(ctx); err != nil {
		return nil, mssqlErr(scheme, hostbase, "connect", err)
	}
	if dbname == "" {
		if err := db.QueryRowContext(ctx, "SELECT DB_NAME()").Scan(&dbname); err != nil {
			return nil, mssqlErr(scheme, hostbase, "resolve database", err)
		}
	}
	in, err := mssqlIntrospect(ctx, db)
	if err != nil {
		return nil, mssqlErr(scheme, hostbase, "introspect", err)
	}
	return mssqlBuildBatch(scheme, hostbase, dbname, in), nil
}

// mssqlErr keeps failure text at scheme+host granularity.
func mssqlErr(scheme, hostbase, phase string, err error) error {
	return fmt.Errorf("%s://%s: %s: %v", scheme, hostbase, phase, err)
}

// mssqlParseURL textually parses the source URL (never net/url.Parse — its
// errors echo the full URL including the password). Returns the driver-ready
// dial URL (scheme normalized to sqlserver://, credentials left
// percent-encoded — the driver decodes them), the original scheme for ids,
// the credential-free host base (host:port, plus /instance when present),
// and the ?database= value.
func mssqlParseURL(raw string) (dialURL, scheme, hostbase, database string, err error) {
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "mssql://"):
		scheme = "mssql"
	case strings.HasPrefix(lower, "sqlserver://"):
		scheme = "sqlserver"
	default:
		return "", "", "", "", fmt.Errorf("mssql: value must start with mssql:// or sqlserver://")
	}
	rest := raw[len(scheme)+3:]
	dialURL = "sqlserver://" + rest

	if h := strings.Index(rest, "#"); h >= 0 {
		rest = rest[:h]
	}
	query := ""
	if q := strings.Index(rest, "?"); q >= 0 {
		rest, query = rest[:q], rest[q+1:]
	}
	authority, instance := rest, ""
	if i := strings.Index(rest, "/"); i >= 0 {
		authority, instance = rest[:i], rest[i+1:]
	}
	hostpart := authority
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		hostpart = authority[at+1:]
	}
	if hostpart == "" {
		return "", "", "", "", fmt.Errorf("mssql: missing host")
	}
	hostbase = hostpart
	if !strings.Contains(hostpart, ":") && instance == "" {
		hostbase = hostpart + ":1433"
	}
	if instance != "" {
		hostbase += "/" + instance
	}
	for _, kv := range strings.Split(query, "&") {
		k, v, _ := strings.Cut(kv, "=")
		if !strings.EqualFold(k, "database") {
			continue
		}
		if strings.Contains(v, "%") {
			dec, uerr := url.QueryUnescape(v)
			if uerr != nil {
				return "", "", "", "", fmt.Errorf("mssql: invalid percent-encoding in database param")
			}
			v = dec
		}
		database = v
	}
	return dialURL, scheme, hostbase, database, nil
}

// ---- introspection rows (the hermetic seam: queries fill these, the pure
// mapping below turns them into a Batch — tests feed fakes) ----

type mssqlTableRow struct {
	Schema, Name string
	IsView       bool
}

type mssqlColumnRow struct {
	Schema, Table, Name, DataType string
	Nullable                      bool
	Position                      int
}

type mssqlKeyRow struct {
	Schema, Table, Column string
}

type mssqlIndexRow struct {
	Schema, Table string
	Count         int
}

type mssqlFKRow struct {
	Schema, Table, Column          string
	RefSchema, RefTable, RefColumn string
	Constraint                     string
}

// mssqlPartitionRow is one sys.partitions row (rows repeat per index; the
// mapping collapses DISTINCT partition numbers per table to a count fact).
type mssqlPartitionRow struct {
	Schema, Table   string
	PartitionNumber int
}

type mssqlIntrospection struct {
	Schemas     []string
	Tables      []mssqlTableRow
	Columns     []mssqlColumnRow
	PrimaryKeys []mssqlKeyRow
	Indexes     []mssqlIndexRow
	ForeignKeys []mssqlFKRow
	Partitions  []mssqlPartitionRow
}

// mssqlSystemSchema — sys, INFORMATION_SCHEMA, guest, and the fixed db_*
// roles are physical/system namespaces, skipped everywhere (the
// logical-shape rule). SQL Server names compare case-insensitively.
func mssqlSystemSchema(name string) bool {
	l := strings.ToLower(name)
	return l == "sys" || l == "information_schema" || l == "guest" || strings.HasPrefix(l, "db_")
}

// mssqlIntrospect runs the fixed sys.* catalog queries (few big queries,
// bounded work) and fills the row seam.
func mssqlIntrospect(ctx context.Context, db *sql.DB) (mssqlIntrospection, error) {
	var in mssqlIntrospection
	{
		rows, err := db.QueryContext(ctx, "SELECT name FROM sys.schemas ORDER BY name")
		if err != nil {
			return in, err
		}
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				rows.Close()
				return in, err
			}
			in.Schemas = append(in.Schemas, s)
		}
		if err := rows.Close(); err != nil {
			return in, err
		}
	}
	{
		q := "SELECT s.name, o.name, CASE WHEN o.type = 'V' THEN 1 ELSE 0 END" +
			" FROM sys.objects o JOIN sys.schemas s ON o.schema_id = s.schema_id" +
			" WHERE o.type IN ('U','V') AND o.is_ms_shipped = 0" +
			" ORDER BY s.name, o.name"
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return in, err
		}
		for rows.Next() {
			var t mssqlTableRow
			var isView int
			if err := rows.Scan(&t.Schema, &t.Name, &isView); err != nil {
				rows.Close()
				return in, err
			}
			t.IsView = isView == 1
			in.Tables = append(in.Tables, t)
		}
		if err := rows.Close(); err != nil {
			return in, err
		}
	}
	{
		q := "SELECT s.name, o.name, c.name, ty.name, c.is_nullable, c.column_id" +
			" FROM sys.columns c" +
			" JOIN sys.objects o ON c.object_id = o.object_id AND o.type IN ('U','V') AND o.is_ms_shipped = 0" +
			" JOIN sys.schemas s ON o.schema_id = s.schema_id" +
			" JOIN sys.types ty ON c.user_type_id = ty.user_type_id" +
			" ORDER BY s.name, o.name, c.column_id"
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return in, err
		}
		for rows.Next() {
			var c mssqlColumnRow
			if err := rows.Scan(&c.Schema, &c.Table, &c.Name, &c.DataType, &c.Nullable, &c.Position); err != nil {
				rows.Close()
				return in, err
			}
			in.Columns = append(in.Columns, c)
		}
		if err := rows.Close(); err != nil {
			return in, err
		}
	}
	{
		q := "SELECT s.name, t.name, c.name" +
			" FROM sys.indexes i" +
			" JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id" +
			" JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id" +
			" JOIN sys.tables t ON i.object_id = t.object_id" +
			" JOIN sys.schemas s ON t.schema_id = s.schema_id" +
			" WHERE i.is_primary_key = 1" +
			" ORDER BY s.name, t.name, ic.key_ordinal"
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return in, err
		}
		for rows.Next() {
			var k mssqlKeyRow
			if err := rows.Scan(&k.Schema, &k.Table, &k.Column); err != nil {
				rows.Close()
				return in, err
			}
			in.PrimaryKeys = append(in.PrimaryKeys, k)
		}
		if err := rows.Close(); err != nil {
			return in, err
		}
	}
	{
		q := "SELECT s.name, t.name, COUNT(*)" +
			" FROM sys.indexes i" +
			" JOIN sys.tables t ON i.object_id = t.object_id" +
			" JOIN sys.schemas s ON t.schema_id = s.schema_id" +
			" WHERE i.type > 0 GROUP BY s.name, t.name ORDER BY s.name, t.name"
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return in, err
		}
		for rows.Next() {
			var ix mssqlIndexRow
			if err := rows.Scan(&ix.Schema, &ix.Table, &ix.Count); err != nil {
				rows.Close()
				return in, err
			}
			in.Indexes = append(in.Indexes, ix)
		}
		if err := rows.Close(); err != nil {
			return in, err
		}
	}
	{
		q := "SELECT ps.name, pt.name, pc.name, rs.name, rt.name, rc.name, fk.name" +
			" FROM sys.foreign_keys fk" +
			" JOIN sys.foreign_key_columns fkc ON fk.object_id = fkc.constraint_object_id" +
			" JOIN sys.tables pt ON fkc.parent_object_id = pt.object_id" +
			" JOIN sys.schemas ps ON pt.schema_id = ps.schema_id" +
			" JOIN sys.columns pc ON fkc.parent_object_id = pc.object_id AND fkc.parent_column_id = pc.column_id" +
			" JOIN sys.tables rt ON fkc.referenced_object_id = rt.object_id" +
			" JOIN sys.schemas rs ON rt.schema_id = rs.schema_id" +
			" JOIN sys.columns rc ON fkc.referenced_object_id = rc.object_id AND fkc.referenced_column_id = rc.column_id" +
			" ORDER BY fk.name, fkc.constraint_column_id"
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return in, err
		}
		for rows.Next() {
			var f mssqlFKRow
			if err := rows.Scan(&f.Schema, &f.Table, &f.Column, &f.RefSchema, &f.RefTable, &f.RefColumn, &f.Constraint); err != nil {
				rows.Close()
				return in, err
			}
			in.ForeignKeys = append(in.ForeignKeys, f)
		}
		if err := rows.Close(); err != nil {
			return in, err
		}
	}
	{
		q := "SELECT DISTINCT s.name, t.name, p.partition_number" +
			" FROM sys.partitions p" +
			" JOIN sys.tables t ON p.object_id = t.object_id" +
			" JOIN sys.schemas s ON t.schema_id = s.schema_id" +
			" ORDER BY s.name, t.name, p.partition_number"
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return in, err
		}
		for rows.Next() {
			var p mssqlPartitionRow
			if err := rows.Scan(&p.Schema, &p.Table, &p.PartitionNumber); err != nil {
				rows.Close()
				return in, err
			}
			in.Partitions = append(in.Partitions, p)
		}
		if err := rows.Close(); err != nil {
			return in, err
		}
	}
	return in, nil
}

// mssqlBuildBatch is the PURE mapping: already-queried rows → Batch. System
// schemas are filtered again here (defense in depth — hermetically tested),
// sys.partitions rows collapse to a distinct-count fact (only when > 1 —
// every SQL Server table has partition 1), and output is fully sorted so the
// same row multiset always renders the same bytes. Node ids follow
// scheme://host:port/db/schema/table/column; labels are schema-qualified.
func mssqlBuildBatch(scheme, hostbase, dbname string, in mssqlIntrospection) *schema.Batch {
	base := scheme + "://" + hostbase
	dbID := base + "/" + dbname
	b := &schema.Batch{Producer: "connector:mssql"}
	b.Nodes = append(b.Nodes, schema.Node{
		ID: dbID, Label: dbname, Kind: "database", FileType: "schema", Source: dbID,
	})

	schemas := map[string]bool{}
	for _, s := range in.Schemas {
		if !mssqlSystemSchema(s) {
			schemas[s] = true
		}
	}
	tableKey := func(s, t string) string { return strings.ToLower(s) + "." + strings.ToLower(t) }
	tables := map[string]bool{}
	for _, t := range in.Tables {
		if !mssqlSystemSchema(t.Schema) {
			schemas[t.Schema] = true
			tables[tableKey(t.Schema, t.Name)] = true
		}
	}
	// Distinct partition numbers per table; a fact only when > 1.
	partSet := map[string]map[int]bool{}
	for _, p := range in.Partitions {
		if mssqlSystemSchema(p.Schema) {
			continue
		}
		k := tableKey(p.Schema, p.Table)
		if partSet[k] == nil {
			partSet[k] = map[int]bool{}
		}
		partSet[k][p.PartitionNumber] = true
	}
	idxCount := map[string]int{}
	for _, ix := range in.Indexes {
		idxCount[tableKey(ix.Schema, ix.Table)] += ix.Count
	}
	pk := map[string]bool{}
	for _, k := range in.PrimaryKeys {
		pk[tableKey(k.Schema, k.Table)+"."+strings.ToLower(k.Column)] = true
	}

	for s := range schemas {
		id := dbID + "/" + s
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: s, Kind: "schema", FileType: "schema", Source: dbID,
		})
		b.Edges = append(b.Edges, schema.Edge{
			Source: dbID, Target: id, Relation: "contains", Confidence: schema.Extracted,
		})
	}
	for _, t := range in.Tables {
		if mssqlSystemSchema(t.Schema) {
			continue
		}
		kind := "table"
		if t.IsView {
			kind = "view"
		}
		id := dbID + "/" + t.Schema + "/" + t.Name
		md := map[string]string{}
		if n := len(partSet[tableKey(t.Schema, t.Name)]); n > 1 && !t.IsView {
			md["partitions"] = strconv.Itoa(n)
		}
		if n := idxCount[tableKey(t.Schema, t.Name)]; n > 0 && !t.IsView {
			md["indexes"] = strconv.Itoa(n)
		}
		if len(md) == 0 {
			md = nil
		}
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: t.Schema + "." + t.Name, Kind: kind,
			FileType: "schema", Source: dbID + "/" + t.Schema, Metadata: md,
		})
		b.Edges = append(b.Edges, schema.Edge{
			Source: dbID + "/" + t.Schema, Target: id,
			Relation: "contains", Confidence: schema.Extracted,
		})
	}
	for _, c := range in.Columns {
		if mssqlSystemSchema(c.Schema) || !tables[tableKey(c.Schema, c.Table)] {
			continue
		}
		tid := dbID + "/" + c.Schema + "/" + c.Table
		id := tid + "/" + c.Name
		md := map[string]string{
			"type":     c.DataType,
			"nullable": strconv.FormatBool(c.Nullable),
			"ordinal":  strconv.Itoa(c.Position),
		}
		if pk[tableKey(c.Schema, c.Table)+"."+strings.ToLower(c.Name)] {
			md["pk"] = "true"
		}
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: c.Schema + "." + c.Table + "." + c.Name, Kind: "column",
			FileType: "schema", Source: dbID + "/" + c.Schema, Metadata: md,
		})
		b.Edges = append(b.Edges, schema.Edge{
			Source: tid, Target: id, Relation: "contains", Confidence: schema.Extracted,
		})
	}

	// FK edges: one per constraint (multi-column FKs grouped), table → table.
	type fkAgg struct {
		src, dst, name string
		cols           []string
	}
	fkMap := map[string]*fkAgg{}
	var fkOrder []string
	for _, f := range in.ForeignKeys {
		if mssqlSystemSchema(f.Schema) || mssqlSystemSchema(f.RefSchema) {
			continue
		}
		if !tables[tableKey(f.Schema, f.Table)] {
			continue // source table must be captured; ref may be cross-batch
		}
		key := tableKey(f.Schema, f.Table) + "|" + f.Constraint + "|" + tableKey(f.RefSchema, f.RefTable)
		a, ok := fkMap[key]
		if !ok {
			a = &fkAgg{
				src:  dbID + "/" + f.Schema + "/" + f.Table,
				dst:  dbID + "/" + f.RefSchema + "/" + f.RefTable,
				name: f.Constraint,
			}
			fkMap[key] = a
			fkOrder = append(fkOrder, key)
		}
		a.cols = append(a.cols, f.Column+"->"+f.RefColumn)
	}
	sort.Strings(fkOrder)
	for _, key := range fkOrder {
		a := fkMap[key]
		sort.Strings(a.cols)
		b.Edges = append(b.Edges, schema.Edge{
			Source: a.src, Target: a.dst, Relation: "references", Confidence: schema.Extracted,
			Metadata: map[string]string{"constraint": a.name, "columns": strings.Join(a.cols, ",")},
		})
	}

	sort.Slice(b.Nodes, func(i, j int) bool { return b.Nodes[i].ID < b.Nodes[j].ID })
	sort.Slice(b.Edges, func(i, j int) bool {
		ki := b.Edges[i].Source + "|" + b.Edges[i].Relation + "|" + b.Edges[i].Target + "|" + b.Edges[i].Metadata["constraint"]
		kj := b.Edges[j].Source + "|" + b.Edges[j].Relation + "|" + b.Edges[j].Target + "|" + b.Edges[j].Metadata["constraint"]
		return ki < kj
	})
	return b
}
