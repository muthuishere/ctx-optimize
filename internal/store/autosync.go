package store

import (
	"bytes"
	"encoding/json"
	"strings"
)

// AutosyncMode is the config value for lazy code re-sync on a read verb (ADR
// 2026-07-24-lazy-autosync, lever 3). Canonical values: "off" (default),
// "lazy", "block". It unmarshals from BOTH a JSON string and a JSON bool so a
// committed config can say any of:
//
//	"autosync": false | "off"      → off   (also: key absent)
//	"autosync": true  | "true" | "lazy"  → lazy  (answer now, resync in background)
//	"autosync": "block"            → block (resync inline first, then answer)
//
// Unknown/garbage → off (fail-safe, deterministic-by-default).
type AutosyncMode string

const (
	AutosyncOff   AutosyncMode = "off"
	AutosyncLazy  AutosyncMode = "lazy"
	AutosyncBlock AutosyncMode = "block"
)

// ParseAutosync normalizes a free-form string (config value or the
// CTX_OPTIMIZE_AUTOSYNC env var) to a canonical mode; anything unrecognized is
// off.
func ParseAutosync(s string) AutosyncMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "lazy", "true", "on", "1", "yes":
		return AutosyncLazy
	case "block":
		return AutosyncBlock
	default: // "off", "false", "0", "no", "", garbage
		return AutosyncOff
	}
}

// UnmarshalJSON accepts a JSON bool or string (see the type doc). Absent keys
// keep the zero value "" — callers treat "" as off.
func (m *AutosyncMode) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*m = AutosyncOff
		return nil
	}
	// Bool form: true → lazy, false → off.
	if string(data) == "true" {
		*m = AutosyncLazy
		return nil
	}
	if string(data) == "false" {
		*m = AutosyncOff
		return nil
	}
	// String form (incl. "true"/"false"/"lazy"/"block"/"off").
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*m = ParseAutosync(s)
	return nil
}
