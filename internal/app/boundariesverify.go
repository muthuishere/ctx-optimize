package app

// cmdBoundaries — `ctx-optimize boundaries verify` (ADR 2026-08-13
// boundary-authoring, D3): the standing check that holds every boundary rule
// to its own evidence.
//
// It reports the LOCAL recall of each rule against its recorded independent
// ground truth, compared to a committed local floor
// (.ctxoptimize/boundaries-baseline.json, written by --record). The number in
// a rule's shipped `verified` block was measured on corpora that are not on
// this machine, so it is printed as provenance beside the local figure and
// never as something this run reproduced. See internal/boundaries/verify.go
// for the full statement of what this can and cannot prove.

import (
	"fmt"
	"io"

	"github.com/muthuishere/ctx-optimize/internal/boundaries"
)

func cmdBoundaries(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	sub := "verify"
	if len(f.args) > 0 {
		sub = f.args[0]
	}
	if sub != "verify" {
		return fmt.Errorf("usage: boundaries verify [--json] [--strict] [--record]")
	}

	root := "."
	if p := f.strs["path"]; p != "" {
		root = p
	}
	rep, err := boundaries.Verify(root)
	if err != nil {
		return err
	}

	if f.bools["record"] {
		p, werr := boundaries.RecordBaseline(root, rep)
		if werr != nil {
			return werr
		}
		fmt.Fprintf(stdout, "recorded local recall floor for %d exercised rule(s) → %s\n", rep.Exercised, p)
		fmt.Fprintln(stdout, "commit it: the floor is reviewed like a golden snapshot, and only moves up.")
		return nil
	}

	if f.bools["json"] {
		if err := emit(stdout, rep); err != nil {
			return err
		}
	} else {
		printVerify(stdout, rep)
	}
	if f.bools["strict"] && rep.Regressed > 0 {
		return fmt.Errorf("boundaries verify --strict: %d rule(s) below their recorded floor", rep.Regressed)
	}
	return nil
}

func printVerify(w io.Writer, rep *boundaries.Report) {
	fmt.Fprintf(w, "boundaries verify: %d rule(s) — %d exercised, %d no sites here, %d unverifiable · %d regressed\n",
		len(rep.Rules), rep.Exercised, rep.Unexercised, rep.Unverifiable, rep.Regressed)
	if rep.HasBaseline {
		fmt.Fprintf(w, "local floor: %s\n", rep.BaselinePath)
	} else {
		fmt.Fprintf(w, "local floor: none yet — record one with `boundaries verify --record` to make drift detectable\n")
	}
	fmt.Fprintln(w)

	for _, r := range rep.Rules {
		switch r.Status {
		case boundaries.StatusUnexercised:
			fmt.Fprintf(w, "  %-18s   no sites here      %s\n", r.Rule, r.Note)
			continue
		case boundaries.StatusUnverifiable:
			fmt.Fprintf(w, "  %-18s   UNVERIFIABLE       %s\n", r.Rule, r.Note)
			continue
		}
		mark := "ok"
		switch r.Status {
		case boundaries.StatusRegressed:
			mark = "⚠ REGRESSED"
		case boundaries.StatusImproved:
			mark = "↑ improved"
		}
		if r.BaselineRecall != nil {
			fmt.Fprintf(w, "  %-18s   recall %s → %s   %s   (%d/%d local)\n",
				r.Rule, boundaries.Pct(r.BaselineRecall), boundaries.Pct(r.Recall), mark, r.Matched, r.Expected)
		} else {
			fmt.Fprintf(w, "  %-18s   recall %s          %s   (%d/%d local, no floor yet)\n",
				r.Rule, boundaries.Pct(r.Recall), mark, r.Matched, r.Expected)
		}
		if r.ClaimedRecall != nil {
			fmt.Fprintf(w, "      shipped block: recall %s  %s  — a different population, not reproduced here\n",
				boundaries.Pct(r.ClaimedRecall), boundaries.CorporaNote(r.ClaimedOn))
		}
		if r.Extra > 0 {
			fmt.Fprintf(w, "      %d rule site(s) outside the ground truth — the sweep may be narrower than the rule\n", r.Extra)
		}
		if r.UnmatchedTotal > 0 {
			fmt.Fprintf(w, "      %d unmatched ground-truth site(s):\n", r.UnmatchedTotal)
			for _, s := range r.Unmatched {
				fmt.Fprintf(w, "        %s:%d  %s\n", s.File, s.Line, s.Text)
			}
			if r.UnmatchedTotal > len(r.Unmatched) {
				fmt.Fprintf(w, "        … %d more not shown (of %d)\n", r.UnmatchedTotal-len(r.Unmatched), r.UnmatchedTotal)
			}
		}
	}
}
