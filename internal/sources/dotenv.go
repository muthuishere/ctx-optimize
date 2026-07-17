package sources

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ParseDotenv parses the validated dotenv subset (spike-env, 16-case table):
//   - KEY=VALUE per line; blank lines and #-comment lines skipped
//   - optional "export " prefix tolerated
//   - whitespace around '=' and around key/value trimmed
//   - single or double quotes stripped (matched pair only); no interpolation,
//     no escapes; inside quotes '#' and '=' are literal
//   - unquoted values: '#' preceded by whitespace starts an inline comment
//     (bash / docker-compose / python-dotenv majority rule)
//   - CRLF and a UTF-8 BOM tolerated; lines without '=' ignored
func ParseDotenv(body string) map[string]string {
	out := map[string]string{}
	body = strings.TrimPrefix(body, "\uFEFF")
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "export "); ok {
			line = strings.TrimSpace(rest)
		}
		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue // no '=' or empty key
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" || strings.ContainsAny(key, " \t") {
			continue // malformed key
		}
		val := strings.TrimSpace(line[eq+1:])
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1] // matched quotes: strip, contents literal
		} else if i := indexInlineComment(val); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		out[key] = val
	}
	return out
}

func indexInlineComment(s string) int {
	for i := 1; i < len(s); i++ {
		if s[i] == '#' && (s[i-1] == ' ' || s[i-1] == '\t') {
			return i
		}
	}
	return -1
}

// Origin labels for Resolver lookups — printed name-only in summaries (M4).
const (
	OriginEnv       = "env"
	OriginRootEnv   = ".env"
	OriginGlobalEnv = "~/.config/ctx-optimize/.env"
)

// GlobalEnvPath is the machine-global dotenv — the last ladder rung, for
// credentials shared across every repo on this machine (a personal dev DB,
// a MinIO on localhost). Lives outside any repo, so it can never be
// committed. Overridable for tests via CTX_OPTIMIZE_GLOBAL_ENV.
func GlobalEnvPath() string {
	if p := os.Getenv("CTX_OPTIMIZE_GLOBAL_ENV"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ctx-optimize", ".env")
}

// Resolver resolves env-var names through the ladder: process environment →
// repo-root .env → ~/.config/ctx-optimize/.env (specific over general; the
// real env always wins for CI/prod, the machine-global file is the
// shared-across-repos fallback and lives outside any repo so it can never
// be committed). The files are read once, in memory, only while resolving
// sources — values are never copied elsewhere or printed.
type Resolver struct {
	repo   string
	root   map[string]string
	global map[string]string
}

// NewResolver loads the dotenv files (absent → empty maps).
func NewResolver(repo string) *Resolver {
	r := &Resolver{repo: repo, root: map[string]string{}, global: map[string]string{}}
	if data, err := os.ReadFile(filepath.Join(repo, ".env")); err == nil {
		r.root = ParseDotenv(string(data))
	}
	if p := GlobalEnvPath(); p != "" {
		if data, err := os.ReadFile(p); err == nil {
			r.global = ParseDotenv(string(data))
		}
	}
	return r
}

// Lookup resolves one name; origin names where the value came from.
func (r *Resolver) Lookup(name string) (value, origin string, ok bool) {
	if v, ok := os.LookupEnv(name); ok {
		return v, OriginEnv, true
	}
	if v, ok := r.root[name]; ok {
		return v, OriginRootEnv, true
	}
	if v, ok := r.global[name]; ok {
		return v, OriginGlobalEnv, true
	}
	return "", "", false
}

// TrackedEnvFiles reports which secret-bearing env files are TRACKED in git
// under repo — the gitignore trap (spike): a file committed BEFORE the
// scaffolded ignore stays tracked, and `git check-ignore` lies about it
// (index wins). Detection is `git ls-files --error-unmatch -- <p>`: exit 0 =
// tracked → warn "git rm --cached"; any other exit or exec error = silent
// no-op (not a repo, no git, not tracked).
func TrackedEnvFiles(repo string) []string {
	var tracked []string
	for _, rel := range []string{OriginRootEnv} {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(rel))); err != nil {
			continue
		}
		cmd := exec.Command("git", "-C", repo, "ls-files", "--error-unmatch", "--", rel)
		cmd.Stdout, cmd.Stderr = nil, nil // never surface git output
		if cmd.Run() == nil {             // exit 0 = tracked
			tracked = append(tracked, rel)
		}
	}
	return tracked
}
