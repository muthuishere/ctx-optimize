package store

// The graph's CONTENT fingerprint — the one definition of "changed" (ADR
// 2026-08-15-index-dies-on-a-noop-gather).
//
// The lookup index used to key its fail-safe header on the graph file's
// size+mtime, while the rebuild guard keyed on whether the node set moved. The
// store rewrites nodes.ndjson/edges.ndjson on EVERY gather, so a gather that
// added nothing bumped the mtime, the reader correctly declared the index
// stale, and the guard correctly decided there was nothing to rebuild. Both
// halves right, the join impossible: `card bio_split` on the linux kernel went
// 6ms -> 1,629ms after the first incremental gather, and stayed there.
//
// The fix is to key the header on the content hash instead. It has to be
// verifiable in MICROSECONDS on the read path — rehashing a 600MB graph per
// lookup would cost more than the full scan it saves — so the hash is computed
// once by the writer (free: it is taken from the same bytes as they stream to
// the temp file) and recorded in a stamp file under graph/index/, together with
// the size+mtime of the inode it describes.
//
// Reading is therefore: cheap stat to prove the stamp still describes this
// file, then compare the recorded hash with the index header. That keeps the
// old fail-safe property EXACTLY: a stamp that is missing, stale, truncated
// or unparseable yields "not current" and the caller falls back to the full
// scan. Nothing here can make an answer wrong; it can only make one fast.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The stamp lives INSIDE graph/index/, as graph/index/<name>.stamp, because it
// is part of the index and not part of the graph: it is machine-local, it is
// derived, it carries this machine's inode mtime, and it is rebuilt on demand.
// graph/index/ is already excluded from the manifest, from remote transport and
// from the byte-identity gate for exactly those reasons — putting the stamp
// beside the graph instead added a NEW non-deterministic file to the store's
// committed artifact set, and the `--jobs 1 vs 8` determinism test caught it
// within the hour (the mtime field differs between two runs, as it must).
// Nothing outside graph/index/ changes.
const stampSuffix = ".stamp"

// contentStamp is the stamp payload: a file's content hash plus the inode
// facts that prove the stamp still belongs to THAT file.
type contentStamp struct {
	Size  int64
	MTime int64
	Hash  string
}

// stampPath maps <store>/graph/nodes.ndjson → <store>/graph/index/nodes.ndjson.stamp.
func stampPath(path string) string {
	return filepath.Join(filepath.Dir(path), "index", filepath.Base(path)+stampSuffix)
}

func (c contentStamp) line() string {
	return fmt.Sprintf("size=%d mtime=%d sha256=%s\n", c.Size, c.MTime, c.Hash)
}

// writeContentStamp records a stamp atomically, INTO the index directory. It
// does not create that directory: no index, nothing to stamp — BuildIndex makes
// the directory and computes the hash itself on the first build. Best-effort by
// contract, since a failure here costs speed (the index reads as stale), never
// correctness.
func writeContentStamp(path string, c contentStamp) error {
	p := stampPath(path)
	if _, err := os.Stat(filepath.Dir(p)); err != nil {
		return err
	}
	f, err := createTemp(p)
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.WriteString(c.line()); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// readContentStamp returns the recorded content hash ONLY if the stamp still
// matches the file on disk. It NEVER hashes: this runs on every lookup.
//
// Two gathers racing on one store rename their own inodes over each other; the
// loser's stamp then describes an inode that is no longer the graph, and the
// size+mtime check catches it (mtime is nanosecond-unique per write). The
// answer is "not current", i.e. a full scan — slow, never wrong.
func readContentStamp(path string) (contentStamp, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return contentStamp{}, false
	}
	data, err := os.ReadFile(stampPath(path))
	if err != nil {
		return contentStamp{}, false
	}
	c, ok := parseContentStamp(string(data))
	if !ok {
		return contentStamp{}, false
	}
	if c.Size != st.Size() || c.MTime != st.ModTime().UnixNano() {
		return contentStamp{}, false
	}
	return c, true
}

func parseContentStamp(s string) (contentStamp, bool) {
	var c contentStamp
	seen := 0
	for _, fld := range strings.Fields(strings.TrimSpace(s)) {
		k, v, found := strings.Cut(fld, "=")
		if !found {
			return contentStamp{}, false
		}
		switch k {
		case "size":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return contentStamp{}, false
			}
			c.Size, seen = n, seen+1
		case "mtime":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return contentStamp{}, false
			}
			c.MTime, seen = n, seen+1
		case "sha256":
			if _, err := hex.DecodeString(v); err != nil || v == "" {
				return contentStamp{}, false
			}
			c.Hash, seen = v, seen+1
		}
	}
	if seen != 3 {
		return contentStamp{}, false
	}
	return c, true
}

// ensureContentStamp returns the file's stamp, computing and recording it when
// the stamp is missing or stale. WRITE side only — a graph written by an
// older binary, or restored by a remote pull, gets its hash here (one read of
// the file, paid by the rebuild that is already scanning it), so the index can
// be keyed on content from the very first build.
func ensureContentStamp(path string) (contentStamp, error) {
	if c, ok := readContentStamp(path); ok {
		return c, nil
	}
	h, size, err := hashFile(path)
	if err != nil {
		return contentStamp{}, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return contentStamp{}, err
	}
	c := contentStamp{Size: size, MTime: st.ModTime().UnixNano(), Hash: h}
	if err := writeContentStamp(path, c); err != nil {
		return contentStamp{}, err
	}
	return c, nil
}
