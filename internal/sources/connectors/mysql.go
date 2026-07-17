// mysql.go — the mysql:// native-source connector (ADR
// 2026-07-17-bundled-adapter-templates, Stage 2). One dial, one Batch,
// deterministic. Introspects information_schema for the LOGICAL shape only:
// system schemas (mysql, sys, performance_schema, information_schema) are
// skipped, and partitions are a COUNT fact on the table node — never
// enumerated as nodes. The go-sql-driver DSN is built via mysql.Config
// (never string concatenation), so percent-decoded credentials with special
// characters survive; node ids/sources are built from the credential-free
// authority only.
package connectors

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	gomysql "github.com/go-sql-driver/mysql"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/sources"
)

func init() { sources.Register(mysqlConnector{}) }

type mysqlConnector struct{}

func (mysqlConnector) Scheme() string { return "mysql" }

func (mysqlConnector) Example() string {
	return "mysql://$MYSQL_USER:$MYSQL_PASS@db.internal:3306/appdb?tls=true"
}

func (mysqlConnector) Params() []sources.Param {
	return []sources.Param{
		{Name: "user:pass userinfo", Desc: "MySQL credentials; percent-encode URL-special characters ('/' → %2F, '@' → %40, ':' → %3A)", Cred: true},
		{Name: "host:port", Desc: "server address; port defaults to 3306"},
		{Name: "/dbname", Desc: "capture only this database; omit to capture every non-system schema the login can see"},
		{Name: "tls", Desc: "TLS mode passed to the driver: true | skip-verify | preferred"},
		{Name: "ssl-ca", Desc: "PATH to a CA certificate PEM for server verification (also accepted: sslca)"},
		{Name: "ssl-cert", Desc: "PATH to a client certificate PEM, paired with ssl-key (also accepted: sslcert)"},
		{Name: "ssl-key", Desc: "PATH to the client key PEM (also accepted: sslkey); key contents never leave this machine"},
	}
}

// Capture dials once, introspects information_schema, and emits the logical
// shape. Error text carries scheme+host only — never userinfo (and the
// caller scrubs regardless).
func (mysqlConnector) Capture(ctx context.Context, rawURL string) (*schema.Batch, error) {
	dsn, hostport, dbname, err := mysqlURLToDSN(rawURL)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, mysqlErr(hostport, "open", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	if err := db.PingContext(ctx); err != nil {
		return nil, mysqlErr(hostport, "connect", err)
	}
	in, err := mysqlIntrospect(ctx, db, dbname)
	if err != nil {
		return nil, mysqlErr(hostport, "introspect", err)
	}
	return mysqlBuildBatch(hostport, in), nil
}

// mysqlErr keeps failure text at scheme+host granularity. Driver errors are
// network/server messages (no DSN echo); the run layer scrubs values anyway.
func mysqlErr(hostport, phase string, err error) error {
	return fmt.Errorf("mysql://%s: %s: %v", hostport, phase, err)
}

// mysqlUnescape percent-decodes one URL component. Its error NEVER echoes
// the component value — a malformed escape in a password must not leak the
// password into an error string.
func mysqlUnescape(s, what string) (string, error) {
	if !strings.Contains(s, "%") {
		return s, nil
	}
	out, err := url.PathUnescape(s)
	if err != nil {
		return "", fmt.Errorf("mysql: invalid percent-encoding in %s (URL-special characters must be encoded: '/' → %%2F, '@' → %%40, ':' → %%3A)", what)
	}
	return out, nil
}

// mysqlURLToDSN converts the mysql:// URL convention to the go-sql-driver
// DSN (the driver does NOT take URLs). Parsing is textual — never
// net/url.Parse, whose errors echo the full URL including the password.
// Credentials are percent-decoded; the DSN is rendered by mysql.Config
// .FormatDSN so special characters survive. hostport (credential-free) is
// the id base; dbname restricts the capture when present.
func mysqlURLToDSN(raw string) (dsn, hostport, dbname string, err error) {
	if !strings.HasPrefix(strings.ToLower(raw), "mysql://") {
		return "", "", "", fmt.Errorf("mysql: value must start with mysql://")
	}
	rest := raw[len("mysql://"):]
	if h := strings.Index(rest, "#"); h >= 0 {
		rest = rest[:h] // fragments have no meaning here
	}
	query := ""
	if q := strings.Index(rest, "?"); q >= 0 {
		rest, query = rest[:q], rest[q+1:]
	}
	authority, path := rest, ""
	if i := strings.Index(rest, "/"); i >= 0 {
		authority, path = rest[:i], rest[i+1:]
	}
	user, pass := "", ""
	hostpart := authority
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		userinfo := authority[:at]
		hostpart = authority[at+1:]
		if c := strings.Index(userinfo, ":"); c >= 0 {
			user, pass = userinfo[:c], userinfo[c+1:]
		} else {
			user = userinfo
		}
		if user, err = mysqlUnescape(user, "username"); err != nil {
			return "", "", "", err
		}
		if pass, err = mysqlUnescape(pass, "password"); err != nil {
			return "", "", "", err
		}
	}
	if hostpart == "" {
		return "", "", "", fmt.Errorf("mysql: missing host")
	}
	hostport = hostpart
	if !strings.Contains(hostpart, ":") {
		hostport = hostpart + ":3306"
	}
	if i := strings.Index(path, "/"); i >= 0 {
		path = path[:i] // only the first segment names a database
	}
	if dbname, err = mysqlUnescape(path, "database name"); err != nil {
		return "", "", "", err
	}

	cfg := gomysql.NewConfig()
	cfg.User, cfg.Passwd = user, pass
	cfg.Net, cfg.Addr = "tcp", hostport
	cfg.DBName = dbname
	cfg.Timeout = 10 * time.Second

	var caPath, certPath, keyPath string
	if query != "" {
		for _, kv := range strings.Split(query, "&") {
			if kv == "" {
				continue
			}
			k, v, _ := strings.Cut(kv, "=")
			vv, err := mysqlUnescape(v, "query param "+k)
			if err != nil {
				return "", "", "", err
			}
			switch strings.ToLower(k) {
			case "ssl-ca", "sslca":
				caPath = vv
			case "ssl-cert", "sslcert":
				certPath = vv
			case "ssl-key", "sslkey":
				keyPath = vv
			case "tls":
				cfg.TLSConfig = vv
			default:
				if cfg.Params == nil {
					cfg.Params = map[string]string{}
				}
				cfg.Params[k] = vv
			}
		}
	}
	if caPath != "" || certPath != "" || keyPath != "" {
		name, err := mysqlRegisterTLS(caPath, certPath, keyPath)
		if err != nil {
			return "", "", "", err
		}
		cfg.TLSConfig = name
	}
	return cfg.FormatDSN(), hostport, dbname, nil
}

// mysqlRegisterTLS builds a tls.Config from cert PATHS in the URL and
// registers it with the driver under a deterministic name (derived from the
// paths, so re-registration is idempotent per path set). Cert/key CONTENTS
// are loaded here for the dial only — they never reach an id, error, or log.
func mysqlRegisterTLS(caPath, certPath, keyPath string) (string, error) {
	h := sha256.Sum256([]byte(caPath + "\x00" + certPath + "\x00" + keyPath))
	name := "ctxopt-" + hex.EncodeToString(h[:6])
	tc := &tls.Config{}
	if caPath != "" {
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return "", fmt.Errorf("mysql: read ssl-ca %s: %v", caPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return "", fmt.Errorf("mysql: ssl-ca %s: no PEM certificates found", caPath)
		}
		tc.RootCAs = pool
	}
	if certPath != "" || keyPath != "" {
		if certPath == "" || keyPath == "" {
			return "", fmt.Errorf("mysql: ssl-cert and ssl-key must be set together")
		}
		pair, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return "", fmt.Errorf("mysql: load client cert pair (%s, %s): %v", certPath, keyPath, err)
		}
		tc.Certificates = []tls.Certificate{pair}
	}
	if err := gomysql.RegisterTLSConfig(name, tc); err != nil {
		return "", fmt.Errorf("mysql: register tls config: %v", err)
	}
	return name, nil
}

// ---- introspection rows (the hermetic seam: queries fill these, the pure
// mapping below turns them into a Batch — tests feed fakes) ----

type mysqlTableRow struct {
	Schema, Name string
	IsView       bool
}

// mysqlPartitionRow is ONE physical partition row from
// information_schema.partitions (partition_name IS NOT NULL). The mapping
// collapses these to a count fact — partitions are never nodes.
type mysqlPartitionRow struct {
	Schema, Table, Partition string
}

type mysqlColumnRow struct {
	Schema, Table, Name, ColumnType, Default string
	Nullable                                 bool
	HasDefault                               bool
	Position                                 int
}

type mysqlKeyRow struct {
	Schema, Table, Column string
}

type mysqlFKRow struct {
	Schema, Table, Column          string
	RefSchema, RefTable, RefColumn string
	Constraint                     string
}

type mysqlIntrospection struct {
	Schemas     []string
	Tables      []mysqlTableRow
	Partitions  []mysqlPartitionRow
	Columns     []mysqlColumnRow
	PrimaryKeys []mysqlKeyRow
	ForeignKeys []mysqlFKRow
}

// mysqlSystemSchemas — skipped everywhere (the logical-shape rule).
var mysqlSystemSchemas = []string{"mysql", "sys", "performance_schema", "information_schema"}

func mysqlSystemSchema(name string) bool {
	for _, s := range mysqlSystemSchemas {
		if strings.EqualFold(name, s) {
			return true
		}
	}
	return false
}

const mysqlSystemSchemaSQL = "('mysql','sys','performance_schema','information_schema')"

// mysqlIntrospect runs the fixed catalog queries (few big queries, bounded
// work) and fills the row seam. dbname != "" scopes every query to that
// database.
func mysqlIntrospect(ctx context.Context, db *sql.DB, dbname string) (mysqlIntrospection, error) {
	var in mysqlIntrospection
	where := func(col string) (string, []any) {
		w := col + " NOT IN " + mysqlSystemSchemaSQL
		if dbname != "" {
			return w + " AND " + col + " = ?", []any{dbname}
		}
		return w, nil
	}

	{
		w, args := where("schema_name")
		q := "SELECT schema_name FROM information_schema.schemata WHERE " + w + " ORDER BY schema_name"
		rows, err := db.QueryContext(ctx, q, args...)
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
		w, args := where("table_schema")
		q := "SELECT table_schema, table_name, table_type FROM information_schema.tables WHERE " + w +
			" ORDER BY table_schema, table_name"
		rows, err := db.QueryContext(ctx, q, args...)
		if err != nil {
			return in, err
		}
		for rows.Next() {
			var t mysqlTableRow
			var typ string
			if err := rows.Scan(&t.Schema, &t.Name, &typ); err != nil {
				rows.Close()
				return in, err
			}
			t.IsView = strings.EqualFold(typ, "VIEW")
			in.Tables = append(in.Tables, t)
		}
		if err := rows.Close(); err != nil {
			return in, err
		}
	}
	{
		w, args := where("table_schema")
		q := "SELECT table_schema, table_name, partition_name FROM information_schema.partitions" +
			" WHERE partition_name IS NOT NULL AND " + w +
			" ORDER BY table_schema, table_name, partition_name"
		rows, err := db.QueryContext(ctx, q, args...)
		if err != nil {
			return in, err
		}
		for rows.Next() {
			var p mysqlPartitionRow
			if err := rows.Scan(&p.Schema, &p.Table, &p.Partition); err != nil {
				rows.Close()
				return in, err
			}
			in.Partitions = append(in.Partitions, p)
		}
		if err := rows.Close(); err != nil {
			return in, err
		}
	}
	{
		w, args := where("table_schema")
		q := "SELECT table_schema, table_name, column_name, column_type, is_nullable, column_default, ordinal_position" +
			" FROM information_schema.columns WHERE " + w +
			" ORDER BY table_schema, table_name, ordinal_position"
		rows, err := db.QueryContext(ctx, q, args...)
		if err != nil {
			return in, err
		}
		for rows.Next() {
			var c mysqlColumnRow
			var nullable string
			var def sql.NullString
			if err := rows.Scan(&c.Schema, &c.Table, &c.Name, &c.ColumnType, &nullable, &def, &c.Position); err != nil {
				rows.Close()
				return in, err
			}
			c.Nullable = strings.EqualFold(nullable, "YES")
			c.Default, c.HasDefault = def.String, def.Valid
			in.Columns = append(in.Columns, c)
		}
		if err := rows.Close(); err != nil {
			return in, err
		}
	}
	{
		w, args := where("table_schema")
		q := "SELECT table_schema, table_name, column_name FROM information_schema.key_column_usage" +
			" WHERE constraint_name = 'PRIMARY' AND " + w +
			" ORDER BY table_schema, table_name, ordinal_position"
		rows, err := db.QueryContext(ctx, q, args...)
		if err != nil {
			return in, err
		}
		for rows.Next() {
			var k mysqlKeyRow
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
		w, args := where("table_schema")
		q := "SELECT table_schema, table_name, column_name, referenced_table_schema, referenced_table_name," +
			" referenced_column_name, constraint_name FROM information_schema.key_column_usage" +
			" WHERE referenced_table_name IS NOT NULL AND " + w +
			" ORDER BY constraint_name, ordinal_position"
		rows, err := db.QueryContext(ctx, q, args...)
		if err != nil {
			return in, err
		}
		for rows.Next() {
			var f mysqlFKRow
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
	return in, nil
}

// mysqlBuildBatch is the PURE mapping: already-queried rows → Batch. System
// schemas are filtered again here (defense in depth — hermetically tested),
// partitions collapse to a count fact, and output is fully sorted so the
// same row multiset always renders the same bytes. Node ids follow
// mysql://host:port/db/table/column; labels are schema-qualified.
func mysqlBuildBatch(hostport string, in mysqlIntrospection) *schema.Batch {
	base := "mysql://" + hostport
	b := &schema.Batch{Producer: "connector:mysql"}

	schemas := map[string]bool{}
	for _, s := range in.Schemas {
		if !mysqlSystemSchema(s) {
			schemas[s] = true
		}
	}
	tableKey := func(s, t string) string { return s + "." + t }
	tables := map[string]bool{}
	for _, t := range in.Tables {
		if !mysqlSystemSchema(t.Schema) {
			schemas[t.Schema] = true
			tables[tableKey(t.Schema, t.Name)] = true
		}
	}
	partCount := map[string]int{}
	for _, p := range in.Partitions {
		if p.Partition == "" || mysqlSystemSchema(p.Schema) {
			continue
		}
		partCount[tableKey(p.Schema, p.Table)]++
	}
	pk := map[string]bool{}
	for _, k := range in.PrimaryKeys {
		pk[k.Schema+"."+k.Table+"."+k.Column] = true
	}

	for s := range schemas {
		b.Nodes = append(b.Nodes, schema.Node{
			ID: base + "/" + s, Label: s, Kind: "database",
			FileType: "schema", Source: base + "/" + s,
		})
	}
	for _, t := range in.Tables {
		if mysqlSystemSchema(t.Schema) {
			continue
		}
		kind := "table"
		if t.IsView {
			kind = "view"
		}
		id := base + "/" + t.Schema + "/" + t.Name
		var md map[string]string
		if n := partCount[tableKey(t.Schema, t.Name)]; n > 0 && !t.IsView {
			md = map[string]string{"partitions": strconv.Itoa(n)}
		}
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: t.Schema + "." + t.Name, Kind: kind,
			FileType: "schema", Source: base + "/" + t.Schema, Metadata: md,
		})
		b.Edges = append(b.Edges, schema.Edge{
			Source: base + "/" + t.Schema, Target: id,
			Relation: "contains", Confidence: schema.Extracted,
		})
	}
	for _, c := range in.Columns {
		if mysqlSystemSchema(c.Schema) || !tables[tableKey(c.Schema, c.Table)] {
			continue
		}
		tid := base + "/" + c.Schema + "/" + c.Table
		id := tid + "/" + c.Name
		md := map[string]string{
			"type":     c.ColumnType,
			"nullable": strconv.FormatBool(c.Nullable),
			"ordinal":  strconv.Itoa(c.Position),
		}
		if c.HasDefault {
			md["default"] = c.Default
		}
		if pk[c.Schema+"."+c.Table+"."+c.Name] {
			md["pk"] = "true"
		}
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: c.Schema + "." + c.Table + "." + c.Name, Kind: "column",
			FileType: "schema", Source: base + "/" + c.Schema, Metadata: md,
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
		if mysqlSystemSchema(f.Schema) || mysqlSystemSchema(f.RefSchema) {
			continue
		}
		if !tables[tableKey(f.Schema, f.Table)] {
			continue // source table must be captured; ref may be cross-batch
		}
		key := f.Schema + "." + f.Table + "|" + f.Constraint + "|" + f.RefSchema + "." + f.RefTable
		a, ok := fkMap[key]
		if !ok {
			a = &fkAgg{
				src:  base + "/" + f.Schema + "/" + f.Table,
				dst:  base + "/" + f.RefSchema + "/" + f.RefTable,
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
