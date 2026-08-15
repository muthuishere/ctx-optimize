package app

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Unknown flags used to be SILENTLY IGNORED, repo-wide, exit 0.
//
// `boundaries --sensitve` (one letter short) printed the FULL list including
// non-secrets and exited 0, so the reader believed they were seeing only
// credentials. The failure always errs toward showing MORE than was asked for,
// and it lands worst on exactly the flag whose job is to narrow output to
// secrets. `nodes --kindd port` and `drift --nosuchflag` had the same shape.
//
// A second, quieter case of the same disease: a REPEATED string flag silently
// dropped every value but the last, because the parser is a map. Measured:
//
//	--where sensitive=true                              -> 1 node
//	--where transport=network.http --where sensitive=true -> 1 node   WRONG
//	--where transport=network.http,sensitive=true        -> 0 nodes   correct
//
// The middle form is what our own skill router documents, and it silently threw
// the transport condition away — returning a plausible answer, which is worse
// than an error. `where` is therefore REPEATABLE here and its values are
// comma-joined, which is exactly the AND that graphfilter already parses.
//
// The allowlist below was derived by a static audit of every `f.strs[...]` and
// `f.bools[...]` read reachable from each verb, including through the helpers
// that take *flags (openStore, resolvePath, ambOpts, noWiki, narrowQuery,
// resolveScope, …) and through graphfilter.ParsePred, which consumes the whole
// map. It is deliberately GENEROUS — a verb's entry is the union of it and its
// subcommands — because the goal is catching typos, not enforcing minimal sets.
// A wrong entry here can only cause a false rejection, so err wide.

// predFlags are consumed by graphfilter.ParsePred, which takes the entire flag
// map. Any verb that calls it accepts all of them.
var predFlags = []string{
	"confidence", "file-type", "from", "id-prefix", "kind",
	"label", "producer", "relation", "scope", "to", "where",
}

// commonFlags are accepted everywhere: every verb either resolves a store or
// can reasonably be asked for machine output, and --help must never be an error.
var commonFlags = []string{"path", "store", "root", "json", "help", "h"}

// repeatableFlags may appear more than once; their values are comma-joined.
// Only `where` qualifies today, because graphfilter already treats a comma as
// AND, so joining is the semantics a reader expects. Everything else repeating
// is a mistake worth surfacing rather than silently resolving to the last one.
var repeatableFlags = map[string]bool{"where": true}

// agentTargets are read as f.bools[p] with p a LOOP VARIABLE (app.go:2994,
// :3223), so a scan for f.bools["literal"] cannot see them. The first cut of
// this allowlist missed them and broke `install --claude` — which is precisely
// why the audit had to be checked against the test suite rather than trusted.
var agentTargets = []string{"claude", "codex", "copilot", "devin"}

// verbFlags maps a dispatch key to the flags it accepts, beyond commonFlags.
var verbFlags = map[string][]string{
	"query":        append([]string{"budget", "include-content", "modules", "fuzzy", "include-ambiguous", "ndjson", "select", "top"}, predFlags...),
	"ask":          append([]string{"budget", "include-content", "modules", "fuzzy", "include-ambiguous", "ndjson", "select", "top"}, predFlags...),
	"card":         {"fuzzy", "include-ambiguous", "include-content", "ndjson"},
	"node":         {"fuzzy", "include-ambiguous", "include-content", "ndjson"},
	"change-plan":  {"depth", "include-ambiguous", "ndjson"},
	"plan":         {"depth", "include-ambiguous", "ndjson"},
	"affected":     append([]string{"depth", "fuzzy", "include-ambiguous", "ndjson"}, predFlags...),
	"impact":       append([]string{"depth", "fuzzy", "include-ambiguous", "ndjson"}, predFlags...),
	"path":         {"include-ambiguous", "ndjson"},
	"explain":      {"fuzzy", "include-ambiguous", "ndjson"},
	"hubs":         append([]string{"include-ambiguous", "ndjson", "top"}, predFlags...),
	"verify":       {"ndjson"},
	"nodes":        append([]string{"ndjson", "select"}, predFlags...),
	"edges":        append([]string{"ndjson", "select"}, predFlags...),
	"deps":         append([]string{"importers", "ndjson", "select"}, predFlags...),
	"export":       append([]string{"format", "ndjson", "out"}, predFlags...),
	"report":       append([]string{"ndjson"}, predFlags...),
	"boundaries":   {"all", "direction", "external", "record", "sensitive", "strict", "transport", "ndjson"},
	"drift":        {"strict", "ndjson"},
	"services":     {"ndjson"},
	"search":       {"count", "ext", "files", "ndjson"},
	"add":          {"force", "jobs", "no-adapters", "no-wiki", "rebuild", "wiki", "adapters", "all", "prune-sources", "sources", "into", "strict"},
	"sync":         {"force", "jobs", "no-adapters", "no-wiki", "rebuild", "wiki", "adapters", "all", "prune-sources", "sources", "strict"},
	"up":           {"force", "jobs", "no-adapters", "no-wiki", "rebuild", "wiki", "adapters", "all", "depth", "scan", "yes", "modules", "instructions", "sources", "prune-sources", "strict"},
	"fresh":        {"force", "jobs", "no-wiki"},
	"init":         {"depth", "force", "instructions", "modules", "scan", "yes"},
	"scan":         {"depth"},
	"capture":      {"sources", "prune-sources"},
	"merge":        {"into"},
	"wiki":         {"delete", "rebuild"},
	"report-save":  {"answer", "correction", "nodes", "outcome", "question", "type"},
	"reflect":      {"half-life-days", "min-corroboration"},
	"serve":        {"host", "port"},
	"dashboard":    {"host", "port"},
	"languages":    {"name", "out", "ref"},
	"grammar":      {"name", "out", "ref"},
	"routes":       {"global"},
	"manifests":    {"global"},
	"remote":       {"force"},
	"config":       {"project", "global"},
	"log":          {"ndjson"},
	"install":      append([]string{"hooks", "skills", "instructions", "global", "yes"}, agentTargets...),
	"uninstall":    append([]string{"hooks", "skills", "global", "yes"}, agentTargets...),
	"store":        {"yes", "delete"},
	"status":       {"ndjson"},
	"update":       append([]string{"check", "hooks", "skills"}, agentTargets...),
	"hook-context": {"format"},
	"adapters":     {"force", "all"},
	"__autosync":   {"dir", "lock"},
}

// wantsHelp reports whether args ask for usage rather than for the verb's
// effect. `--help` was ACCEPTED by every verb (it is in commonFlags) and then
// IGNORED by every verb's handler, so `install --help` performed the install —
// skills written for four agents and a global rule added to the user's home —
// and `uninstall --help` removed them again. Same class as the unknown flag
// that was silently dropped: a flag that reads as a QUESTION was answered with
// an ACTION. `--help` is the flag someone types when they are not sure they
// want the effect, so it must be handled before dispatch, for every verb (ADR
// 2026-08-15-skills-on-by-default, D2).
//
// A value never starts with `--` in this parser, so any `--help` token is a
// flag and never someone's argument.
func wantsHelp(args []string) bool {
	for _, a := range args {
		switch a {
		case "--help", "--h", "-h":
			return true
		case "--": // everything after the separator is literal
			return false
		}
	}
	return false
}

// verbHelp prints what the verb does and what it accepts, and writes nothing
// else anywhere. The description is lifted from the same usage() text the bare
// `help` verb prints, so the two can never drift; when no block matches (an
// alias or an undocumented verb) the accepted-flag list still answers the
// question that was asked.
func verbHelp(w io.Writer, verb string) {
	if block := usageBlock(verb); block != "" {
		fmt.Fprint(w, block)
	} else {
		fmt.Fprintf(w, "ctx-optimize %s\n", verb)
	}
	if allowed, known := verbFlags[verb]; known {
		ok := map[string]bool{}
		for _, n := range commonFlags {
			ok[n] = true
		}
		for _, n := range allowed {
			ok[n] = true
		}
		fmt.Fprintf(w, "\naccepted flags: %s\n", flagList(ok))
	}
	fmt.Fprintln(w, "\n`ctx-optimize help` lists every command.")
}

// usageBlock returns the lines of usage() that document verb: the entry whose
// first token matches (aliases are written `query|ask`, so the token is split
// on `|`) plus its indented continuation lines.
func usageBlock(verb string) string {
	var buf strings.Builder
	in := false
	for _, line := range strings.Split(usageText(), "\n") {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
			head := strings.Fields(strings.TrimSpace(line))
			in = false
			if len(head) > 0 {
				for _, alias := range strings.Split(head[0], "|") {
					if alias == verb {
						in = true
					}
				}
			}
		} else if strings.TrimSpace(line) == "" {
			in = false
		}
		if in {
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}
	return buf.String()
}

// checkFlags rejects a flag the verb does not accept. Returns a usage error, so
// the caller exits 2 — matching what an unknown VERB already does.
//
// Verbs absent from the table are unvalidated rather than rejected: a new verb
// that forgets to register must keep working, because failing closed here would
// turn an omission in this file into a broken command.
func checkFlags(verb string, args []string) error {
	allowed, known := verbFlags[verb]
	if !known {
		return nil
	}
	ok := map[string]bool{}
	for _, n := range commonFlags {
		ok[n] = true
	}
	for _, n := range allowed {
		ok[n] = true
	}

	seen := map[string]int{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			continue
		}
		name := strings.TrimPrefix(a, "--")
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name = name[:eq]
		} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			i++ // this flag consumed a value; skip it so a value is never read as a flag
		}
		if name == "" { // the bare `--` separator
			continue
		}
		if !ok[name] {
			return fmt.Errorf("unknown flag --%s for %q%s\n\naccepted: %s",
				name, verb, suggest(name, ok), flagList(ok))
		}
		seen[name]++
		if seen[name] > 1 && !repeatableFlags[name] {
			return fmt.Errorf("--%s given more than once for %q; only the last would take effect. "+
				"Combine the values instead", name, verb)
		}
	}
	return nil
}

// suggest names the closest accepted flag when one is obviously meant — a
// single edit away, or a prefix. Silence is better than a bad guess.
func suggest(bad string, ok map[string]bool) string {
	best, bestD := "", 3
	for n := range ok {
		if d := editDistance(bad, n); d < bestD {
			best, bestD = n, d
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf(" (did you mean --%s?)", best)
}

// editDistance is Levenshtein, bounded by the caller's threshold. Small inputs
// (flag names), so the simple full matrix is the right tradeoff.
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(min(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func flagList(ok map[string]bool) string {
	var out []string
	for n := range ok {
		if n == "h" || n == "help" {
			continue
		}
		out = append(out, "--"+n)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}
