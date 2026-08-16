package freshness

import "testing"

func TestEvaluate(t *testing.T) {
	const now = int64(1000)
	tests := []struct {
		name        string
		rec         Source
		curHead     string
		curHeadUnix int64
		wantState   State
		wantAge     int64
		wantBehind  int64
	}{
		{
			name:      "equal heads → fresh",
			rec:       Source{Head: "abc", HeadUnix: 100, AddedUnix: 200},
			curHead:   "abc",
			wantState: Fresh,
			wantAge:   800,
		},
		{
			name:        "differing heads → stale with behind",
			rec:         Source{Head: "abc", HeadUnix: 100, AddedUnix: 200},
			curHead:     "def",
			curHeadUnix: 400,
			wantState:   Stale,
			wantAge:     800,
			wantBehind:  300,
		},
		{
			name:      "no current head → unknown",
			rec:       Source{Head: "abc", HeadUnix: 100, AddedUnix: 200},
			curHead:   "",
			wantState: Unknown,
			wantAge:   800,
		},
		{
			name:      "no recorded head → unknown",
			rec:       Source{Head: "", AddedUnix: 200},
			curHead:   "def",
			wantState: Unknown,
			wantAge:   800,
		},
		{
			name:      "stale but no head times → no behind",
			rec:       Source{Head: "abc", AddedUnix: 200},
			curHead:   "def",
			wantState: Stale,
			wantAge:   800,
		},
		{
			name:      "added in the future → age clamped to zero",
			rec:       Source{Head: "abc", AddedUnix: 5000},
			curHead:   "abc",
			wantState: Fresh,
			wantAge:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.rec, tt.curHead, tt.curHeadUnix, now)
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q", got.State, tt.wantState)
			}
			if got.AgeSeconds != tt.wantAge {
				t.Errorf("AgeSeconds = %d, want %d", got.AgeSeconds, tt.wantAge)
			}
			if got.BehindSecond != tt.wantBehind {
				t.Errorf("BehindSecond = %d, want %d", got.BehindSecond, tt.wantBehind)
			}
			if got.StoreHead != tt.rec.Head || got.CurrentHead != tt.curHead {
				t.Errorf("heads not echoed: store=%q cur=%q", got.StoreHead, got.CurrentHead)
			}
		})
	}
}

func TestOverallAndExitCode(t *testing.T) {
	tests := []struct {
		name     string
		reports  []Report
		want     State
		wantCode int
	}{
		{"empty → unknown", nil, Unknown, 2},
		{"all fresh → fresh", []Report{{State: Fresh}, {State: Fresh}}, Fresh, 0},
		{"any stale wins → stale", []Report{{State: Fresh}, {State: Stale}, {State: Unknown}}, Stale, 1},
		{"fresh+unknown → unknown", []Report{{State: Fresh}, {State: Unknown}}, Unknown, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Overall(tt.reports)
			if got != tt.want {
				t.Errorf("Overall = %q, want %q", got, tt.want)
			}
			if code := ExitCode(got); code != tt.wantCode {
				t.Errorf("ExitCode(%q) = %d, want %d", got, code, tt.wantCode)
			}
		})
	}
}

// Dated is the third kind of out-of-date, and it has to be quiet unless it is
// certain: an old store that predates the record, or a caller that cannot load
// rules, must never be accused. A false DATED sends someone to rebuild a store
// that was fine.
func TestDatedIsConservativeOnBothSides(t *testing.T) {
	for _, tc := range []struct {
		name, stored, current string
		want                  bool
	}{
		{"same vocabulary", "abc123", "abc123", false},
		{"vocabulary moved", "abc123", "def456", true},
		{"store predates the record", "", "def456", false},
		{"rules could not be loaded", "abc123", "", false},
		{"neither known", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Source{Path: "/x", RulesSig: tc.stored}
			if got := s.Dated(tc.current); got != tc.want {
				t.Errorf("Dated(%q) with stored %q = %v, want %v", tc.current, tc.stored, got, tc.want)
			}
		})
	}
}

// Dated is orthogonal to the git verdict: a store can sit exactly at HEAD and
// still answer in a vocabulary the binary has stopped using. Folding it into
// State would have made "fresh" mean two different things to a hook.
func TestDatedIsNotAState(t *testing.T) {
	src := Source{Path: "/x", Head: "abc", HeadUnix: 100, AddedUnix: 100, RulesSig: "old"}
	r := Evaluate(src, "abc", 100, 200)
	if r.State != Fresh {
		t.Fatalf("state = %s, want fresh — the code did not move", r.State)
	}
	if !src.Dated("new") {
		t.Fatal("a fresh store with a moved vocabulary is still dated")
	}
}
