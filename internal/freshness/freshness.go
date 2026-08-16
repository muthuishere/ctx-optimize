// Package freshness compares a store's recorded source provenance (the git HEAD
// captured at add time) against the source repo's current HEAD, so an agent can
// tell whether the store still reflects the code or has fallen behind.
//
// Everything here is pure: no git, no filesystem, no wall clock. The CLI layer
// reads the current HEAD (best-effort) and passes it in. This keeps the
// comparison deterministic and unit-testable with the stdlib only.
package freshness

// State is the freshness verdict for one tracked source root.
type State string

const (
	// Fresh — recorded head equals the current head; the store is up to date.
	Fresh State = "fresh"
	// Stale — both heads are known and differ; the store predates the code.
	Stale State = "stale"
	// Unknown — a head is missing (not a git repo, git absent, or no provenance
	// was recorded). Freshness cannot be determined; never treated as an error.
	Unknown State = "unknown"
	// Partial — the gather that wrote this store had producer lanes FAIL, so
	// the store is incomplete whatever its head says. A DISTINCT state, not
	// Stale: stale means "the code moved on, re-gather", partial means "the
	// last gather broke, look at why". Overloading one exit code would tell a
	// hook the wrong fix (ADR 2026-07-26-failure-containment, issue #13).
	Partial State = "partial"
)

// Source is what add recorded about one gathered root.
type Source struct {
	Path      string `json:"path"`               // absolute source root
	Head      string `json:"head"`               // git HEAD sha at add time
	HeadUnix  int64  `json:"head_unix"`          // committer time of that HEAD
	AddedUnix int64  `json:"added_unix"`         // when add ran
	TreeSig   string `json:"tree_sig,omitempty"` // stat-signature of the source tree at add time (path+mtime+size hash) — the 0-change short-circuit gate (ADR 2026-07-24-lazy-autosync, lever 1)
	// Partial names the producer lanes that FAILED in the gather that wrote
	// this record. Lane failures are contained (one broken adapter no longer
	// discards a whole gather), and containment is only honest if the
	// incompleteness is recorded: a store missing its code lane must not
	// answer as though it has one. Empty = complete.
	Partial []string `json:"partial,omitempty"`
	// RulesSig is the boundary RULE VOCABULARY this gather ran with. Empty on
	// a store written before stores recorded it.
	//
	// A third kind of out-of-date, and it needs its own name for the same
	// reason Partial does: Stale means the code moved on (fix: `add`), Partial
	// means the last gather broke (fix: look at why), and Dated means the rules
	// changed underneath a store the code never touched — which `add` will NOT
	// fix, because the tree signature is identical and every module is
	// correctly skipped. Only `add --force` rewrites it.
	RulesSig string `json:"rules_sig,omitempty"`
}

// Dated reports whether this store's boundary answers were produced by a
// different rule vocabulary than the one now loaded.
//
// Conservative on both sides: a store with no recorded signature is NOT called
// dated (it predates the record, and guessing would cry wolf on every old
// store), and an empty current signature never accuses anything.
func (s Source) Dated(currentRulesSig string) bool {
	return s.RulesSig != "" && currentRulesSig != "" && s.RulesSig != currentRulesSig
}

// Report is the freshness verdict for one source.
type Report struct {
	Path         string `json:"path"`
	State        State  `json:"state"`
	StoreHead    string `json:"store_head"`               // head recorded at add time
	CurrentHead  string `json:"current_head"`             // head right now (may be "")
	AgeSeconds   int64  `json:"age_seconds"`              // now - added_unix (store snapshot age)
	BehindSecond int64  `json:"behind_seconds,omitempty"` // current_head_unix - store_head_unix, when stale & known
	// Partial carries the producer lanes that failed in the gather that wrote
	// this source, so a caller can say WHICH part is missing rather than just
	// that something is.
	Partial []string `json:"partial,omitempty"`
	// Dated: the boundary rules changed since this store was written. NOT a
	// State — it is orthogonal to git freshness, and a store can be perfectly
	// fresh and dated at once.
	Dated bool `json:"dated,omitempty"`
}

// Evaluate compares one recorded source against the repo's current head.
// currentHead == "" (or an empty recorded head) yields Unknown. now and
// currentHeadUnix are injected so the function stays pure.
func Evaluate(rec Source, currentHead string, currentHeadUnix, now int64) Report {
	r := Report{
		Path:        rec.Path,
		StoreHead:   rec.Head,
		CurrentHead: currentHead,
	}
	if rec.AddedUnix > 0 && now >= rec.AddedUnix {
		r.AgeSeconds = now - rec.AddedUnix
	}
	r.Partial = rec.Partial
	switch {
	case len(rec.Partial) > 0:
		// Wins over everything: a store missing a producer is not trustworthy
		// however well its head matches. A head-matching partial store used to
		// report Fresh, which is the exact lie this state exists to stop.
		r.State = Partial
	case rec.Head == "" || currentHead == "":
		r.State = Unknown
	case rec.Head == currentHead:
		r.State = Fresh
	default:
		r.State = Stale
		if currentHeadUnix > 0 && rec.HeadUnix > 0 && currentHeadUnix > rec.HeadUnix {
			r.BehindSecond = currentHeadUnix - rec.HeadUnix
		}
	}
	return r
}

// Overall folds many reports into a single exit-code-friendly verdict:
// any Stale ⇒ Stale; else any Unknown (or none at all) ⇒ Unknown; else Fresh.
// Empty input is Unknown (no provenance to judge).
func Overall(reports []Report) State {
	if len(reports) == 0 {
		return Unknown
	}
	// Severity order: Partial > Stale > Unknown > Fresh. Partial is highest
	// because it is the only one that means data is MISSING rather than old,
	// and a mixed store must not have that masked by a sibling's staleness.
	sawStale, sawUnknown := false, false
	for _, r := range reports {
		switch r.State {
		case Partial:
			return Partial
		case Stale:
			sawStale = true
		case Unknown:
			sawUnknown = true
		}
	}
	switch {
	case sawStale:
		return Stale
	case sawUnknown:
		return Unknown
	}
	return Fresh
}

// ExitCode maps an overall state to a process exit code for agent/hook gating:
// 0 fresh, 1 stale, 2 unknown, 3 partial.
//
// 3 is a new code rather than a reuse of 1: an existing hook that gates on
// `!= 0` keeps working unchanged, while one that distinguishes cases can tell
// "re-gather, the code moved" from "the last gather broke, go look".
func ExitCode(s State) int {
	switch s {
	case Fresh:
		return 0
	case Stale:
		return 1
	case Partial:
		return 3
	default:
		return 2
	}
}
