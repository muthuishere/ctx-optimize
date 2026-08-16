package app

// cmdBoundariesReport — `ctx-optimize boundaries` with no subcommand (ADR
// 2026-08-15-boundaries-verb): the system-context answer.
//
// The facts have existed since the boundary lane shipped; what was missing was
// a shape. `nodes --kind port` answers with an alphabetical dump led by
// whichever dynamic identifier sorts first, which is data rather than an
// answer. This renders the C4 system-context view instead: what this system
// CALLS OUT TO, what it EXPOSES, and which partners are inside the workspace.
//
// Two rules it must never break, both from ADR 15 D2:
//   - env var NAMES only, never a value. The identifier of a config.env port
//     IS the variable name; no value is ever read, stored or printed.
//   - never enumerate unbounded. Groups are budgeted like query's hits, and
//     the withheld count is always stated — silent truncation reads as "that
//     is everything".

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/analyze"
)

func cmdBoundariesReport(f *flags, stdout io.Writer) error {
	nodes, edges, err := loadGraph(f)
	if err != nil {
		return err
	}
	t0 := time.Now()
	cw := &countingWriter{w: stdout}
	st, _ := openStore(f)
	defer func() { served(st, "boundaries", "", len(nodes), cw, t0) }()

	opt := analyze.BoundaryOptions{
		Direction: f.strs["direction"],
		Transport: f.strs["transport"],
		OnlyExt:   f.bools["external"],
		OnlySens:  f.bools["sensitive"],
		All:       f.bools["all"],
	}
	switch opt.Direction {
	case "", "provides", "consumes":
	default:
		return fmt.Errorf("--direction must be provides or consumes, got %q", opt.Direction)
	}

	r := analyze.Boundaries(nodes, edges, opt)
	if f.bools["json"] {
		return emit(cw, r)
	}
	printBoundaries(cw, r)
	return nil
}

func printBoundaries(w io.Writer, r *analyze.BoundaryReport) {
	if r.Ports == 0 {
		fmt.Fprintln(w, "no port nodes in this store — gather with the boundaries lane first: ctx-optimize add .")
		return
	}
	hdr := fmt.Sprintf("%d ports", r.Ports)
	if n := len(r.Modules); n > 0 {
		hdr = fmt.Sprintf("%d modules, %s", n, hdr)
	}
	fmt.Fprintf(w, "boundaries: %s\n", hdr)

	section := func(title, note string, gs []analyze.BoundaryGroup) {
		if len(gs) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s %s\n", title, note)
		for _, g := range gs {
			fmt.Fprintf(w, "  %-14s %s\n", g.Transport, groupCounts(g))
			for _, e := range g.Entries {
				printBoundaryEntry(w, e)
			}
			if g.Withheld > 0 {
				fmt.Fprintf(w, "      … %d more not shown (of %d) — --all for the full list\n",
					g.Withheld, g.Total)
			}
		}
	}
	section("CONSUMES", "(what this system calls out to)", r.Consumes)
	section("PROVIDES", "(what this system exposes)", r.Provides)

	if r.DynamicTotal > 0 {
		fmt.Fprintf(w, "\nUNRESOLVED  %d port(s) carry a dynamic identifier — os.Getenv(varName) and\n", r.DynamicTotal)
		fmt.Fprintf(w, "            friends. The SITE is certain, the value is not; --all lists them.\n")
	}
}

// groupCounts is the line that makes the summary readable before any entry is:
// how many, how many stay inside the workspace, how many are secrets.
//
// There is no "N external" count. `scope` is emitted only when the join
// PROVES a port internal (ADR 2026-08-15-scope-join-broken); printing the
// remainder as external would restate the constant this ADR removed.
func groupCounts(g analyze.BoundaryGroup) string {
	parts := []string{fmt.Sprintf("%d", g.Total)}
	if g.Internal > 0 {
		parts = append(parts, fmt.Sprintf("%d internal", g.Internal))
	}
	if g.Sensitive > 0 {
		parts = append(parts, fmt.Sprintf("%d SENSITIVE", g.Sensitive))
	}
	if g.Dynamic > 0 {
		parts = append(parts, fmt.Sprintf("%d dynamic", g.Dynamic))
	}
	return strings.Join(parts, " · ")
}

func printBoundaryEntry(w io.Writer, e analyze.BoundaryEntry) {
	marks := ""
	if e.Sensitive {
		marks += " SECRET"
	}
	if e.Scope == "internal" {
		marks += " internal"
	}
	if e.Dynamic {
		marks += " dynamic"
	}
	tier := e.Tier
	if e.MixedTiers {
		tier += "+"
	}
	cite := e.Cite
	if e.Sites > 1 {
		cite = fmt.Sprintf("%s (+%d sites)", cite, e.Sites-1)
	}
	// Identifier and cite are repo-derived: a hostname or env-var name comes
	// from source, and a control byte there could overwrite this line
	// (safetext.go). Sanitize AFTER trunc so the width math sees real runes.
	fmt.Fprintf(w, "      %-38s %-10s%s  %s\n",
		analyze.SafeLine(trunc(e.Identifier, 38)), tier, marks, analyze.SafeLine(cite))
	if len(e.Modules) > 1 {
		fmt.Fprintf(w, "          modules: %s\n", analyze.SafeLine(strings.Join(e.Modules, ", ")))
	}
	// semconv keys ride to JSON always, but only print the ones that ADD
	// information: otel.server.address on an http port is the identifier
	// spelled twice, and a line that repeats itself is noise in a summary.
	if len(e.Otel) > 0 {
		keys := make([]string, 0, len(e.Otel))
		for k, v := range e.Otel {
			if v != e.Identifier {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		var kv []string
		for _, k := range keys {
			kv = append(kv, fmt.Sprintf("%s=%s", k, e.Otel[k]))
		}
		if len(kv) > 0 {
			fmt.Fprintf(w, "          %s\n", strings.Join(kv, " "))
		}
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 2 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
