package app

import (
	"os/exec"
	"strconv"
	"strings"
)

// gitHead reads a working tree's current HEAD sha and its committer unix time.
// It is best-effort reflection, NOT intelligence: if git is absent or dir is
// not a repo, ok is false and callers treat freshness as "unknown" — never an
// error. Read-only; touches nothing. No secret ever crosses here.
func gitHead(dir string) (head string, unixTime int64, ok bool) {
	head = strings.TrimSpace(runGit(dir, "rev-parse", "HEAD"))
	if head == "" {
		return "", 0, false
	}
	if t := strings.TrimSpace(runGit(dir, "log", "-1", "--format=%ct")); t != "" {
		unixTime, _ = strconv.ParseInt(t, 10, 64)
	}
	return head, unixTime, true
}

func runGit(dir string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output() // stderr discarded: a non-repo/failure is just "unknown"
	if err != nil {
		return ""
	}
	return string(out)
}
