package app

import (
	"fmt"
	"io"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/analyze"
)

// cmdDrift — the boundary-surface disagreement report (ADR 2026-08-13 D6,
// verb half). Findings are EXTRACTED×EXTRACTED only; everything softer is
// listed under OBSERVED, never accused. --strict exits nonzero when findings
// exist, so CI can hold the boundary surface still.
func cmdDrift(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	nodes, edges, err := loadGraph(f)
	if err != nil {
		return err
	}
	t0 := time.Now()
	cw := &countingWriter{w: stdout}
	st, _ := openStore(f)
	defer func() { served(st, "drift", "", len(nodes), cw, t0) }()

	r := analyze.Drift(nodes, edges)
	if f.bools["json"] {
		if err := emit(cw, r); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(cw, "drift: %d ports in %d contracts — %d finding(s), %d observation(s)\n",
			r.Ports, r.Groups, len(r.Findings), len(r.Observations))
		if len(r.Findings) > 0 {
			fmt.Fprintf(cw, "\nFINDINGS (every leg EXTRACTED):\n")
			printDrift(cw, r.Findings)
		}
		if len(r.Observations) > 0 {
			fmt.Fprintf(cw, "\nOBSERVED (an INFERRED/AMBIGUOUS leg, or pending normalization — listed, not accused):\n")
			printDrift(cw, r.Observations)
		}
		if r.Ports == 0 {
			fmt.Fprintf(cw, "(no port nodes — gather with the boundaries lane first: ctx-optimize add .)\n")
		}
	}
	if f.bools["strict"] && len(r.Findings) > 0 {
		return fmt.Errorf("drift --strict: %d finding(s)", len(r.Findings))
	}
	return nil
}

func printDrift(w io.Writer, fs []analyze.DriftFinding) {
	for _, d := range fs {
		fmt.Fprintf(w, "  [%s] %s %s — %s\n", d.Kind, d.Transport, d.Identifier, d.Detail)
		if len(d.Identifiers) > 1 {
			fmt.Fprintf(w, "      spellings:")
			for _, id := range d.Identifiers {
				fmt.Fprintf(w, " %q", id)
			}
			fmt.Fprintln(w)
		}
		cite := func(role string, sites []analyze.DriftSite) {
			for _, s := range sites {
				loc := s.File
				if s.Site != "" {
					loc = s.Site
				}
				fmt.Fprintf(w, "      %s %s [%s", role, loc, s.Confidence)
				if s.Rule != "" {
					fmt.Fprintf(w, ", rule %s", s.Rule)
				}
				fmt.Fprintln(w, "]")
			}
		}
		cite("provides", d.Providers)
		cite("consumes", d.Consumers)
	}
}
