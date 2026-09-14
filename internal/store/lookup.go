package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// Lookup is a read session over the index: every file is opened and every
// staleness stamp checked ONCE, then any number of point lookups reuse them.
//
// Store.NodeByID and Store.EdgesTo are right for a handful of lookups (card,
// a small blast radius) but pay a stat, a stamp read, two opens and a fresh
// 64 KB reader on EVERY call. A walk that visits tens of thousands of nodes
// spends its time there, not in the search: on linux, `affected kfree
// --include-ambiguous` (71,519 rows) took 7.2 s through per-call lookups
// against 3.4 s for a full load; through a session (and readLineAround's
// doubling probe window) it takes 1.7 s.
//
// Answers are identical to Store.NodeByID / Store.EdgesTo on a current index
// (same binary search, same file-order offsets, same decoder), which is the
// only state OpenLookup accepts. Open handles also pin the files: a gather
// that renames new graph files into place mid-walk does not mix two graphs
// into one answer.
type Lookup struct {
	ids, byTgt   *os.File
	idsBody      int64
	idsSize      int64
	tgtBody      int64
	tgtSize      int64
	nodes, edges *os.File
	nr, er       *bufio.Reader
}

// OpenLookup returns a session, or ok=false when the ids or edges-by-target
// index is absent or stale — callers then take the full-scan path.
func (s *Store) OpenLookup() (*Lookup, bool) {
	l := &Lookup{}
	var ok bool
	if l.ids, l.idsBody, l.idsSize, ok = openIndex(filepath.Join(s.indexDir(), idsIndex), s.nodesPath()); !ok {
		return nil, false
	}
	if l.byTgt, l.tgtBody, l.tgtSize, ok = openIndex(filepath.Join(s.indexDir(), edgesByTgt), s.edgesPath()); !ok {
		l.Close()
		return nil, false
	}
	var err error
	if l.nodes, err = os.Open(s.nodesPath()); err != nil {
		l.Close()
		return nil, false
	}
	if l.edges, err = os.Open(s.edgesPath()); err != nil {
		l.Close()
		return nil, false
	}
	// Small on purpose: the reader is reset after every seek, so a 64 KB buffer
	// refills 64 KB to decode one line. ReadBytes grows past the buffer when a
	// line is longer, so the size bounds cost, never correctness.
	l.nr = bufio.NewReaderSize(l.nodes, 8*1024)
	l.er = bufio.NewReaderSize(l.edges, 8*1024)
	return l, true
}

// Close releases every handle; safe on a partially opened session.
func (l *Lookup) Close() {
	for _, f := range []*os.File{l.ids, l.byTgt, l.nodes, l.edges} {
		if f != nil {
			f.Close()
		}
	}
}

// NodeByID returns the node with exactly this id, or nil.
func (l *Lookup) NodeByID(id string) (*schema.Node, error) {
	offs, found := lookupOffsets(l.ids, l.idsBody, l.idsSize, id)
	if !found {
		return nil, nil
	}
	got, err := readAtOpen[schema.Node](l.nodes, l.nr, offs)
	if err != nil || len(got) == 0 {
		return nil, err
	}
	return &got[0], nil
}

// EdgesTo returns every edge whose target is exactly id, in edge-file order.
func (l *Lookup) EdgesTo(id string) ([]schema.Edge, error) {
	offs, found := lookupOffsets(l.byTgt, l.tgtBody, l.tgtSize, id)
	if !found {
		return nil, nil
	}
	return readAtOpen[schema.Edge](l.edges, l.er, offs)
}

// readAtOpen is readAt over an already open file and a reused reader.
func readAtOpen[T any](f *os.File, r *bufio.Reader, offs []int64) ([]T, error) {
	if len(offs) == 0 {
		return nil, nil
	}
	out := make([]T, 0, len(offs))
	for _, off := range offs {
		if _, err := f.Seek(off, 0); err != nil {
			return nil, err
		}
		r.Reset(f)
		line, err := r.ReadBytes('\n')
		if err != nil && len(line) == 0 {
			return nil, err
		}
		var v T
		if err := json.Unmarshal(bytes.TrimSpace(line), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
