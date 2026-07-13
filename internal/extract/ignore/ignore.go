// Package ignore answers "is this path gitignored?" with git's OWN
// semantics — nested .gitignore files, negations, global excludes — by
// asking git once per gather instead of reimplementing the spec.
// Best-effort by design: no git binary or not a repo → nil matcher, and the
// extractors fall back to their built-in prune lists alone. The lesson is
// graphify #1363: a gather that ignores .gitignore will eventually index a
// neutrally-named secret (prod-dump.sql) into artifacts people commit.
package ignore

import (
	"bufio"
	"bytes"
	"os/exec"
	"strings"
)

// Matcher reports whether a root-relative slash path is gitignored.
type Matcher func(rel string) bool

// New builds a Matcher for root. Returns nil when git is unavailable, root
// is not a work tree, or nothing is ignored — callers treat nil as
// "no extra filtering".
func New(root string) Matcher {
	out, err := exec.Command("git", "-C", root, "ls-files",
		"--others", "--ignored", "--exclude-standard", "--directory", "-z").Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	dirs := map[string]bool{}
	files := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	sc.Split(splitNul)
	for sc.Scan() {
		p := sc.Text()
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "/") {
			dirs[strings.TrimSuffix(p, "/")] = true
		} else {
			files[p] = true
		}
	}
	if len(dirs) == 0 && len(files) == 0 {
		return nil
	}
	return func(rel string) bool {
		if files[rel] || dirs[rel] {
			return true
		}
		// inside an ignored directory
		for i := len(rel) - 1; i > 0; i-- {
			if rel[i] == '/' {
				if dirs[rel[:i]] {
					return true
				}
			}
		}
		return false
	}
}

func splitNul(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
