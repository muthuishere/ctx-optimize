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
)

// Source is what add recorded about one gathered root.
type Source struct {
	Path      string `json:"path"`       // absolute source root
	Head      string `json:"head"`       // git HEAD sha at add time
	HeadUnix  int64  `json:"head_unix"`  // committer time of that HEAD
	AddedUnix int64  `json:"added_unix"` // when add ran
}

// Report is the freshness verdict for one source.
type Report struct {
	Path         string `json:"path"`
	State        State  `json:"state"`
	StoreHead    string `json:"store_head"`             // head recorded at add time
	CurrentHead  string `json:"current_head"`           // head right now (may be "")
	AgeSeconds   int64  `json:"age_seconds"`            // now - added_unix (store snapshot age)
	BehindSecond int64  `json:"behind_seconds,omitempty"` // current_head_unix - store_head_unix, when stale & known
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
	switch {
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
	sawUnknown := false
	for _, r := range reports {
		switch r.State {
		case Stale:
			return Stale
		case Unknown:
			sawUnknown = true
		}
	}
	if sawUnknown {
		return Unknown
	}
	return Fresh
}

// ExitCode maps an overall state to a process exit code for agent/hook gating:
// 0 fresh, 1 stale, 2 unknown.
func ExitCode(s State) int {
	switch s {
	case Fresh:
		return 0
	case Stale:
		return 1
	default:
		return 2
	}
}
