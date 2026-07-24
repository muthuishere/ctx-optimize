package app

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/graphfilter"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// The native, portable, jq-free filter surface (ADR 2026-07-24). nodes/edges/
// deps stream the loaded (federated) graph through the shared graphfilter core
// — one in-process pass, no serialization, no subprocess. Table by default;
// --json (array) / --ndjson (one record per line) for machines; --select for
// projection. loadGraph federates across all modules at a monorepo root.

// narrowQuery applies the shared predicate to the graph BEFORE query ranking
// (pre-rank narrowing, ADR 2026-07-24): "top react files" ranks WITHIN
// --kind file, so the budget isn't spent on higher-scoring other kinds.
func narrowQuery(f *flags, nodes []schema.Node, edges []schema.Edge) ([]schema.Node, []schema.Edge, error) {
	pred, err := graphfilter.ParsePred(f.strs)
	if err != nil {
		return nil, nil, err
	}
	n, e := graphfilter.Apply(nodes, edges, pred)
	return n, e, nil
}

// cmdNodes lists/filters nodes by kind/file-type/id-prefix/label/scope/where.
func cmdNodes(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	pred, err := graphfilter.ParsePred(f.strs)
	if err != nil {
		return err
	}
	nodes, _, err := loadGraph(f)
	if err != nil {
		return err
	}
	t0 := time.Now()
	cw := &countingWriter{w: stdout}
	st, _ := openStore(f)
	defer func() { served(st, "nodes", f.strs["kind"], len(nodes), cw, t0) }()

	out := nodes[:0:0]
	for _, n := range nodes {
		if pred.MatchNode(n) {
			out = append(out, n)
		}
	}
	graphfilter.SortNodes(out)
	return emitNodes(cw, out, graphfilter.Fields(f.strs["select"]), f.bools["ndjson"], f.bools["json"])
}

// cmdEdges lists/filters edges by relation/confidence/from/to/id-prefix/where.
func cmdEdges(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	pred, err := graphfilter.ParsePred(f.strs)
	if err != nil {
		return err
	}
	_, edges, err := loadGraph(f)
	if err != nil {
		return err
	}
	t0 := time.Now()
	cw := &countingWriter{w: stdout}
	st, _ := openStore(f)
	defer func() { served(st, "edges", f.strs["relation"], len(edges), cw, t0) }()

	out := edges[:0:0]
	for _, e := range edges {
		if pred.MatchEdge(e) {
			out = append(out, e)
		}
	}
	graphfilter.SortEdges(out)
	return emitEdges(cw, out, graphfilter.Fields(f.strs["select"]), f.bools["ndjson"], f.bools["json"])
}

// cmdDeps is `nodes --kind dependency` with dependency-shaped ergonomics:
// --scope narrows by runtime/dev/…, --importers appends the files that import
// each dependency (file --imports--> module:// --resolves_to--> dep:).
func cmdDeps(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	pred, err := graphfilter.ParsePred(f.strs)
	if err != nil {
		return err
	}
	pred.Kinds = []string{"dependency"} // the alias's whole point
	nodes, edges, err := loadGraph(f)
	if err != nil {
		return err
	}
	t0 := time.Now()
	cw := &countingWriter{w: stdout}
	st, _ := openStore(f)
	defer func() { served(st, "deps", f.strs["scope"], len(nodes), cw, t0) }()

	var deps []schema.Node
	for _, n := range nodes {
		if pred.MatchNode(n) {
			deps = append(deps, n)
		}
	}
	graphfilter.SortNodes(deps)

	// The deps verb always surfaces `scopes` at the top level (it is
	// dependency-specific — no need to dig into metadata). --importers adds the
	// two-hop join to importing files (module --resolves_to--> dep, then
	// file --imports--> module).
	importers := map[string][]string{}
	if f.bools["importers"] {
		depSet := map[string]bool{}
		for _, d := range deps {
			depSet[d.ID] = true
		}
		moduleToDep := map[string]string{} // module:// id -> dep id
		for _, e := range edges {
			if e.Relation == "resolves_to" && depSet[e.Target] {
				moduleToDep[e.Source] = e.Target
			}
		}
		seen := map[string]bool{}
		for _, e := range edges {
			if e.Relation != "imports" {
				continue
			}
			if dep, ok := moduleToDep[e.Target]; ok {
				key := dep + "\x00" + e.Source
				if seen[key] {
					continue
				}
				seen[key] = true
				importers[dep] = append(importers[dep], e.Source)
			}
		}
	}
	return emitDepsWithImporters(cw, deps, importers, f.bools["importers"], f.bools["ndjson"], f.bools["json"])
}

// ---- emitters ----

func emitNodes(w io.Writer, ns []schema.Node, fields []string, ndjson, jsonArr bool) error {
	switch {
	case ndjson:
		enc := json.NewEncoder(w)
		for _, n := range ns {
			var rec any = n
			if len(fields) > 0 {
				rec = graphfilter.ProjectNode(n, fields)
			}
			if err := enc.Encode(rec); err != nil {
				return err
			}
		}
		return nil
	case jsonArr:
		if len(fields) > 0 {
			recs := make([]map[string]any, len(ns))
			for i, n := range ns {
				recs[i] = graphfilter.ProjectNode(n, fields)
			}
			return emit(w, recs)
		}
		return emit(w, ns)
	default:
		for _, n := range ns {
			loc := ""
			if n.Location != "" {
				loc = "  " + n.Location
			}
			fmt.Fprintf(w, "%s  [%s]  %s%s\n", n.Label, n.Kind, n.ID, loc)
		}
		fmt.Fprintf(w, "(%d nodes)\n", len(ns))
		return nil
	}
}

func emitEdges(w io.Writer, es []schema.Edge, fields []string, ndjson, jsonArr bool) error {
	switch {
	case ndjson:
		enc := json.NewEncoder(w)
		for _, e := range es {
			var rec any = e
			if len(fields) > 0 {
				rec = graphfilter.ProjectEdge(e, fields)
			}
			if err := enc.Encode(rec); err != nil {
				return err
			}
		}
		return nil
	case jsonArr:
		if len(fields) > 0 {
			recs := make([]map[string]any, len(es))
			for i, e := range es {
				recs[i] = graphfilter.ProjectEdge(e, fields)
			}
			return emit(w, recs)
		}
		return emit(w, es)
	default:
		for _, e := range es {
			fmt.Fprintf(w, "%s  --%s-->  %s  [%s]\n", e.Source, e.Relation, e.Target, e.Confidence)
		}
		fmt.Fprintf(w, "(%d edges)\n", len(es))
		return nil
	}
}

func emitDepsWithImporters(w io.Writer, deps []schema.Node, importers map[string][]string, withImporters, ndjson, jsonArr bool) error {
	type depOut struct {
		ID        string    `json:"id"`
		Label     string    `json:"label"`
		Scopes    string    `json:"scopes,omitempty"`
		Ecosystem string    `json:"ecosystem,omitempty"`
		Importers *[]string `json:"importers,omitempty"`
	}
	rows := make([]depOut, len(deps))
	for i, d := range deps {
		rows[i] = depOut{
			ID: d.ID, Label: d.Label,
			Scopes:    d.Metadata["scopes"],
			Ecosystem: d.Metadata["ecosystem"],
		}
		if withImporters {
			imps := importers[d.ID]
			if imps == nil {
				imps = []string{}
			}
			rows[i].Importers = &imps
		}
	}
	switch {
	case ndjson:
		enc := json.NewEncoder(w)
		for _, r := range rows {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		return nil
	case jsonArr:
		return emit(w, rows)
	default:
		for _, r := range rows {
			scope := r.Scopes
			if scope == "" {
				scope = "-"
			}
			if r.Importers != nil {
				fmt.Fprintf(w, "%s  [%s]  (%d importers)\n", r.Label, scope, len(*r.Importers))
				for _, imp := range *r.Importers {
					fmt.Fprintf(w, "    %s\n", imp)
				}
			} else {
				fmt.Fprintf(w, "%s  [%s]  %s\n", r.Label, scope, r.ID)
			}
		}
		fmt.Fprintf(w, "(%d dependencies)\n", len(rows))
		return nil
	}
}
