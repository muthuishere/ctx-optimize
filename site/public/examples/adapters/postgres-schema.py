#!/usr/bin/env python3
"""Example adapter: PostgreSQL schema -> ctx-optimize batch JSON.

Drop this file into your repo's .ctxoptimize/adapters/ and every
`ctx-optimize add` will gather your database schema into the same graph as
your code and docs: one node per table (columns in metadata), one
`references` edge per foreign key.

Connection comes from $DATABASE_URL (standard postgres:// URL). The value is
read from the environment at run time and never written or printed — the
house rule for credentials. No driver needed: uses the `psql` CLI.

If $DATABASE_URL is unset the adapter emits an empty batch and says so on
stderr, so repos without a database still gather cleanly.
"""
import json
import os
import subprocess
import sys

PRODUCER = "postgres-schema"


def emit(nodes, edges):
    json.dump({"producer": PRODUCER, "nodes": nodes, "edges": edges}, sys.stdout)
    sys.stdout.write("\n")


url = os.environ.get("DATABASE_URL")
if not url:
    print("postgres-schema: DATABASE_URL not set — emitting empty batch", file=sys.stderr)
    emit([], [])
    sys.exit(0)


def q(sql):
    out = subprocess.run(
        ["psql", url, "-At", "-F", "\t", "-c", sql],
        capture_output=True, text=True, check=True,
    ).stdout
    return [line.split("\t") for line in out.strip().splitlines() if line]


db = q("select current_database()")[0][0]

columns = {}
for table, col, dtype in q(
    "select table_name, column_name, data_type from information_schema.columns "
    "where table_schema='public' order by table_name, ordinal_position"
):
    columns.setdefault(table, []).append(f"{col}:{dtype}")

nodes = []
for table, in q(
    "select table_name from information_schema.tables "
    "where table_schema='public' and table_type='BASE TABLE' order by table_name"
):
    nodes.append({
        "id": f"pg://{db}/{table}",
        "label": table,
        "kind": "table",
        "file_type": "schema",
        "source": f"pg://{db}/{table}",
        "metadata": {"columns": ", ".join(columns.get(table, []))},
    })

edges = []
for src, dst, cons in q(
    "select tc.table_name, ccu.table_name, tc.constraint_name "
    "from information_schema.table_constraints tc "
    "join information_schema.constraint_column_usage ccu using (constraint_name, table_schema) "
    "where tc.constraint_type='FOREIGN KEY' and tc.table_schema='public' "
    "order by 1, 2"
):
    edges.append({
        "source": f"pg://{db}/{src}",
        "target": f"pg://{db}/{dst}",
        "relation": "references",
        "confidence": "EXTRACTED",
        "weight": 1,
        "metadata": {"constraint": cons},
    })

emit(nodes, edges)
