// verify — the defensive verb (ADR 2026-07-16-verify-verb): deterministic
// citation checking. An agent about to hand a human a claim shaped like
// "RunPayroll @ services/worker/payroll.go L10-L20" runs it through here;
// fabricated citations, drifted code, and out-of-range lines all fail
// mechanically. No LLM, no network — a store lookup, a stat, a line count,
// and the git provenance the store already records.
package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/analyze"
	"github.com/muthuishere/ctx-optimize/internal/gitinfo"
	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

// fuzzyPick returns the top candidate's id when --fuzzy accepts an
// ambiguous resolution (analyze.AmbiguousError); otherwise ok=false and the
// original error stands. Candidates are already deterministically ordered.
func fuzzyPick(err error, f *flags) (string, bool) {
	var amb *analyze.AmbiguousError
	if err != nil && f.bools["fuzzy"] && errors.As(err, &amb) && len(amb.Candidates) > 0 {
		return amb.Candidates[0].ID, true
	}
	return "", false
}

// Verdict values, worst-first for reporting.
const (
	verdictOK          = "ok"
	verdictDrifted     = "drifted"
	verdictMissingNode = "missing-node"
	verdictMissingFile = "missing-file"
	verdictOutOfRange  = "out-of-range"
)

type verifyResult struct {
	Claim    string `json:"claim"`
	Verdict  string `json:"verdict"`
	Node     string `json:"node,omitempty"`
	Source   string `json:"source,omitempty"`
	Location string `json:"location,omitempty"`
	Drift    string `json:"drift,omitempty"` // clean | changed | unknown
	Detail   string `json:"detail,omitempty"`
}

var fileClaimRe = regexp.MustCompile(`^(.+):L(\d+)(?:-L?(\d+))?$`)
var locRe = regexp.MustCompile(`L(\d+)(?:-L?(\d+))?`)

// cmdVerify checks one or more citation claims against the store AND the
// working tree. Claim forms: a node id, an EXACT label (verify never
// fuzzes — a fuzzy verify would re-introduce the guess it exists to catch),
// or "path/file.go:L10-L20". Exit 0 only when every claim holds.
func cmdVerify(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if len(f.args) == 0 {
		return fmt.Errorf(`usage: ctx-optimize verify "<node-id | exact-label | file:L10-L20>" ...`)
	}
	nodes, _, sc, _, err := loadGraphScoped(f)
	if err != nil {
		return err
	}
	// Gather-time git provenance for the drift check.
	head := gatherHead(f, sc)

	results := make([]verifyResult, 0, len(f.args))
	failed := 0
	for _, claim := range f.args {
		r := verifyClaim(claim, nodes, sc.rootDir, head)
		if r.Verdict != verdictOK {
			failed++
		}
		results = append(results, r)
	}
	if f.bools["json"] {
		if err := emit(stdout, map[string]any{"claims": results, "ok": failed == 0}); err != nil {
			return err
		}
	} else {
		for _, r := range results {
			line := r.Verdict + "  " + r.Claim
			if r.Node != "" && r.Node != r.Claim {
				line += "  → " + r.Node
			}
			if r.Source != "" {
				line += "  (" + r.Source
				if r.Location != "" {
					line += " " + r.Location
				}
				if r.Drift != "" {
					line += ", " + driftWord(r.Drift)
				}
				line += ")"
			}
			if r.Detail != "" {
				line += "  — " + r.Detail
			}
			fmt.Fprintln(stdout, line)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d claims failed verification", failed, len(results))
	}
	return nil
}

func driftWord(d string) string {
	switch d {
	case "clean":
		return "unchanged since gather"
	case "changed":
		return "CHANGED since gather — run `ctx-optimize sync` and re-check"
	default:
		return "drift unknown (no git provenance)"
	}
}

// gatherHead returns the git HEAD recorded at gather time for this scope's
// store ("" when unrecorded — drift reports unknown, never a false ok).
func gatherHead(f *flags, sc *scope) string {
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return ""
	}
	s, err := store.Open(storeRoot, sc.storeKey)
	if err != nil {
		return ""
	}
	srcs, err := s.Sources()
	if err != nil {
		return ""
	}
	for _, src := range srcs {
		if src.Head != "" {
			return src.Head
		}
	}
	return ""
}

func verifyClaim(claim string, nodes []schema.Node, repoDir, gatherHead string) verifyResult {
	r := verifyResult{Claim: claim}

	// file:Lstart-Lend claim — no node needed, check the location itself.
	if m := fileClaimRe.FindStringSubmatch(claim); m != nil && !strings.Contains(m[1], "://") {
		r.Source = m[1]
		r.Location = "L" + m[2]
		if m[3] != "" {
			r.Location += "-L" + m[3]
		}
		checkLocation(&r, repoDir, gatherHead, m[1], m[2], m[3])
		return r
	}

	// Node claim: exact id or exact label ONLY.
	var hit *schema.Node
	for i := range nodes {
		if nodes[i].ID == claim {
			hit = &nodes[i]
			break
		}
	}
	if hit == nil {
		for i := range nodes {
			if strings.EqualFold(nodes[i].Label, claim) {
				if hit == nil || nodes[i].ID < hit.ID {
					hit = &nodes[i]
				}
			}
		}
	}
	if hit == nil {
		r.Verdict = verdictMissingNode
		if _, _, err := analyze.ResolveVia(nodes, claim); err != nil {
			r.Detail = strings.SplitN(err.Error(), "\n", 2)[0] // suggestions, first line
		} else {
			r.Detail = "no exact match — verify never fuzzes; use the exact label or id"
		}
		return r
	}
	r.Node = hit.ID
	r.Source = hit.Source
	r.Location = hit.Location
	if hit.Source == "" || strings.Contains(hit.Source, "://") {
		// Adapter-fed nodes (kafka://…) have no file to check against.
		r.Verdict = verdictOK
		r.Drift = "unknown"
		r.Detail = "non-file source — store existence verified only"
		return r
	}
	start, end := "", ""
	if lm := locRe.FindStringSubmatch(hit.Location); lm != nil {
		start, end = lm[1], lm[2]
	}
	checkLocation(&r, repoDir, gatherHead, hit.Source, start, end)
	return r
}

// readSourceBody hydrates a node's VERBATIM source text on demand — the
// content-hydration spike (openspec/changes/2026-07-24-content-hydration):
// the store keeps only pointers (file:line), so `query`/`card
// --include-content` read the slice from the working tree at answer time
// instead of ever storing bodies. Same location parsing (locRe) and file-open
// discipline as checkLocation above — reused, not reinvented.
//
// Freshness caveat: the file is read AS-IS now; if it drifted since the store
// was gathered, the cited range may no longer line up with the node it was
// extracted from (see `verify` for drift detection — not run here, this is
// the fast path).
func readSourceBody(root string, n schema.Node) (string, error) {
	if n.Source == "" || strings.Contains(n.Source, "://") {
		return "", fmt.Errorf("no file source (adapter/non-file node)")
	}
	m := locRe.FindStringSubmatch(n.Location)
	if m == nil {
		return "", fmt.Errorf("no line location on this node")
	}
	if root == "" {
		root = "."
	}
	p := filepath.Join(root, filepath.FromSlash(n.Source))
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("source file not found: %s", n.Source)
		}
		return "", fmt.Errorf("cannot read %s: %w", n.Source, err)
	}
	lines := strings.Split(string(data), "\n")
	start, err := strconv.Atoi(m[1])
	if err != nil {
		return "", fmt.Errorf("bad location %q", n.Location)
	}
	end := start
	if m[2] != "" {
		if e, err := strconv.Atoi(m[2]); err == nil {
			end = e
		}
	}
	if start < 1 || start > len(lines) {
		return "", fmt.Errorf("start line %d out of range (%s has %d lines)", start, n.Source, len(lines))
	}
	if end > len(lines) {
		end = len(lines)
	}
	if end < start {
		end = start
	}
	body := strings.Join(lines[start-1:end], "\n")
	// An empty/whitespace-only slice must surface as an explicit error, not a
	// silent empty `content` field (omitempty would drop it, leaving the hit
	// with neither content nor a reason — the "empty and not there" case).
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("empty source range at %s %s", n.Source, n.Location)
	}
	return body, nil
}

// checkLocation fills verdict+drift for a file (+ optional line range).
func checkLocation(r *verifyResult, repoDir, gatherHead, rel, start, end string) {
	p := filepath.Join(repoDir, filepath.FromSlash(rel))
	data, err := os.ReadFile(p)
	if err != nil {
		r.Verdict = verdictMissingFile
		r.Detail = rel + " does not exist under " + repoDir
		return
	}
	if start != "" {
		lines := bytes.Count(data, []byte{'\n'}) + 1
		s, _ := strconv.Atoi(start)
		e := s
		if end != "" {
			e, _ = strconv.Atoi(end)
		}
		if s < 1 || e < s || s > lines || e > lines {
			r.Verdict = verdictOutOfRange
			r.Detail = fmt.Sprintf("cited L%s-L%s but %s has %d lines", start, end, rel, lines)
			return
		}
	}
	changed, ok := gitinfo.FileChangedSince(repoDir, gatherHead, rel)
	switch {
	case !ok:
		r.Drift = "unknown"
		r.Verdict = verdictOK
	case changed:
		r.Drift = "changed"
		r.Verdict = verdictDrifted
		r.Detail = "file changed since the store was gathered — the cited lines may have moved"
	default:
		r.Drift = "clean"
		r.Verdict = verdictOK
	}
}
