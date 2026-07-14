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

func run(dir string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output() // stderr discarded: a non-repo/failure is just "unknown"
	if err != nil {
		return ""
	}
	return string(out)
}
