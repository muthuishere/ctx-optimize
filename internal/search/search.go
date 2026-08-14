// Package search is the cross-OS literal sweep (ADR 2026-08-13
// boundary-model-and-defaults, D4). Ground truth for boundary rules must not
// assume rg or POSIX grep — grep does not exist on Windows, and Windows is a
// release target — so the binary carries its own.
//
// Two properties matter more than speed:
//
//   - SAME FILE SET as the extractor: the walk below mirrors the code
//     producer's (gitignore via git itself, the same skip-dir names, the same
//     size cap). A vendored corpus that the extractor never reads must never
//     appear in a ground-truth count either — the spike's false positive
//     (benchmarks/corpus-flask reported as egress) was exactly a file-set
//     disagreement between two tools.
//   - SAME REGEX ENGINE as the boundary rules (Go RE2): a rule and its
//     verification can never disagree about regex semantics.
//
// Deterministic output (sorted by file, then line) per the store doctrine.
package search

import (
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/ignore"
)

// maxFileBytes mirrors the code producer's cap: a file the extractor refuses
// to parse must not sneak into a ground-truth count.
const maxFileBytes = 2 << 20

// Match is one matching line. Text is the trimmed line, capped — evidence for
// a human to eyeball, not a data transport.
type Match struct {
	File string `json:"file"` // slash-separated, relative to root
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Options narrows the sweep. Zero value = every text file under root.
type Options struct {
	Exts []string // e.g. [".go", ".ts"]; empty = all
	Path string   // slash-relative prefix, e.g. "internal/"; empty = all
}

// Run sweeps root for re. It never follows symlinks and silently skips
// unreadable or binary files — a sweep is a count, not an audit of the disk.
func Run(root string, re *regexp.Regexp, o Options) ([]Match, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	extOK := func(name string) bool {
		if len(o.Exts) == 0 {
			return true
		}
		for _, e := range o.Exts {
			if strings.HasSuffix(name, e) {
				return true
			}
		}
		return false
	}
	ignored := ignore.New(absRoot) // .gitignore semantics via git itself; nil = no git

	var files []string
	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(absRoot, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if ignored != nil && rel != "." && ignored(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			// The code producer's skip list, verbatim — same file set.
			if path != absRoot && (strings.HasPrefix(name, ".") || name == "node_modules" ||
				name == "vendor" || name == "target" || name == "dist" || name == "build" ||
				strings.HasSuffix(name, "-out")) {
				return filepath.SkipDir
			}
			return nil
		}
		if o.Path != "" && !strings.HasPrefix(rel, o.Path) {
			return nil
		}
		if !extOK(name) {
			return nil
		}
		if info, ierr := d.Info(); ierr != nil || info.Size() > maxFileBytes || !info.Mode().IsRegular() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)

	var out []Match
	for _, f := range files {
		matches, merr := sweepFile(absRoot, f, re)
		if merr != nil {
			continue // unreadable mid-walk: skip, same as the extractor
		}
		out = append(out, matches...)
	}
	return out, nil
}

func sweepFile(root, path string, re *regexp.Regexp) ([]Match, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)

	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var out []Match
	line := 0
	for sc.Scan() {
		b := sc.Bytes()
		line++
		if line == 1 && bytes.IndexByte(b, 0) >= 0 {
			return nil, nil // binary: a NUL on the first line, not a text file
		}
		if !re.Match(b) {
			continue
		}
		t := strings.TrimSpace(string(b))
		if len(t) > 200 {
			t = t[:199] + "…"
		}
		out = append(out, Match{File: rel, Line: line, Text: t})
	}
	// A scanner error (line too long, read failure) ends the file, not the
	// sweep — matches already collected stand.
	return out, nil
}
