// Package audit is the append-only change log for every mutation door —
// dashboard endpoints and CLI verbs route through the same writer, so a human
// can always answer "who changed what, when" from one plain file:
//
//	<store-root>/audit.ndjson — one JSON line per mutation, keys sorted
//	(struct field order is alphabetical on purpose), git-diffable like every
//	other store artifact. ts, actor (dashboard|cli), action, target (usually
//	a file path), before_hash → after_hash (sha256 of the target file, when
//	it is a file).
//
// No secrets ever land here: targets are paths and hashes, never values.
package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

const FileName = "audit.ndjson"

// Line is one recorded mutation. Field order = alphabetical JSON keys, so the
// emitted lines stay byte-stable and sorted-field like the rest of the store.
type Line struct {
	Action     string `json:"action"`
	Actor      string `json:"actor"`
	AfterHash  string `json:"after_hash,omitempty"`
	BeforeHash string `json:"before_hash,omitempty"`
	Target     string `json:"target"`
	TS         string `json:"ts"`
}

func Path(storeRoot string) string { return filepath.Join(storeRoot, FileName) }

// Append writes one line to <storeRoot>/audit.ndjson, creating the file (and
// the store root) if needed. TS is stamped UTC RFC3339 when the caller left
// it empty. Append-only: the file is never rewritten.
func Append(storeRoot string, l Line) error {
	if l.TS == "" {
		l.TS = time.Now().UTC().Format(time.RFC3339)
	}
	if err := os.MkdirAll(storeRoot, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(Path(storeRoot), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// List reads every recorded line, oldest first. Absent file → empty, never an
// error, and nothing is created (read paths must not create store dirs).
func List(storeRoot string) ([]Line, error) {
	f, err := os.Open(Path(storeRoot))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Line
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var l Line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			continue // one garbled line never hides the rest of the history
		}
		out = append(out, l)
	}
	return out, sc.Err()
}

// FileHash returns the sha256 hex of a file, or "" when it does not exist —
// the before/after fingerprint mutations record around a file edit.
func FileHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
