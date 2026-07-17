// Postgres connector (Stage 2, ADR 2026-07-17-bundled-adapter-templates).
// One dial, few big pg_catalog queries (never per-table round trips), one
// deterministic Batch. The logical-shape rule is enforced twice — in SQL
// (schema filters + NOT relispartition) AND again in Go (buildPGBatch), so a
// fake-row seam test and a real capture agree by construction.
//
// Secret hygiene: node ids/sources come from the SANITIZED url (entry.go
// Sanitize — userinfo + credential params stripped); error text carries
// scheme+host only, with the raw URL / userinfo / password literal-replaced
// before wrapping (run.go scrubs too, but this file never relies on it).
package connectors

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/sources"
)

// init arms the connector under "postgres". The postgresql:// scheme routes
// here too — entry.go's connectorForScheme maps it (sources.Route("postgresql://…")
// = "postgres" is pinned by TestRoute), so a second registration would
// change that contract rather than extend it.
func init() {
	sources.Register(pgConnector{})
}

// pgConnector implements Connector for postgres:// sources.
type pgConnector struct{}

func (pgConnector) Scheme() string { return "postgres" }

func (pgConnector) Params() []sources.Param {
	return []sources.Param{
		{Name: "user:pass userinfo", Desc: "credentials — postgres://$PG_USER:$PG_PASS@host:5432/db (single PG_URL var or a template)", Cred: true},
		{Name: "sslmode", Desc: "disable | require | verify-ca | verify-full (libpq URI param)"},
		{Name: "sslrootcert", Desc: "CA certificate PATH (paths are not secrets and may sit in the URL)"},
		{Name: "sslcert", Desc: "client certificate PATH"},
		{Name: "sslkey", Desc: "client key PATH — stripped from stored ids, contents never leave this machine"},
		{Name: "schemas", Desc: "ctx-optimize filter: capture only these schemas, comma-separated (?schemas=public,billing); removed before the dial"},
	}
}

func (pgConnector) Example() string {
	return "postgres://$PG_USER:$PG_PASS@db.internal:5432/appdb?sslmode=verify-full&schemas=public,billing"
}

func (c pgConnector) Capture(ctx context.Context, rawurl string) (*schema.Batch, error) {
	include, connURL := popPGSchemasParam(rawurl)
	host, err := pgHost(rawurl)
	if err != nil {
		return nil, err
	}
	secrets := pgSecrets(rawurl)
	conn, err := pgx.Connect(ctx, connURL)
	if err != nil {
		return nil, pgWrap(host, "connect", err, secrets)
	}
	defer conn.Close(ctx)
	var db string
	if err := conn.QueryRow(ctx, "SELECT current_database()").Scan(&db); err != nil {
		return nil, pgWrap(host, "current_database", err, secrets)
	}
	cat, err := loadPGCatalog(ctx, conn, include)
	if err != nil {
		return nil, pgWrap(host, "introspect", err, secrets)
	}
	return buildPGBatch(host, db, include, cat), nil
}

// ---- queried rows (the seam: tests feed these, Capture fills them) ----

type pgTable struct {
	Schema, Name string
	Kind         string // r=table p=partitioned v=view m=materialized view
	IsPartition  bool   // relispartition — a partition CHILD, never a node
}

type pgColumn struct {
	Schema, Table, Name, Type string
	Nullable                  bool
	Default                   string
}

type pgPK struct{ Schema, Table, Column string }

type pgFK struct {
	Name                           string // constraint name
	Schema, Table, Column          string
	RefSchema, RefTable, RefColumn string
}

type pgIndex struct{ Schema, Table, Name string }

type pgView struct{ Schema, Name, Definition string }

type pgComment struct{ Schema, Table, Column, Text string } // Column=="" → table-level

// pgCatalog is everything one capture queried, before shaping.
type pgCatalog struct {
	Tables     []pgTable
	Partitions map[string]int // "schema.table" → child count (a FACT, never nodes)
	Columns    []pgColumn
	PKs        []pgPK
	FKs        []pgFK
	Indexes    []pgIndex
	Views      []pgView
	Comments   []pgComment
}

// ---- the logical-shape rule, Go tier ----

// excludedPGSchema applies the logical-shape rule: catalog schemas, toolkit
// internals (pg_*), and TimescaleDB machinery are never captured.
func excludedPGSchema(name string) bool {
	return name == "information_schema" ||
		strings.HasPrefix(name, "pg_") ||
		strings.HasPrefix(name, "_timescaledb_")
}

const (
	pgViewDefCap   = 400 // view definition snippet cap (chars)
	pgIndexNameCap = 20  // index names listed per table; count is always exact
)

// buildPGBatch shapes queried rows into the Batch — pure, deterministic,
// sorted everywhere. host is the sanitized authority (verbatim, ports and
// multi-host kept); ids follow postgres://host/db/schema/table/column.
func buildPGBatch(host, db string, include []string, cat *pgCatalog) *schema.Batch {
	incSet := map[string]bool{}
	for _, s := range include {
		incSet[s] = true
	}
	keepSchema := func(s string) bool {
		if excludedPGSchema(s) {
			return false
		}
		return len(incSet) == 0 || incSet[s]
	}
	base := "postgres://" + host + "/" + db

	// tables kept: logical shape only — no partition children, no internals.
	kept := map[string]pgTable{} // "schema.table"
	schemas := map[string]bool{}
	for _, t := range cat.Tables {
		if t.IsPartition || !keepSchema(t.Schema) {
			continue
		}
		kept[t.Schema+"."+t.Name] = t
		schemas[t.Schema] = true
	}

	viewDef := map[string]string{}
	for _, v := range cat.Views {
		viewDef[v.Schema+"."+v.Name] = v.Definition
	}
	tblComment := map[string]string{}
	colComment := map[string]string{}
	for _, c := range cat.Comments {
		key := c.Schema + "." + c.Table
		if c.Column == "" {
			tblComment[key] = c.Text
		} else {
			colComment[key+"."+c.Column] = c.Text
		}
	}
	pkSet := map[string]bool{}
	for _, pk := range cat.PKs {
		pkSet[pk.Schema+"."+pk.Table+"."+pk.Column] = true
	}
	idxByTable := map[string][]string{}
	for _, ix := range cat.Indexes {
		key := ix.Schema + "." + ix.Table
		if _, ok := kept[key]; ok {
			idxByTable[key] = append(idxByTable[key], ix.Name)
		}
	}

	tableID := func(s, t string) string { return base + "/" + s + "/" + t }
	b := &schema.Batch{Producer: "postgres"}

	// db node
	b.Nodes = append(b.Nodes, schema.Node{
		ID: base, Label: db, Kind: "database", FileType: "schema", Source: base,
		Metadata: map[string]string{"host": host},
	})

	// schema nodes + db→schema contains
	for s := range schemas {
		id := base + "/" + s
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: s, Kind: "schema", FileType: "schema", Source: id,
		})
		b.Edges = append(b.Edges, schema.Edge{
			Source: base, Target: id, Relation: "contains", Confidence: schema.Extracted, Weight: 1,
		})
	}

	// table/view nodes + schema→table contains
	for key, t := range kept {
		id := tableID(t.Schema, t.Name)
		kind := "table"
		md := map[string]string{}
		switch t.Kind {
		case "v":
			kind = "view"
		case "m":
			kind = "view"
			md["materialized"] = "true"
		case "p":
			md["partitions"] = strconv.Itoa(cat.Partitions[key])
		}
		if n, ok := cat.Partitions[key]; ok && t.Kind != "p" {
			md["partitions"] = strconv.Itoa(n)
		}
		if def, ok := viewDef[key]; ok && def != "" {
			if len(def) > pgViewDefCap {
				def = def[:pgViewDefCap] + "…"
			}
			md["definition"] = def
		}
		if c, ok := tblComment[key]; ok && c != "" {
			md["comment"] = c
		}
		if names := idxByTable[key]; len(names) > 0 {
			sort.Strings(names)
			md["index_count"] = strconv.Itoa(len(names))
			if len(names) > pgIndexNameCap {
				names = names[:pgIndexNameCap]
			}
			md["indexes"] = strings.Join(names, ", ")
		}
		if len(md) == 0 {
			md = nil
		}
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: key, Kind: kind, FileType: "schema", Source: id, Metadata: md,
		})
		b.Edges = append(b.Edges, schema.Edge{
			Source: base + "/" + t.Schema, Target: id, Relation: "contains",
			Confidence: schema.Extracted, Weight: 1,
		})
	}

	// column nodes + table→column contains
	for _, c := range cat.Columns {
		key := c.Schema + "." + c.Table
		if _, ok := kept[key]; !ok {
			continue
		}
		id := tableID(c.Schema, c.Table) + "/" + c.Name
		md := map[string]string{"type": c.Type, "nullable": strconv.FormatBool(c.Nullable)}
		if c.Default != "" {
			md["default"] = c.Default
		}
		if pkSet[key+"."+c.Name] {
			md["primary_key"] = "true"
		}
		if cm, ok := colComment[key+"."+c.Name]; ok && cm != "" {
			md["comment"] = cm
		}
		b.Nodes = append(b.Nodes, schema.Node{
			ID: id, Label: c.Table + "." + c.Name, Kind: "column", FileType: "schema",
			Source: id, Metadata: md,
		})
		b.Edges = append(b.Edges, schema.Edge{
			Source: tableID(c.Schema, c.Table), Target: id, Relation: "contains",
			Confidence: schema.Extracted, Weight: 1,
		})
	}

	// FK edges: one edge per (table, referenced table) pair, constraints
	// aggregated — declared constraints are catalog-certain (EXTRACTED).
	type fkPair struct{ src, dst string }
	fkCols := map[fkPair]map[string][]string{} // pair → constraint → "col→refcol"
	for _, fk := range cat.FKs {
		if _, ok := kept[fk.Schema+"."+fk.Table]; !ok {
			continue
		}
		p := fkPair{tableID(fk.Schema, fk.Table), tableID(fk.RefSchema, fk.RefTable)}
		if fkCols[p] == nil {
			fkCols[p] = map[string][]string{}
		}
		fkCols[p][fk.Name] = append(fkCols[p][fk.Name], fk.Column+"→"+fk.RefColumn)
	}
	for p, byCon := range fkCols {
		var cons []string
		for name, cols := range byCon {
			sort.Strings(cols)
			cons = append(cons, name+"("+strings.Join(cols, ", ")+")")
		}
		sort.Strings(cons)
		b.Edges = append(b.Edges, schema.Edge{
			Source: p.src, Target: p.dst, Relation: "references",
			Confidence: schema.Extracted, Weight: 1,
			Metadata: map[string]string{"constraints": strings.Join(cons, "; ")},
		})
	}

	sort.Slice(b.Nodes, func(i, j int) bool { return b.Nodes[i].ID < b.Nodes[j].ID })
	sort.Slice(b.Edges, func(i, j int) bool {
		a, c := b.Edges[i], b.Edges[j]
		if a.Source != c.Source {
			return a.Source < c.Source
		}
		if a.Relation != c.Relation {
			return a.Relation < c.Relation
		}
		return a.Target < c.Target
	})
	return b
}

// ---- catalog queries (few big queries, reference: bench-pg Stage 0) ----

// pgSchemaFilter is the SQL tier of the logical-shape rule; buildPGBatch
// re-applies it in Go so both lanes agree.
const pgSchemaFilter = `n.nspname NOT IN ('pg_catalog','information_schema')
  AND n.nspname NOT LIKE 'pg\_%'
  AND n.nspname NOT LIKE '\_timescaledb\_%'`

const pgChildFilter = ` AND NOT c.relispartition`

func loadPGCatalog(ctx context.Context, conn *pgx.Conn, include []string) (*pgCatalog, error) {
	inc, incIdx := "", ""
	var args []any
	if len(include) > 0 {
		inc, incIdx = " AND n.nspname = ANY($1)", " AND i.schemaname = ANY($1)"
		args = []any{include}
	}
	cat := &pgCatalog{Partitions: map[string]int{}}
	q := func(sql string, scan func(rows pgx.Rows) error) error {
		rows, err := conn.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}

	// 1. tables + views (children excluded; parents are the nodes)
	if err := q(`SELECT n.nspname, c.relname, c.relkind::text, c.relispartition
	   FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	   WHERE c.relkind IN ('r','p','v','m') AND `+pgSchemaFilter+pgChildFilter+inc,
		func(rows pgx.Rows) error {
			var t pgTable
			if err := rows.Scan(&t.Schema, &t.Name, &t.Kind, &t.IsPartition); err != nil {
				return err
			}
			cat.Tables = append(cat.Tables, t)
			return nil
		}); err != nil {
		return nil, err
	}

	// 1b. partition child counts per parent — a FACT, not an enumeration
	if err := q(`SELECT n.nspname, c.relname, count(i.inhrelid)::int
	   FROM pg_partitioned_table pt
	   JOIN pg_class c ON c.oid = pt.partrelid
	   JOIN pg_namespace n ON n.oid = c.relnamespace
	   LEFT JOIN pg_inherits i ON i.inhparent = c.oid
	   WHERE `+pgSchemaFilter+inc+`
	   GROUP BY 1, 2`,
		func(rows pgx.Rows) error {
			var s, t string
			var cnt int
			if err := rows.Scan(&s, &t, &cnt); err != nil {
				return err
			}
			cat.Partitions[s+"."+t] = cnt
			return nil
		}); err != nil {
		return nil, err
	}

	// 2. columns — one query for everything
	if err := q(`SELECT n.nspname, c.relname, a.attname,
	          format_type(a.atttypid, a.atttypmod),
	          NOT a.attnotnull,
	          COALESCE(pg_get_expr(ad.adbin, ad.adrelid), '')
	   FROM pg_attribute a
	   JOIN pg_class c ON c.oid = a.attrelid
	   JOIN pg_namespace n ON n.oid = c.relnamespace
	   LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
	   WHERE a.attnum > 0 AND NOT a.attisdropped
	     AND c.relkind IN ('r','p','v','m') AND `+pgSchemaFilter+pgChildFilter+inc,
		func(rows pgx.Rows) error {
			var col pgColumn
			if err := rows.Scan(&col.Schema, &col.Table, &col.Name, &col.Type, &col.Nullable, &col.Default); err != nil {
				return err
			}
			cat.Columns = append(cat.Columns, col)
			return nil
		}); err != nil {
		return nil, err
	}

	// 3. primary keys
	if err := q(`SELECT n.nspname, c.relname, a.attname
	   FROM pg_index i
	   JOIN pg_class c ON c.oid = i.indrelid
	   JOIN pg_namespace n ON n.oid = c.relnamespace
	   JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY(i.indkey)
	   WHERE i.indisprimary AND `+pgSchemaFilter+pgChildFilter+inc,
		func(rows pgx.Rows) error {
			var pk pgPK
			if err := rows.Scan(&pk.Schema, &pk.Table, &pk.Column); err != nil {
				return err
			}
			cat.PKs = append(cat.PKs, pk)
			return nil
		}); err != nil {
		return nil, err
	}

	// 4. foreign keys with referenced table/column
	if err := q(`SELECT con.conname, n.nspname, c.relname, a.attname,
	          fn.nspname, fc.relname, fa.attname
	   FROM pg_constraint con
	   JOIN pg_class c ON c.oid = con.conrelid
	   JOIN pg_namespace n ON n.oid = c.relnamespace
	   JOIN pg_class fc ON fc.oid = con.confrelid
	   JOIN pg_namespace fn ON fn.oid = fc.relnamespace
	   JOIN LATERAL unnest(con.conkey, con.confkey) AS k(att, fatt) ON true
	   JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.att
	   JOIN pg_attribute fa ON fa.attrelid = con.confrelid AND fa.attnum = k.fatt
	   WHERE con.contype = 'f' AND `+pgSchemaFilter+pgChildFilter+inc,
		func(rows pgx.Rows) error {
			var fk pgFK
			if err := rows.Scan(&fk.Name, &fk.Schema, &fk.Table, &fk.Column,
				&fk.RefSchema, &fk.RefTable, &fk.RefColumn); err != nil {
				return err
			}
			cat.FKs = append(cat.FKs, fk)
			return nil
		}); err != nil {
		return nil, err
	}

	// 5. indexes (count + names as facts on the table node)
	if err := q(`SELECT i.schemaname, i.tablename, i.indexname
	   FROM pg_indexes i
	   JOIN pg_class c ON c.relname = i.tablename
	   JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = i.schemaname
	   WHERE NOT c.relispartition
	     AND i.schemaname NOT IN ('pg_catalog','information_schema')
	     AND i.schemaname NOT LIKE 'pg\_%'
	     AND i.schemaname NOT LIKE '\_timescaledb\_%'`+incIdx,
		func(rows pgx.Rows) error {
			var ix pgIndex
			if err := rows.Scan(&ix.Schema, &ix.Table, &ix.Name); err != nil {
				return err
			}
			cat.Indexes = append(cat.Indexes, ix)
			return nil
		}); err != nil {
		return nil, err
	}

	// 6. view definitions (snippet capped in buildPGBatch)
	if err := q(`SELECT n.nspname, c.relname, pg_get_viewdef(c.oid, true)
	   FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	   WHERE c.relkind IN ('v','m') AND `+pgSchemaFilter+inc,
		func(rows pgx.Rows) error {
			var v pgView
			if err := rows.Scan(&v.Schema, &v.Name, &v.Definition); err != nil {
				return err
			}
			cat.Views = append(cat.Views, v)
			return nil
		}); err != nil {
		return nil, err
	}

	// 7. comments (table- and column-level)
	if err := q(`SELECT n.nspname, c.relname, COALESCE(a.attname,''), d.description
	   FROM pg_description d
	   JOIN pg_class c ON c.oid = d.objoid AND d.classoid = 'pg_class'::regclass
	   JOIN pg_namespace n ON n.oid = c.relnamespace
	   LEFT JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = d.objsubid AND d.objsubid > 0
	   WHERE `+pgSchemaFilter+pgChildFilter+inc,
		func(rows pgx.Rows) error {
			var cm pgComment
			if err := rows.Scan(&cm.Schema, &cm.Table, &cm.Column, &cm.Text); err != nil {
				return err
			}
			cat.Comments = append(cat.Comments, cm)
			return nil
		}); err != nil {
		return nil, err
	}
	return cat, nil
}

// ---- url plumbing (textual — never net/url.Parse; see entry.go doctrine) ----

// popPGSchemasParam extracts and REMOVES the ctx-optimize-only ?schemas=a,b
// filter (libpq/pgx would reject an unknown connection option). Returned
// schema names are sorted; the remaining URL is passed to the driver verbatim.
func popPGSchemasParam(raw string) (include []string, connURL string) {
	q := strings.Index(raw, "?")
	if q < 0 {
		return nil, raw
	}
	base, query := raw[:q], raw[q+1:]
	frag := ""
	if h := strings.Index(query, "#"); h >= 0 {
		frag, query = query[h:], query[:h]
	}
	var kept []string
	for _, kv := range strings.Split(query, "&") {
		k, v, _ := strings.Cut(kv, "=")
		if strings.EqualFold(k, "schemas") {
			for _, s := range strings.Split(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					include = append(include, s)
				}
			}
			continue
		}
		kept = append(kept, kv)
	}
	sort.Strings(include)
	if len(kept) == 0 {
		return include, base + frag
	}
	return include, base + "?" + strings.Join(kept, "&") + frag
}

// pgHost returns the sanitized authority (userinfo stripped, ports and
// multi-host kept verbatim) — the only URL fragment allowed in ids or errors.
func pgHost(raw string) (string, error) {
	s, ok := sources.Sanitize(raw)
	if !ok {
		return "", fmt.Errorf("postgres: source value defies parsing — check the URL form (adapters help postgres)")
	}
	i := strings.Index(s, "://")
	if i < 0 {
		return "", fmt.Errorf("postgres: not a URL")
	}
	host := s[i+3:]
	if e := strings.IndexAny(host, "/?#"); e >= 0 {
		host = host[:e]
	}
	if host == "" {
		return "", fmt.Errorf("postgres: missing host in source URL")
	}
	return host, nil
}

// pgSecrets lists the substrings that must never survive into an error: the
// raw URL, its userinfo, and the password alone.
func pgSecrets(raw string) []string {
	out := []string{raw}
	if i := strings.Index(raw, "://"); i >= 0 {
		rest := raw[i+3:]
		auth := rest
		if e := strings.IndexAny(rest, "/?#"); e >= 0 {
			auth = rest[:e]
		}
		if at := strings.LastIndex(auth, "@"); at >= 0 {
			ui := auth[:at]
			out = append(out, ui)
			if _, pass, ok := strings.Cut(ui, ":"); ok && pass != "" {
				out = append(out, pass)
			}
		}
	}
	return out
}

// pgWrap wraps a driver error with scheme+host only, literal-replacing every
// secret substring first — a *url.Error or driver echo can never leak.
func pgWrap(host, stage string, err error, secrets []string) error {
	msg := err.Error()
	for _, s := range secrets {
		if s != "" {
			msg = strings.ReplaceAll(msg, s, "***")
		}
	}
	return fmt.Errorf("postgres %s: %s: %s", host, stage, msg)
}
