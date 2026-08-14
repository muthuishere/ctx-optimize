package app

// cmdSearch — `ctx-optimize search <regex>` (ADR 2026-08-13
// boundary-model-and-defaults, D4): the cross-OS literal sweep. Ground truth
// for boundary rules and the agent-facing literal search must not assume rg
// or POSIX grep — grep does not exist on Windows, and windows is a release
// target. Same RE2 engine as the boundary rules, same file set as the
// extractor (internal/search mirrors the code producer's walk).

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/search"
)

func cmdSearch(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if len(f.args) != 1 {
		return fmt.Errorf("usage: search <regex> [--ext .go,.ts] [--path dir/] [--count] [--files] [--json]")
	}
	re, err := regexp.Compile(f.args[0])
	if err != nil {
		return fmt.Errorf("bad regex (Go RE2 syntax — the same engine boundary rules use): %w", err)
	}
	var exts []string
	if v := f.strs["ext"]; v != "" {
		for _, e := range strings.Split(v, ",") {
			if e = strings.TrimSpace(e); e != "" {
				if !strings.HasPrefix(e, ".") {
					e = "." + e
				}
				exts = append(exts, e)
			}
		}
	}
	matches, err := search.Run(".", re, search.Options{Exts: exts, Path: f.strs["path"]})
	if err != nil {
		return err
	}

	switch {
	case f.bools["count"]:
		// One number — what a verified.ground_truth block records.
		fmt.Fprintln(stdout, len(matches))
	case f.bools["files"]:
		seen := map[string]bool{}
		var files []string
		for _, m := range matches {
			if !seen[m.File] {
				seen[m.File] = true
				files = append(files, m.File)
			}
		}
		sort.Strings(files)
		for _, fp := range files {
			fmt.Fprintln(stdout, fp)
		}
	case f.bools["json"]:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", " ")
		return enc.Encode(matches)
	default:
		for _, m := range matches {
			fmt.Fprintf(stdout, "%s:%d: %s\n", m.File, m.Line, m.Text)
		}
	}
	return nil
}
