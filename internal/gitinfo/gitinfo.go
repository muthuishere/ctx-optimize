// Package gitinfo is the one place that shells out to git for read-only
// reflection. It is best-effort by design: if git is absent or the dir is not
// a repo, callers get ok=false and treat freshness as "unknown" — never an
// error. Touches nothing, prints nothing, no secret ever crosses here.
package gitinfo

import (
	"os/exec"
	"strconv"
	"strings"
)

// Head reads a working tree's current HEAD sha and its committer unix time.
func Head(dir string) (head string, unixTime int64, ok bool) {
	head = strings.TrimSpace(run(dir, "rev-parse", "HEAD"))
	if head == "" {
		return "", 0, false
	}
	if t := strings.TrimSpace(run(dir, "log", "-1", "--format=%ct")); t != "" {
		unixTime, _ = strconv.ParseInt(t, 10, 64)
	}
	return head, unixTime, true
}

// Remote reads the origin remote URL and the current branch name — the two
// facts the dashboard needs to build a GitHub blob link. ok=false when there
// is no origin (or git is absent). branch is "" on a detached HEAD, which the
// caller treats as "no per-branch link".
func Remote(dir string) (origin, branch string, ok bool) {
	origin = strings.TrimSpace(run(dir, "remote", "get-url", "origin"))
	if origin == "" {
		return "", "", false
	}
	branch = strings.TrimSpace(run(dir, "rev-parse", "--abbrev-ref", "HEAD"))
	if branch == "HEAD" { // detached — no branch to anchor a blob URL on
		branch = ""
	}
	return origin, branch, true
}

// FileChangedSince reports whether relPath differs between the given commit
// and the CURRENT working tree — committed, staged, and unstaged changes all
// count. ok=false when git or the sha is unavailable, so callers report
// drift "unknown" instead of a false verdict (verify's drift check, ADR
// 2026-07-16-verify-verb).
func FileChangedSince(dir, sinceSHA, relPath string) (changed, ok bool) {
	if sinceSHA == "" {
		return false, false
	}
	if strings.TrimSpace(run(dir, "rev-parse", "--verify", sinceSHA+"^{commit}")) == "" {
		return false, false
	}
	// Untracked files never appear in diff — "unchanged" would be a false
	// verdict for a file git isn't watching; report unknown instead.
	if strings.TrimSpace(run(dir, "ls-files", "--", relPath)) == "" {
		return false, false
	}
	// A failed diff (e.g. bad path) and "no change" both come back empty;
	// the sha was just verified, so empty here means unchanged.
	return strings.TrimSpace(run(dir, "diff", "--name-only", sinceSHA, "--", relPath)) != "", true
}

func run(dir string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output() // stderr discarded: a non-repo/failure is just "unknown"
	if err != nil {
		return ""
	}
	return string(out)
}
