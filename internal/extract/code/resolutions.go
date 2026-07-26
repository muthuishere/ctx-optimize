// resolutions.go — declared resolutions (ADR 2026-07-26-declared-resolutions).
//
// The extractor abstains on call sites it cannot justify, which is correct and
// leaves the same question to be re-derived by every agent forever. This is the
// door for writing an answer down: `.ctxoptimize/resolutions.json`, committed
// with the repo.
//
// The first (and currently only) key is deliberately the one that CANNOT make
// the graph wrong:
//
//	{"external_methods": ["Error", "String", "Close"]}
//
// It declares method names owned by types the repo does NOT control. Its only
// effect is to suppress a SHORTLIST — a maybe becomes nothing. It never removes
// a resolved edge and never creates one, so a mistaken entry costs recall and
// can never produce a false fact. Keys that do resolve (receiver_types) were
// scoped out: they invert who is responsible for a wrong edge, and that is a
// separate decision.
//
// Note this is the same list ADR 2026-07-25-method-call-resolution REJECTED as
// a built-in denylist. Shipped by us it is a guess about strangers' code;
// declared by the repo owner it is an assertion about their own. The authority
// is the whole difference.
package code

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ResolutionsFile is the committed declaration file, beside the other packs.
func ResolutionsFile(repo string) string {
	return filepath.Join(repo, ".ctxoptimize", "resolutions.json")
}

// Resolutions is the parsed declaration file. Absent file = zero value, which
// changes nothing.
type Resolutions struct {
	// ExternalMethods are method names whose receivers are never ours. A
	// receiver-qualified call to one is dropped instead of shortlisted.
	ExternalMethods []string `json:"external_methods"`

	// Unknown keys are REFUSED rather than ignored: a typo'd or
	// future-versioned key that silently did nothing would be a declaration
	// the author believes is in force. Same reason packs fail loudly.
	Extra map[string]json.RawMessage `json:"-"`
}

// LoadResolutions reads and validates the declaration file. A missing file is
// not an error. Anything present but malformed IS — a silently ignored
// declaration is worse than none, because the author will trust it.
func LoadResolutions(repo string) (*Resolutions, error) {
	path := ResolutionsFile(repo)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Resolutions{}, nil
	}
	if err != nil {
		return nil, err
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var r Resolutions
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var unknown []string
	for k := range probe {
		if k != "external_methods" {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("%s: unknown key(s) %s — supported: external_methods",
			path, strings.Join(unknown, ", "))
	}
	for _, m := range r.ExternalMethods {
		if strings.TrimSpace(m) == "" {
			return nil, fmt.Errorf("%s: external_methods contains an empty name", path)
		}
		if strings.ContainsAny(m, ".:()") {
			return nil, fmt.Errorf("%s: external_methods entry %q must be a BARE method name (no receiver, no parens) — the declaration is about the name, whatever the receiver", path, m)
		}
	}
	return &r, nil
}

// externalSet indexes the declaration for lookup, and tracks which entries
// actually did something so a rotted one can be reported.
type externalSet struct {
	names map[string]bool
	used  map[string]bool
}

func newExternalSet(r *Resolutions) *externalSet {
	s := &externalSet{names: map[string]bool{}, used: map[string]bool{}}
	if r == nil {
		return s
	}
	for _, m := range r.ExternalMethods {
		s.names[m] = true
	}
	return s
}

// suppress reports whether this call site is declared to target a type we do
// not own. Only RECEIVER-QUALIFIED calls qualify: the declaration is about
// method names, and an unqualified `Error()` is a plain function call that may
// well be ours.
func (s *externalSet) suppress(c callSite) bool {
	if s == nil || len(s.names) == 0 || c.recv == "" {
		return false
	}
	if !s.names[c.callee] {
		return false
	}
	s.used[c.callee] = true
	return true
}

// unused returns declared names that matched nothing, sorted. A renamed type or
// a deleted call site leaves a stale line behind, and a declaration file nobody
// prunes decays into a set of confident-looking lies about the code.
func (s *externalSet) unused() []string {
	if s == nil {
		return nil
	}
	var out []string
	for n := range s.names {
		if !s.used[n] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
