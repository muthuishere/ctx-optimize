package store

import (
	"encoding/json"
	"testing"
)

// A committed config may spell autosync as a bool OR a string; both must land on
// the same canonical mode (ADR 2026-07-24-lazy-autosync, owner decision).
func TestAutosyncModeUnmarshal(t *testing.T) {
	cases := map[string]AutosyncMode{
		`true`:      AutosyncLazy,  // bool true = on = lazy
		`false`:     AutosyncOff,   // bool false = off
		`"true"`:    AutosyncLazy,  // string "true" = lazy
		`"false"`:   AutosyncOff,   //
		`"lazy"`:    AutosyncLazy,  //
		`"block"`:   AutosyncBlock, //
		`"off"`:     AutosyncOff,   //
		`"ON"`:      AutosyncLazy,  // case-insensitive
		`"garbage"`: AutosyncOff,   // unknown → off (fail-safe)
		`null`:      AutosyncOff,   //
	}
	for raw, want := range cases {
		var v struct {
			Autosync AutosyncMode `json:"autosync"`
		}
		if err := json.Unmarshal([]byte(`{"autosync":`+raw+`}`), &v); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if v.Autosync != want {
			t.Errorf("autosync=%s → %q, want %q", raw, v.Autosync, want)
		}
	}
	// Absent key = zero value "", which callers treat as off.
	var none struct {
		Autosync AutosyncMode `json:"autosync"`
	}
	if err := json.Unmarshal([]byte(`{}`), &none); err != nil || none.Autosync != "" {
		t.Fatalf("absent key: got %q err %v", none.Autosync, err)
	}
}

func TestParseAutosync(t *testing.T) {
	for in, want := range map[string]AutosyncMode{
		"lazy": AutosyncLazy, "block": AutosyncBlock, "off": AutosyncOff,
		"true": AutosyncLazy, "false": AutosyncOff, "": AutosyncOff,
		"  LAZY  ": AutosyncLazy, "nonsense": AutosyncOff,
	} {
		if got := ParseAutosync(in); got != want {
			t.Errorf("ParseAutosync(%q)=%q, want %q", in, got, want)
		}
	}
}
