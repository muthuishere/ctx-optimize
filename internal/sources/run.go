package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

// ProducerPrefix namespaces every source's producer tag in the store, so
// reconcile can tell source producers from gather/adapter producers (H1).
const ProducerPrefix = "source:"

// DefaultTTL is how long a captured source stays fresh before `up` re-dials
// it (M3) — agents running `up` reflexively must not schema-scan prod DBs
// dozens of times a day. `add`/`capture` always dial.
const DefaultTTL = 24 * time.Hour

// Modes for Options.Mode.
const (
	ModeTTL    = "ttl"    // default: dial only when freshness is older than TTL
	ModeAlways = "always" // dial everything
	ModeNever  = "never"  // dial nothing
)

// Options tunes one Run.
type Options struct {
	Mode   string        // ModeTTL (default "" ⇒ TTL) | ModeAlways | ModeNever
	TTL    time.Duration // 0 ⇒ DefaultTTL
	Strict bool          // skips (unset vars) become failures — CI
	Now    time.Time     // zero ⇒ time.Now() (injected for tests)
}

// Outcome statuses (H4): exactly three, plus the reason for a skip.
const (
	StatusCaptured = "captured"
	StatusSkipped  = "skipped"
	StatusFailed   = "failed"

	ReasonUnset = "unset" // a referenced var is not set anywhere in the ladder
	ReasonFresh = "fresh" // freshness younger than TTL
	ReasonNever = "never" // --sources=never
)

// Outcome is one source's per-run result. Every string in it is already
// scrubbed — safe to print, emit as JSON, or store.
type Outcome struct {
	Entry  string        `json:"entry"`            // raw config entry (committed, var-shaped by the load gate)
	ID     string        `json:"id"`               // producer identity: var name, or sanitized template
	Origin string        `json:"origin,omitempty"` // where the value came from: env | .ctxoptimize/.env | .env
	Status string        `json:"status"`           // captured | skipped | failed
	Reason string        `json:"reason,omitempty"` // for skips: unset | fresh | never
	Detail string        `json:"detail,omitempty"` // scrubbed skip/error text
	Nodes  int           `json:"nodes,omitempty"`
	Edges  int           `json:"edges,omitempty"`
	Age    time.Duration `json:"-"` // freshness age at decision time (0 = none recorded)

	batch  *schema.Batch // pending merge (captured only; nil after merge)
	values []string      // resolved secret values — scrub material, NEVER leaves this package
}

// Run captures the given source entries: parallel dial goroutines, one
// result per entry, then a SERIAL merge into the store (C2 — store writes
// are read-modify-rewrite and racy by design). Per-source outcomes print to
// out with origins (names only) and staleness ages; freshness stamps are
// recorded on successful merge only. The returned outcomes preserve entry
// order.
func Run(entries []string, repo string, st *store.Store, opts Options, out io.Writer) ([]Outcome, error) {
	if opts.Mode == "" {
		opts.Mode = ModeTTL
	}
	if opts.TTL == 0 {
		opts.TTL = DefaultTTL
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	stamps, err := SourceStamps(st.Dir)
	if err != nil {
		return nil, err
	}
	res := NewResolver(repo)

	outcomes := make([]Outcome, len(entries))
	var wg sync.WaitGroup
	for i, entry := range entries {
		oc := Outcome{Entry: entry, ID: SourceID(entry)}
		stamped := false
		if ts, ok := stamps[oc.ID]; ok && ts > 0 && now.Unix() >= ts {
			stamped = true
			oc.Age = time.Duration(now.Unix()-ts) * time.Second
		}
		switch {
		case opts.Mode == ModeNever:
			oc.Status, oc.Reason, oc.Detail = StatusSkipped, ReasonNever, "--sources=never"
			outcomes[i] = oc
			continue
		case opts.Mode == ModeTTL && stamped && oc.Age < opts.TTL:
			oc.Status, oc.Reason = StatusSkipped, ReasonFresh
			oc.Detail = fmt.Sprintf("captured %s ago — --sources=always to force", FormatAge(oc.Age))
			outcomes[i] = oc
			continue
		}
		wg.Add(1)
		go func(i int, oc Outcome) {
			defer wg.Done()
			outcomes[i] = dial(oc, res)
		}(i, oc)
	}
	wg.Wait()

	// SERIAL merge — ONE writer touches the store, in entry order.
	captured, skipped, failed := 0, 0, 0
	for i := range outcomes {
		oc := &outcomes[i]
		if oc.batch != nil {
			if _, _, err := st.Replace(oc.batch, false); err != nil {
				oc.Status = StatusFailed
				oc.Detail = Scrub(oc.values, err.Error())
			} else {
				oc.Nodes, oc.Edges = len(oc.batch.Nodes), len(oc.batch.Edges)
				if err := recordStamp(st.Dir, oc.ID, now.Unix()); err != nil {
					return outcomes, err
				}
			}
			oc.batch = nil
		}
		oc.values = nil // scrub material dies with the merge
		switch oc.Status {
		case StatusCaptured:
			captured++
		case StatusFailed:
			failed++
		default:
			skipped++
		}
		fmt.Fprintln(out, oc.Line())
	}
	if len(outcomes) > 0 {
		fmt.Fprintf(out, "sources: %d captured, %d skipped, %d failed\n", captured, skipped, failed)
	}
	return outcomes, nil
}

// dial resolves, routes, and captures ONE source. Everything that can carry
// a secret is scrubbed before it lands in the outcome.
func dial(oc Outcome, res *Resolver) Outcome {
	var values []string
	origins := map[string]bool{}
	lookup := func(name string) (string, bool) {
		v, origin, ok := res.Lookup(name)
		if ok {
			values = append(values, v)
			origins[origin] = true
		}
		return v, ok
	}
	expanded, missing := Expand(oc.Entry, lookup)
	oc.values = values
	oc.Origin = joinOrigins(origins)
	if len(missing) > 0 {
		oc.Status, oc.Reason = StatusSkipped, ReasonUnset
		oc.Detail = strings.Join(missing, ", ") + " not set (checked env, .ctxoptimize/.env, .env)"
		return oc
	}
	name, err := Route(expanded)
	if err != nil {
		oc.Status, oc.Detail = StatusFailed, Scrub(values, err.Error())
		return oc
	}
	var b *schema.Batch
	if c, lerr := Lookup(name); lerr == nil {
		// In-process connector (companion build, or a test stub) wins.
		b, err = safeCapture(c, expanded)
	} else if bridgeArmed {
		// Main binary: zero driver imports — exec the companion (names-only
		// argv; the child re-resolves the same env/.env ladder from repo).
		b, err = bridgeCapture(oc.Entry, res.repo)
	} else {
		oc.Status, oc.Detail = StatusFailed, Scrub(values, lerr.Error())
		return oc
	}
	if err != nil {
		oc.Status = StatusFailed
		oc.Detail = Scrub(values, err.Error()) + " — prior nodes kept"
		return oc
	}
	b.Producer = ProducerPrefix + oc.ID // identity is ours, never the connector's
	if err := b.Validate(); err != nil {
		oc.Status, oc.Detail = StatusFailed, Scrub(values, err.Error())
		return oc
	}
	oc.Status, oc.batch = StatusCaptured, b
	return oc
}

// CaptureOnly resolves and dials ONE entry without touching any store — the
// `capture` verb (the composition/debug primitive). Error text is already
// scrubbed.
func CaptureOnly(entry, repo string) (*schema.Batch, error) {
	oc := dial(Outcome{Entry: entry, ID: SourceID(entry)}, NewResolver(repo))
	if oc.Status != StatusCaptured {
		return nil, fmt.Errorf("source %s: %s", oc.ID, oc.Detail)
	}
	return oc.batch, nil
}

// safeCapture wraps a connector dial so a panicking connector becomes a
// failed outcome (scrubbed by the caller), never a crashed run.
func safeCapture(c Connector, url string) (b *schema.Batch, err error) {
	defer func() {
		if r := recover(); r != nil {
			b, err = nil, fmt.Errorf("connector panic: %v", r)
		}
	}()
	return c.Capture(context.Background(), url)
}

func joinOrigins(set map[string]bool) string {
	if len(set) == 0 {
		return ""
	}
	out := make([]string, 0, len(set))
	for o := range set {
		out = append(out, o)
	}
	sort.Strings(out)
	return strings.Join(out, "+")
}

// Line renders the one-line human summary for this outcome (M4): origin
// names only, staleness ages, never a value.
func (oc Outcome) Line() string {
	from := ""
	if oc.Origin != "" {
		from = " ← " + oc.Origin
	}
	switch oc.Status {
	case StatusCaptured:
		return fmt.Sprintf("source %s%s: captured (%d nodes, %d edges)", oc.ID, from, oc.Nodes, oc.Edges)
	case StatusFailed:
		return fmt.Sprintf("source %s%s: FAILED (%s)", oc.ID, from, oc.Detail)
	default:
		return fmt.Sprintf("source %s: skipped (%s)", oc.ID, oc.Detail)
	}
}

// StrictError folds outcomes under --strict: unset-var skips and failures
// become one error (CI gate). TTL/never skips stay clean — they are policy,
// not problems.
func StrictError(outcomes []Outcome) error {
	var bad []string
	for _, oc := range outcomes {
		if oc.Status == StatusFailed || (oc.Status == StatusSkipped && oc.Reason == ReasonUnset) {
			bad = append(bad, oc.ID)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("--strict: %d source(s) not captured: %s", len(bad), strings.Join(bad, ", "))
}

// ---- reconcile (H1) ----

// Reconcile compares the store's source-namespace producers against the
// declared entries: producers no longer declared are reported (renames/
// edits cannot permanently orphan ghost schemas) and pruned only when prune
// is set. Returns the orphaned ids (sorted) and how many nodes were pruned.
func Reconcile(st *store.Store, entries []string, prune bool) (orphans []string, pruned int, err error) {
	declared := map[string]bool{}
	for _, e := range entries {
		declared[ProducerPrefix+SourceID(e)] = true
	}
	nodes, err := st.Nodes()
	if err != nil {
		return nil, 0, err
	}
	orphanSet := map[string]int{}
	for _, n := range nodes {
		p := n.Metadata["producer"]
		if strings.HasPrefix(p, ProducerPrefix) && !declared[p] {
			orphanSet[strings.TrimPrefix(p, ProducerPrefix)]++
		}
	}
	for id := range orphanSet {
		orphans = append(orphans, id)
	}
	sort.Strings(orphans)
	if !prune {
		return orphans, 0, nil
	}
	for _, id := range orphans {
		count := orphanSet[id]
		if _, _, err := st.Replace(&schema.Batch{Producer: ProducerPrefix + id}, true); err != nil {
			return orphans, pruned, err
		}
		pruned += count
	}
	// A pruned source's freshness stamp goes with it — status must not keep
	// reporting a ghost as "captured Xh ago".
	if len(orphans) > 0 {
		if err := removeStamps(st.Dir, orphans); err != nil {
			return orphans, pruned, err
		}
	}
	return orphans, pruned, nil
}

// ---- per-source freshness stamps (H5) ----
//
// <store>/sources.json records the sanitized id + last-captured unix time
// per source — the sources' own freshness axis, sibling to source.json (git
// provenance). Sorted by id, newline-terminated, atomic rename: git-diffable
// like every store artifact. Machine-local (excluded from the manifest).

type stamp struct {
	ID           string `json:"id"`
	CapturedUnix int64  `json:"captured_unix"`
}

func stampsPath(storeDir string) string { return filepath.Join(storeDir, "sources.json") }

// SourceStamps loads id → last-captured unix time (absent file → empty).
func SourceStamps(storeDir string) (map[string]int64, error) {
	data, err := os.ReadFile(stampsPath(storeDir))
	if os.IsNotExist(err) {
		return map[string]int64{}, nil
	}
	if err != nil {
		return nil, err
	}
	var list []stamp
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse sources.json: %w", err)
	}
	out := make(map[string]int64, len(list))
	for _, s := range list {
		out[s.ID] = s.CapturedUnix
	}
	return out, nil
}

func recordStamp(storeDir, id string, ts int64) error {
	m, err := SourceStamps(storeDir)
	if err != nil {
		return err
	}
	m[id] = ts
	return writeStamps(storeDir, m)
}

func removeStamps(storeDir string, ids []string) error {
	m, err := SourceStamps(storeDir)
	if err != nil {
		return err
	}
	if len(m) == 0 {
		return nil
	}
	for _, id := range ids {
		delete(m, id)
	}
	return writeStamps(storeDir, m)
}

func writeStamps(storeDir string, m map[string]int64) error {
	list := make([]stamp, 0, len(m))
	for k, v := range m {
		list = append(list, stamp{ID: k, CapturedUnix: v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := stampsPath(storeDir) + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, stampsPath(storeDir))
}

// FormatAge renders a duration as a compact staleness age: 45s, 12m, 3h, 180d.
func FormatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
