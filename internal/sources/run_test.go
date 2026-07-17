package sources

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/schema"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

// stubConn is the Stage-1 fake connector: behavior keyed by the dialed URL's
// host segment (okhost → batch, badhost → error echoing the full URL,
// panichost → panic echoing the full URL). Real connectors land in Stage 2.
type stubConn struct {
	scheme string
	dials  atomic.Int64
	nodes  func(url string) []schema.Node
}

func (s *stubConn) Scheme() string { return s.scheme }
func (s *stubConn) Params() []Param {
	return []Param{
		{Name: "user:pass userinfo", Desc: "credentials (values via $VAR)", Cred: true},
		{Name: "tls_ca", Desc: "CA certificate PATH", Cred: false},
	}
}
func (s *stubConn) Example() string { return s.scheme + "://$USER:$PASS@host:1234/db" }

func (s *stubConn) Capture(_ context.Context, url string) (*schema.Batch, error) {
	s.dials.Add(1)
	switch {
	case strings.Contains(url, "badhost"):
		// A hostile/chatty driver: echoes the FULL resolved URL, password
		// included — exactly what Scrub must catch.
		return nil, fmt.Errorf("dial %s: authentication failed", url)
	case strings.Contains(url, "panichost"):
		panic("connector exploded on " + url)
	}
	time.Sleep(5 * time.Millisecond) // let goroutines overlap under -race
	nodes := []schema.Node{{
		ID: s.scheme + "://okhost/db/t1", Label: "t1", Kind: "table",
		FileType: "schema", Source: s.scheme + "://okhost/db/t1",
	}}
	if s.nodes != nil {
		nodes = s.nodes(url)
	}
	return &schema.Batch{Producer: "stub-should-be-overridden", Nodes: nodes}, nil
}

func newStub(t *testing.T, scheme string) *stubConn {
	t.Helper()
	c := &stubConn{scheme: scheme}
	Register(c)
	t.Cleanup(func() { Unregister(scheme) })
	return c
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir(), "mod")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunParallelCaptureSerialMerge(t *testing.T) {
	stub := newStub(t, "stub")
	stub.nodes = func(url string) []schema.Node {
		// One distinct node per source (db name = the path tail).
		db := url[strings.LastIndex(url, "/")+1:]
		id := "stub://okhost/" + db
		return []schema.Node{{ID: id, Label: db, Kind: "database", FileType: "schema", Source: id}}
	}
	st := openStore(t)
	var entries []string
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("STUB_URL_%d", i)
		t.Setenv(name, fmt.Sprintf("stub://u:pw@okhost:1/db%d", i))
		entries = append(entries, name)
	}
	var out bytes.Buffer
	outcomes, err := Run(entries, t.TempDir(), st, Options{Mode: ModeAlways}, &out)
	if err != nil {
		t.Fatal(err)
	}
	for i, oc := range outcomes {
		if oc.Status != StatusCaptured {
			t.Fatalf("outcome[%d] = %+v, want captured", i, oc)
		}
		if oc.Origin != OriginEnv {
			t.Errorf("outcome[%d] origin = %q, want env", i, oc.Origin)
		}
	}
	nodes, err := st.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 8 {
		t.Fatalf("store has %d nodes, want 8", len(nodes))
	}
	producers := map[string]bool{}
	for _, n := range nodes {
		producers[n.Metadata["producer"]] = true
	}
	if !producers["source:STUB_URL_0"] || !producers["source:STUB_URL_7"] {
		t.Errorf("producer identities wrong: %v", producers)
	}
	if !strings.Contains(out.String(), "8 captured, 0 skipped, 0 failed") {
		t.Errorf("summary line: %s", out.String())
	}
	if !strings.Contains(out.String(), "STUB_URL_0 ← env") {
		t.Errorf("origin missing from line: %s", out.String())
	}
}

func TestRunOutcomesSkipFailStrict(t *testing.T) {
	newStub(t, "stub")
	st := openStore(t)
	const secret = "hunter2xyzq"
	t.Setenv("GOOD_URL", "stub://alice:"+secret+"@okhost:1/db")
	t.Setenv("BAD_URL", "stub://alice:"+secret+"@badhost:1/db")
	t.Setenv("PANIC_URL", "stub://alice:"+secret+"@panichost:1/db")
	entries := []string{"GOOD_URL", "BAD_URL", "PANIC_URL", "UNSET_URL"}
	var out bytes.Buffer
	outcomes, err := Run(entries, t.TempDir(), st, Options{Mode: ModeAlways}, &out)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Outcome{}
	for _, oc := range outcomes {
		byID[oc.ID] = oc
	}
	if byID["GOOD_URL"].Status != StatusCaptured {
		t.Errorf("GOOD_URL: %+v", byID["GOOD_URL"])
	}
	if oc := byID["BAD_URL"]; oc.Status != StatusFailed || !strings.Contains(oc.Detail, "prior nodes kept") {
		t.Errorf("BAD_URL: %+v", oc)
	}
	if oc := byID["PANIC_URL"]; oc.Status != StatusFailed || !strings.Contains(oc.Detail, "panic") {
		t.Errorf("PANIC_URL: %+v", oc)
	}
	if oc := byID["UNSET_URL"]; oc.Status != StatusSkipped || oc.Reason != ReasonUnset ||
		!strings.Contains(oc.Detail, "UNSET_URL not set") {
		t.Errorf("UNSET_URL: %+v", oc)
	}
	// Choke B: the failure/panic text echoed the secret — outputs must not.
	all := out.String()
	for _, oc := range outcomes {
		all += oc.Detail + oc.Line()
	}
	if strings.Contains(all, secret) {
		t.Fatalf("secret leaked into output:\n%s", all)
	}
	// Strict folds unset+failed into an error; fresh/never skips would not.
	if err := StrictError(outcomes); err == nil ||
		!strings.Contains(err.Error(), "BAD_URL") || !strings.Contains(err.Error(), "UNSET_URL") {
		t.Errorf("StrictError = %v", err)
	}
	if err := StrictError([]Outcome{{Status: StatusSkipped, Reason: ReasonFresh}, {Status: StatusCaptured}}); err != nil {
		t.Errorf("StrictError on fresh skip = %v, want nil", err)
	}
}

func TestRunFailureKeepsPriorNodes(t *testing.T) {
	newStub(t, "stub")
	st := openStore(t)
	t.Setenv("FLAKY_URL", "stub://u:pw@okhost:1/db")
	if _, err := Run([]string{"FLAKY_URL"}, t.TempDir(), st, Options{Mode: ModeAlways}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Nodes()
	if len(before) == 0 {
		t.Fatal("no nodes captured")
	}
	// Now the DB is down (badhost): failed keeps prior nodes.
	t.Setenv("FLAKY_URL", "stub://u:pw@badhost:1/db")
	outcomes, err := Run([]string{"FLAKY_URL"}, t.TempDir(), st, Options{Mode: ModeAlways}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if outcomes[0].Status != StatusFailed {
		t.Fatalf("outcome: %+v", outcomes[0])
	}
	after, _ := st.Nodes()
	if len(after) != len(before) {
		t.Errorf("failed capture changed the store: %d → %d nodes", len(before), len(after))
	}
}

func TestRunProducerReplace(t *testing.T) {
	stub := newStub(t, "stub")
	emit := []string{"t1", "t2"}
	stub.nodes = func(string) []schema.Node {
		var out []schema.Node
		for _, n := range emit {
			id := "stub://okhost/db/" + n
			out = append(out, schema.Node{ID: id, Label: n, Kind: "table", FileType: "schema", Source: id})
		}
		return out
	}
	st := openStore(t)
	t.Setenv("DB_URL", "stub://u:pw@okhost:1/db")
	if _, err := Run([]string{"DB_URL"}, t.TempDir(), st, Options{Mode: ModeAlways}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	// Re-capture: t2 dropped — producer-scoped Replace must prune it.
	emit = []string{"t1"}
	if _, err := Run([]string{"DB_URL"}, t.TempDir(), st, Options{Mode: ModeAlways}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	nodes, _ := st.Nodes()
	if len(nodes) != 1 || nodes[0].Label != "t1" {
		t.Errorf("replace did not prune: %+v", nodes)
	}
}

func TestRunTTL(t *testing.T) {
	stub := newStub(t, "stub")
	st := openStore(t)
	t.Setenv("TTL_URL", "stub://u:pw@okhost:1/db")
	now := time.Now()
	if _, err := Run([]string{"TTL_URL"}, t.TempDir(), st, Options{Mode: ModeAlways, Now: now}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if stub.dials.Load() != 1 {
		t.Fatalf("dials = %d", stub.dials.Load())
	}
	// Within TTL: default mode skips as fresh, with the age in the line.
	var out bytes.Buffer
	outcomes, err := Run([]string{"TTL_URL"}, t.TempDir(), st, Options{Now: now.Add(2 * time.Hour)}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if outcomes[0].Status != StatusSkipped || outcomes[0].Reason != ReasonFresh {
		t.Fatalf("within TTL: %+v", outcomes[0])
	}
	if stub.dials.Load() != 1 {
		t.Errorf("fresh source was dialed anyway")
	}
	if !strings.Contains(out.String(), "captured 2h ago") {
		t.Errorf("age missing: %s", out.String())
	}
	// --sources=always forces the dial even when fresh.
	if _, err := Run([]string{"TTL_URL"}, t.TempDir(), st, Options{Mode: ModeAlways, Now: now.Add(2 * time.Hour)}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if stub.dials.Load() != 2 {
		t.Errorf("--sources=always did not dial")
	}
	// Past TTL: default mode dials again.
	if _, err := Run([]string{"TTL_URL"}, t.TempDir(), st, Options{Now: now.Add(50 * time.Hour)}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if stub.dials.Load() != 3 {
		t.Errorf("stale source was not re-dialed")
	}
	// --sources=never skips everything, dial-free.
	outcomes, err = Run([]string{"TTL_URL"}, t.TempDir(), st, Options{Mode: ModeNever, Now: now.Add(100 * time.Hour)}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if outcomes[0].Status != StatusSkipped || outcomes[0].Reason != ReasonNever || stub.dials.Load() != 3 {
		t.Errorf("--sources=never: %+v dials=%d", outcomes[0], stub.dials.Load())
	}
}

func TestRunStampsOnSuccessOnly(t *testing.T) {
	newStub(t, "stub")
	st := openStore(t)
	t.Setenv("OK_URL", "stub://u:pw@okhost:1/db")
	t.Setenv("DOWN_URL", "stub://u:pw@badhost:1/db")
	if _, err := Run([]string{"OK_URL", "DOWN_URL"}, t.TempDir(), st, Options{Mode: ModeAlways}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	stamps, err := SourceStamps(st.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stamps["OK_URL"]; !ok {
		t.Error("no stamp for successful capture")
	}
	if _, ok := stamps["DOWN_URL"]; ok {
		t.Error("failed capture got a freshness stamp")
	}
}

func TestReconcile(t *testing.T) {
	newStub(t, "stub")
	st := openStore(t)
	t.Setenv("OLD_URL", "stub://u:pw@okhost:1/db")
	if _, err := Run([]string{"OLD_URL"}, t.TempDir(), st, Options{Mode: ModeAlways}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	// Entry renamed away in config: report first, prune only on confirm.
	orphans, pruned, err := Reconcile(st, []string{"NEW_URL"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || orphans[0] != "OLD_URL" || pruned != 0 {
		t.Fatalf("report pass: orphans=%v pruned=%d", orphans, pruned)
	}
	if nodes, _ := st.Nodes(); len(nodes) == 0 {
		t.Fatal("report pass must not prune")
	}
	orphans, pruned, err = Reconcile(st, []string{"NEW_URL"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || pruned == 0 {
		t.Fatalf("prune pass: orphans=%v pruned=%d", orphans, pruned)
	}
	if nodes, _ := st.Nodes(); len(nodes) != 0 {
		t.Errorf("prune left nodes: %+v", nodes)
	}
	if stamps, _ := SourceStamps(st.Dir); len(stamps) != 0 {
		t.Errorf("prune left freshness stamps: %v", stamps)
	}
	// Declared producers are never orphans.
	t.Setenv("NEW_URL", "stub://u:pw@okhost:1/db")
	if _, err := Run([]string{"NEW_URL"}, t.TempDir(), st, Options{Mode: ModeAlways}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if orphans, _, _ := Reconcile(st, []string{"NEW_URL"}, false); len(orphans) != 0 {
		t.Errorf("declared source reported orphan: %v", orphans)
	}
}

func TestCaptureOnly(t *testing.T) {
	newStub(t, "stub")
	const secret = "hunter2xyzq"
	t.Setenv("CAP_URL", "stub://alice:"+secret+"@okhost:1/db")
	b, err := CaptureOnly("CAP_URL", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b.Producer != "source:CAP_URL" || len(b.Nodes) == 0 {
		t.Fatalf("batch: %+v", b)
	}
	// Failure path: pre-scrubbed error, no store involved at all.
	t.Setenv("CAP_URL", "stub://alice:"+secret+"@badhost:1/db")
	if _, err := CaptureOnly("CAP_URL", t.TempDir()); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("CaptureOnly failure err = %v", err)
	}
	// Unset: skip reason as the error.
	if _, err := CaptureOnly("NOPE_URL", t.TempDir()); err == nil || !strings.Contains(err.Error(), "not set") {
		t.Fatalf("unset err = %v", err)
	}
}

func TestHelpCard(t *testing.T) {
	newStub(t, "stub")
	card, err := HelpCard("stub")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"stub://$USER:$PASS@host:1234/db", // value format from Example()
		"user:pass userinfo",              // param table from Params()
		"[credential — use a $VAR",        // cred marking
		"percent-encoded",                 // the %2F hint
		"export MY_STUB_URL=",             // export example
		"ctx-optimize add MY_STUB_URL",    // paste-ready add
	} {
		if !strings.Contains(card, want) {
			t.Errorf("help card missing %q:\n%s", want, card)
		}
	}
	if _, err := HelpCard("ftp"); err == nil {
		t.Error("HelpCard(ftp) should fail with the supported set")
	}
}

func TestFormatAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{12 * time.Minute, "12m"},
		{3 * time.Hour, "3h"},
		{180 * 24 * time.Hour, "180d"},
	}
	for _, c := range cases {
		if got := FormatAge(c.d); got != c.want {
			t.Errorf("FormatAge(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
