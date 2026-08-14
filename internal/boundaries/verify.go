package boundaries

// Verify is ADR 2026-08-13-boundary-authoring D3: the standing check that
// holds every rule to its own evidence.
//
// WHAT IT CAN AND CANNOT PROVE — the honest bit, stated once, here.
//
// A shipped `verified` block records an AGGREGATE measured across corpora that
// are not on this machine (env-go's expected=179 is go-kubernetes + go-gin +
// ctx-optimize together). Verify therefore does NOT re-derive those numbers and
// never pretends to: it re-runs the rule's recorded ground truth against THE
// REPO IN FRONT OF IT and reports the LOCAL recall, carrying the shipped figure
// alongside, labelled with the corpora it came from. Two numbers, never
// conflated.
//
// Regression is therefore measured against a LOCAL baseline
// (`.ctxoptimize/boundaries-baseline.json`, written by `--record`), governed
// like the golden net: recall may move up freely, and a drop is a reviewed
// diff. This is what makes D3's `recall 0.96 → 0.71  ⚠ 14 new exec sites
// unmatched` real — those 14 sites are new code in THIS repo that the rule
// stopped seeing, which is exactly the failure ADR 2 exists to catch.
//
// PRECISION IS NOT RE-MEASURED. D1 defines precision as confirmed ÷ sampled
// with a human (or agent) checking each hit at its file:line — `ctx-optimize
// verify` is that step. A machine cannot self-confirm a true positive, so the
// recorded precision is carried as a claim, never recomputed and never
// silently blessed.
//
// A rule with no ground-truth sites in this repo is reported `unexercised` —
// counted apart from passes, because a rule that never ran has not passed.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/search"
)

// BaselineFile is the committed local floor, reviewed like a golden snapshot.
const BaselineFile = "boundaries-baseline.json"

// Rule statuses. Anything that is not `ok` is visible in the summary line —
// silence that reads as completeness is the failure this whole ADR prevents.
const (
	StatusOK           = "ok"
	StatusRegressed    = "regressed"
	StatusImproved     = "improved"
	StatusUnexercised  = "unexercised"
	StatusUnverifiable = "unverifiable"
)

// groundTruth is the recorded independent count (D2). `re`/`ext` are the
// machine-readable form of `cmd`: the human string is shell-shaped and one
// shipped regex contains nested quotes and a backtick, so re-parsing it would
// be a guess. Both forms are checked to agree when defaults.json is authored.
type groundTruth struct {
	Tool    string   `json:"tool"`
	Cmd     string   `json:"cmd"`
	Re      string   `json:"re"`
	Ext     []string `json:"ext"`
	Corpora []string `json:"corpora"`
}

type verifiedBlock struct {
	At          string      `json:"at"`
	GroundTruth groundTruth `json:"ground_truth"`
	Expected    int         `json:"expected"`
	Matched     int         `json:"matched"`
	Recall      *float64    `json:"recall"`
	Sampled     int         `json:"sampled"`
	Confirmed   int         `json:"confirmed"`
	Precision   *float64    `json:"precision"`
	KnownMisses []string    `json:"known_misses"`
}

// Site is one (file, line) — the unit both sides count, matching what
// `search --count` reports (one per matching line).
type Site struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text,omitempty"`
}

// RuleResult is one rule measured against the repo in front of us.
type RuleResult struct {
	Rule      string `json:"rule"`
	Transport string `json:"transport"`
	Tier      string `json:"tier"`
	Status    string `json:"status"`

	Expected int      `json:"expected"` // local ground-truth sites (the denominator)
	Matched  int      `json:"matched"`  // local rule sites that land on a ground-truth site
	Extra    int      `json:"extra"`    // rule sites the ground truth does NOT cover
	Recall   *float64 `json:"recall"`

	BaselineRecall *float64 `json:"baseline_recall,omitempty"`
	ClaimedRecall  *float64 `json:"claimed_recall,omitempty"`
	ClaimedOn      []string `json:"claimed_on,omitempty"` // corpora behind ClaimedRecall
	KnownMisses    []string `json:"known_misses,omitempty"`

	Unmatched      []Site `json:"unmatched,omitempty"` // capped sample
	UnmatchedTotal int    `json:"unmatched_total"`
	Note           string `json:"note,omitempty"`
}

// Report is the whole run.
type Report struct {
	Root         string       `json:"root"`
	BaselinePath string       `json:"baseline_path,omitempty"`
	HasBaseline  bool         `json:"has_baseline"`
	Rules        []RuleResult `json:"rules"`
	Exercised    int          `json:"exercised"`
	Unexercised  int          `json:"unexercised"`
	Unverifiable int          `json:"unverifiable"`
	Regressed    int          `json:"regressed"`
}

// Baseline is the committed local floor.
type Baseline struct {
	Version int                `json:"version"`
	Note    string             `json:"note"`
	Rules   map[string]float64 `json:"rules"` // rule id → recall floor
}

// unmatchedCap bounds the examples printed per rule. The TOTAL is always
// reported beside them — a silent truncation would read as "that is all of
// them", which is the same lie as a missing known_misses.
const unmatchedCap = 5

// Verify measures every merged rule against root.
func Verify(root string) (*Report, error) {
	rules, err := Load(root)
	if err != nil {
		return nil, err
	}
	rep := &Report{Root: root}

	base, basePath, err := loadBaseline(root)
	if err != nil {
		return nil, err
	}
	if base != nil {
		rep.HasBaseline = true
		rep.BaselinePath = basePath
	}

	for i := range rules {
		r := &rules[i]
		res := verifyRule(root, r)
		if base != nil {
			if b, ok := base.Rules[r.ID]; ok {
				bb := b
				res.BaselineRecall = &bb
				if res.Recall != nil && *res.Recall < bb-1e-9 {
					res.Status = StatusRegressed
				} else if res.Recall != nil && *res.Recall > bb+1e-9 {
					res.Status = StatusImproved
				}
			}
		}
		switch res.Status {
		case StatusUnexercised:
			rep.Unexercised++
		case StatusUnverifiable:
			rep.Unverifiable++
		default:
			rep.Exercised++
		}
		if res.Status == StatusRegressed {
			rep.Regressed++
		}
		rep.Rules = append(rep.Rules, res)
	}
	sort.Slice(rep.Rules, func(i, j int) bool { return rep.Rules[i].Rule < rep.Rules[j].Rule })
	return rep, nil
}

func verifyRule(root string, r *Rule) RuleResult {
	res := RuleResult{Rule: r.ID, Transport: r.Transport, Tier: r.Tier}
	if res.Tier == "" {
		res.Tier = "EXTRACTED"
	}

	var vb verifiedBlock
	if len(r.Verified) == 0 {
		res.Status = StatusUnverifiable
		res.Note = "no verified block — a rule without its measurement is invalid by definition (ADR 2 D1)"
		return res
	}
	if err := json.Unmarshal(r.Verified, &vb); err != nil {
		res.Status = StatusUnverifiable
		res.Note = "verified block does not parse: " + err.Error()
		return res
	}
	res.ClaimedRecall = vb.Recall
	res.ClaimedOn = vb.GroundTruth.Corpora
	res.KnownMisses = vb.KnownMisses

	if vb.GroundTruth.Re == "" {
		res.Status = StatusUnverifiable
		res.Note = "ground_truth has no machine-readable `re` — cannot re-run it here"
		return res
	}
	gtRe, err := regexp.Compile(vb.GroundTruth.Re)
	if err != nil {
		res.Status = StatusUnverifiable
		res.Note = "ground_truth.re does not compile: " + err.Error()
		return res
	}

	// Ground truth: the independent sweep, same RE2 engine and same file set
	// as the extractor (that is the whole reason `search` exists).
	gtMatches, err := search.Run(root, gtRe, search.Options{Exts: vb.GroundTruth.Ext})
	if err != nil {
		res.Status = StatusUnverifiable
		res.Note = "ground-truth sweep failed: " + err.Error()
		return res
	}
	// The rule's own scope exclusions apply to BOTH sides. A rule that
	// deliberately skips vendored trees must not be scored as if those sites
	// were misses — excluding them is the precision decision ADR 2 demands
	// (the spike's false positive was a vendored corpus reported as egress).
	gt := map[Site]string{}
	for _, m := range gtMatches {
		if excludedPath(m.File, r.Exclude.Path) {
			continue
		}
		gt[Site{File: m.File, Line: m.Line}] = m.Text
	}

	ruleSites, err := ruleSites(root, r)
	if err != nil {
		res.Status = StatusUnverifiable
		res.Note = "rule sweep failed: " + err.Error()
		return res
	}

	res.Expected = len(gt)
	var unmatched []Site
	for s, text := range gt {
		if ruleSites[s] {
			res.Matched++
		} else {
			unmatched = append(unmatched, Site{File: s.File, Line: s.Line, Text: text})
		}
	}
	for s := range ruleSites {
		if _, ok := gt[s]; !ok {
			res.Extra++
		}
	}

	if res.Expected == 0 {
		res.Status = StatusUnexercised
		res.Note = "no ground-truth sites in this repo — the rule was not exercised, so it has not passed"
		return res
	}
	recall := float64(res.Matched) / float64(res.Expected)
	res.Recall = &recall
	res.Status = StatusOK

	sort.Slice(unmatched, func(i, j int) bool {
		if unmatched[i].File != unmatched[j].File {
			return unmatched[i].File < unmatched[j].File
		}
		return unmatched[i].Line < unmatched[j].Line
	})
	res.UnmatchedTotal = len(unmatched)
	if len(unmatched) > unmatchedCap {
		unmatched = unmatched[:unmatchedCap]
	}
	res.Unmatched = unmatched
	return res
}

// ruleSites runs one rule over the same walk `search` uses and returns the
// (file, line) sites where it successfully captured an identifier. Line
// granularity — not raw match count — is what makes it comparable to a
// `search --count`, which reports one hit per matching line.
func ruleSites(root string, r *Rule) (map[Site]bool, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	files, err := search.Files(root, search.Options{Exts: r.When.Ext})
	if err != nil {
		return nil, err
	}
	out := map[Site]bool{}
	for _, path := range files {
		rel, rerr := filepath.Rel(absRoot, path)
		if rerr != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if excludedPath(rel, r.Exclude.Path) {
			continue
		}
		content, cerr := os.ReadFile(path)
		if cerr != nil {
			continue
		}
		for ri, re := range r.res {
			gi := r.Match[ri].Identifier
			for _, m := range re.FindAllSubmatchIndex(content, -1) {
				var ident string
				if gi > 0 && 2*gi+1 < len(m) && m[2*gi] >= 0 {
					ident = string(content[m[2*gi]:m[2*gi+1]])
				}
				if ident == "" || Normalize(r.Transport, ident) == "" {
					continue // no identifier captured = no port emitted = not a site
				}
				line := 1 + bytes.Count(content[:m[0]], []byte{'\n'})
				out[Site{File: rel, Line: line}] = true
			}
		}
	}
	return out, nil
}

func baselinePath(root string) string {
	return filepath.Join(root, ".ctxoptimize", BaselineFile)
}

func loadBaseline(root string) (*Baseline, string, error) {
	p := baselinePath(root)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", err
	}
	var b Baseline
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return nil, "", fmt.Errorf("%s: %w", p, err) // fail-closed, like every other door
	}
	return &b, p, nil
}

// RecordBaseline writes the current LOCAL recalls as the floor. Only
// exercised rules are recorded: pinning a floor for a rule that never ran
// would be a number with nothing behind it.
func RecordBaseline(root string, rep *Report) (string, error) {
	b := Baseline{
		Version: 1,
		Note: "LOCAL recall floor for `ctx-optimize boundaries verify` (ADR 2026-08-13-boundary-authoring D3). " +
			"Measured against THIS repo, not the corpora behind each rule's shipped verified block. " +
			"Governed like the golden net: recall may move up freely; lowering a floor is a reviewed diff.",
		Rules: map[string]float64{},
	}
	for _, r := range rep.Rules {
		if r.Recall != nil {
			b.Rules[r.Rule] = *r.Recall
		}
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	p := baselinePath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// Pct renders a recall for humans; a nil recall means the rule never ran.
func Pct(f *float64) string {
	if f == nil {
		return "  —  "
	}
	return fmt.Sprintf("%.2f", *f)
}

// CorporaNote renders the provenance of a claimed number so a reader is never
// invited to compare it with the local one as though they measured the same
// thing.
func CorporaNote(on []string) string {
	if len(on) == 0 {
		return ""
	}
	return "claimed on " + strings.Join(on, ", ")
}
