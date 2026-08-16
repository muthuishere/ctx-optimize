package store

// An access path for the graph (ADR 2026-08-05-query-at-scale).
//
// Every verb used to materialize the whole graph before it could answer about
// one symbol: `Store.Nodes()` parses all 2.85M linux records at ~3.2s, and
// reading the same bytes costs 0.12s — so ~97% of a query was deserialization
// paid before the question was even known.
//
// This file adds a derived index beside the graph: plain sorted text, one line
// per key, rebuilt from the graph like every other derived artifact. A lookup
// mmaps it, binary-searches by line, and parses ONLY the matching records.
// Measured on linux: 3.19s -> 0.0001s.
//
// Two rules make it safe, and both are load-bearing:
//
//   - FAIL SAFE. The header records the source file's CONTENT hash (see
//     contenthash.go; it was size+modtime until ADR 18). On any mismatch —
//     absent, stale, truncated, garbage — the caller falls back to the full
//     scan. The index can make an answer FAST; it can never make one WRONG.
//     Same discipline as the wiki work in 0.12.0: we may say "slower", never
//     "wrong".
//   - SETS, NEVER FIRST HITS. A key maps to every matching offset. 20.3% of
//     linux records sit under a non-unique label ("description" alone has
//     14,989), so a one-offset index would silently under-report while looking
//     complete.
import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// indexVersion is bumped whenever the on-disk format changes; a mismatched
// version fails the header check and falls back, so an old index is never read
// with new assumptions.
//
// v2 switched the header from size+mtime to the graph's content hash (ADR 18):
// a v1 index is not readable under the new rule, fails the header check, and is
// rebuilt by the next gather — which now also repairs a stale index even when
// the node set did not move.
const indexVersion = 2

const (
	labelsIndex = "labels.idx"
	idsIndex    = "ids.idx"
	edgesBySrc  = "edges-by-source.idx"
	edgesByTgt  = "edges-by-target.idx"
)

func (s *Store) indexDir() string { return filepath.Join(s.Dir, "graph", "index") }

// jsonField extracts a JSON string value by key without a full unmarshal.
//
// The naive version — take the bytes between `"key":"` and the next quote — is
// wrong twice, and the spike's equivalence run caught both on real kernel data
// (76 mismatches in 20,031 labels):
//
//   - it stops at an ESCAPED quote (\"), truncating the value;
//   - it returns ENCODED bytes, so `\t` stays backslash-t and `<` never
//     becomes '<'. 11,798 linux labels (0.414%) contain an escape.
//
// A mis-keyed index does not error. It fails to find the symbol, so callers go
// missing and blast radius shrinks — silently. So: walk the string respecting
// escapes to find the true end, and pay for decoding ONLY when one is present.
// 99.6% of keys take the zero-allocation path.
func jsonField(line []byte, key string) string {
	needle := []byte(`"` + key + `":"`)
	k := bytes.Index(line, needle)
	if k < 0 {
		return ""
	}
	rest := line[k+len(needle):]
	escaped := false
	end := -1
	for i := 0; i < len(rest); i++ {
		if rest[i] == '\\' {
			escaped = true
			i++ // skip the escaped byte, whatever it is
			continue
		}
		if rest[i] == '"' {
			end = i
			break
		}
	}
	if end < 0 {
		return ""
	}
	if !escaped {
		return string(rest[:end])
	}
	quoted := make([]byte, 0, end+2)
	quoted = append(quoted, '"')
	quoted = append(quoted, rest[:end]...)
	quoted = append(quoted, '"')
	var out string
	if err := json.Unmarshal(quoted, &out); err != nil {
		return "" // unparseable key: skip it rather than index a wrong one
	}
	return out
}

// scanOffsets walks an ndjson file, calling fn with each line and its byte
// offset. One pass, no per-record allocation beyond the extracted keys.
func scanOffsets(path string, fn func(line []byte, off int64)) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var off int64
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		var line []byte
		if i < 0 {
			line, data = data, nil
		} else {
			line, data = data[:i], data[i+1:]
		}
		if len(bytes.TrimSpace(line)) > 0 {
			fn(line, off)
		}
		off += int64(len(line) + 1)
	}
	return nil
}

func indexHeader(hash string) string { return fmt.Sprintf("#ctxidx v%d sha256=%s", indexVersion, hash) }

// buildStamp is the fail-safe header written at BUILD time: the content hash of
// the graph file. It may compute the hash (a graph written by an older binary
// has no sidecar yet) — acceptable here, because the build is already reading
// the whole file.
//
// It is keyed on CONTENT and not on size+mtime because the store rewrites the
// graph on every gather whether or not anything moved: mtime answered
// "rewritten", the rebuild guard answered "changed", and the two can never
// agree. See contenthash.go for the measurement that forced this.
func buildStamp(path string) (string, error) {
	c, err := ensureContentStamp(path)
	if err != nil {
		return "", err
	}
	return indexHeader(c.Hash), nil
}

// readStamp is the same header at READ time, and it NEVER hashes — it trusts
// the sidecar only as far as a stat can prove it, and refuses otherwise, so the
// caller falls back to the full scan.
func readStamp(path string) (string, bool) {
	c, ok := readContentStamp(path)
	if !ok {
		return "", false
	}
	return indexHeader(c.Hash), true
}

// writeIndex serializes key -> offsets as sorted plain text, atomically.
func writeIndex(path, header string, m map[string][]int64) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	f, err := createTemp(path) // unique per writer — see store.go createTemp
	if err != nil {
		return err
	}
	tmp := f.Name()
	w := bufio.NewWriterSize(f, 4<<20)
	fmt.Fprintln(w, header)
	for _, k := range keys {
		// A key containing a tab or newline would break the line format. Such
		// keys exist (kernel config labels carry tabs), so they are stored
		// JSON-quoted and decoded on read — never silently dropped.
		if strings.ContainsAny(k, "\t\n") {
			b, _ := json.Marshal(k)
			w.Write(b)
		} else {
			w.WriteString(k)
		}
		offs := m[k]
		sort.Slice(offs, func(a, b int) bool { return offs[a] < offs[b] })
		for _, o := range offs {
			w.WriteByte('\t')
			w.WriteString(strconv.FormatInt(o, 10))
		}
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// BuildIndex rebuilds every index for this store from the graph on disk.
// Called after a gather writes the graph; safe to call any time.
//
// Measured: ~2.5s / 432MB on linux (2.85M nodes, 5.54M edges), 0.008s / 0.6MB
// on a 1,476-file repo — i.e. ~2% of a kernel gather, and noise on real repos.
func (s *Store) BuildIndex() error {
	if err := os.MkdirAll(s.indexDir(), 0o755); err != nil {
		return err
	}

	if _, err := os.Stat(s.nodesPath()); err == nil {
		hdr, err := buildStamp(s.nodesPath())
		if err != nil {
			return err
		}
		// Labels are keyed LOWERCASED because ResolveVia matches them with
		// strings.EqualFold. A case-sensitive index would silently fail to
		// resolve `alpha` -> `Alpha` — findable by the old path, unfindable by
		// the new one, which is a changed answer.
		labels := make(map[string][]int64, 1024)
		ids := make(map[string][]int64, 1024)
		if err := scanOffsets(s.nodesPath(), func(line []byte, off int64) {
			if l := jsonField(line, "label"); l != "" {
				labels[strings.ToLower(l)] = append(labels[strings.ToLower(l)], off)
			}
			// IDs are case-SENSITIVE: ResolveVia's first tier is `ID == name`.
			if id := jsonField(line, "id"); id != "" {
				ids[id] = append(ids[id], off)
			}
		}); err != nil {
			return err
		}
		if err := writeIndex(filepath.Join(s.indexDir(), labelsIndex), hdr, labels); err != nil {
			return err
		}
		if err := writeIndex(filepath.Join(s.indexDir(), idsIndex), hdr, ids); err != nil {
			return err
		}
	}

	if _, err := os.Stat(s.edgesPath()); err == nil {
		hdr, err := buildStamp(s.edgesPath())
		if err != nil {
			return err
		}
		bySrc := make(map[string][]int64, 1024)
		byTgt := make(map[string][]int64, 1024)
		if err := scanOffsets(s.edgesPath(), func(line []byte, off int64) {
			if v := jsonField(line, "source"); v != "" {
				bySrc[v] = append(bySrc[v], off)
			}
			if v := jsonField(line, "target"); v != "" {
				byTgt[v] = append(byTgt[v], off)
			}
		}); err != nil {
			return err
		}
		if err := writeIndex(filepath.Join(s.indexDir(), edgesBySrc), hdr, bySrc); err != nil {
			return err
		}
		if err := writeIndex(filepath.Join(s.indexDir(), edgesByTgt), hdr, byTgt); err != nil {
			return err
		}
	}
	return nil
}

// openIndex opens an index IF it is current for its source file, WITHOUT
// reading it. A stale, missing, truncated or version-mismatched index returns
// ok=false and the caller scans instead — never trusted, always verified.
//
// Reading the file here was the first implementation and it was wrong: it
// pulled the whole 103MB label index (230MB for edges) into memory on EVERY
// lookup, which the scalability test caught at 6.98ms per lookup against the
// spike's 0.0001s. A lookup must touch kilobytes, not the whole index — so the
// file stays open and binary search reads small windows with ReadAt.
func openIndex(idxPath, srcPath string) (f *os.File, body, size int64, ok bool) {
	want, ok := readStamp(srcPath)
	if !ok {
		return nil, 0, 0, false
	}
	fh, err := os.Open(idxPath)
	if err != nil {
		return nil, 0, 0, false
	}
	st, err := fh.Stat()
	if err != nil {
		fh.Close()
		return nil, 0, 0, false
	}
	// The header is one short line; read a bounded window, never the file.
	hdr := make([]byte, min64(512, st.Size()))
	n, _ := fh.ReadAt(hdr, 0)
	hdr = hdr[:n]
	nl := bytes.IndexByte(hdr, '\n')
	if nl < 0 || string(hdr[:nl]) != want {
		fh.Close()
		return nil, 0, 0, false
	}
	return fh, int64(nl + 1), st.Size(), true
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// readLineAround returns the full line containing pos, plus its start offset.
// It grows a window until both boundaries are found, so a long line (a label
// with thousands of offsets) is still read whole.
func readLineAround(f *os.File, pos, body, size int64) (line []byte, start int64, err error) {
	const win = 8 << 10
	lo := pos - win
	if lo < body {
		lo = body
	}
	hi := pos + win
	if hi > size {
		hi = size
	}
	for {
		buf := make([]byte, hi-lo)
		n, rerr := f.ReadAt(buf, lo)
		if n == 0 && rerr != nil {
			return nil, 0, rerr
		}
		buf = buf[:n]
		rel := pos - lo
		if rel > int64(len(buf)) {
			rel = int64(len(buf))
		}
		s0 := bytes.LastIndexByte(buf[:rel], '\n')
		e0 := bytes.IndexByte(buf[rel:], '\n')
		haveStart := s0 >= 0 || lo == body
		haveEnd := e0 >= 0 || hi == size
		if haveStart && haveEnd {
			st := int64(0)
			if s0 >= 0 {
				st = int64(s0) + 1
			}
			en := int64(len(buf))
			if e0 >= 0 {
				en = rel + int64(e0)
			}
			return buf[st:en], lo + st, nil
		}
		// Widen and retry — bounded by the file, so this terminates.
		if !haveStart {
			lo -= win
			if lo < body {
				lo = body
			}
		}
		if !haveEnd {
			hi += win
			if hi > size {
				hi = size
			}
		}
		if lo == body && hi == size {
			// Whole body already in the window; take what we have.
			buf2 := make([]byte, size-body)
			n2, _ := f.ReadAt(buf2, body)
			return buf2[:n2], body, nil
		}
	}
}

// decodeKey reverses writeIndex's quoting for keys holding a tab or newline.
func decodeKey(b []byte) string {
	if len(b) > 0 && b[0] == '"' {
		var out string
		if json.Unmarshal(b, &out) == nil {
			return out
		}
	}
	return string(b)
}

// lookupOffsets binary-searches the sorted index for an exact key and returns
// every offset recorded for it. Touches O(log n) small windows, never the whole
// file — that is the difference between 7ms and microseconds.
func lookupOffsets(f *os.File, body, size int64, key string) ([]int64, bool) {
	lo, hi := body, size
	for lo < hi {
		mid := (lo + hi) / 2
		line, start, err := readLineAround(f, mid, body, size)
		if err != nil {
			return nil, false
		}
		tab := bytes.IndexByte(line, '\t')
		var k string
		if tab < 0 {
			k = decodeKey(line)
		} else {
			k = decodeKey(line[:tab])
		}
		switch {
		case k == key:
			if tab < 0 {
				return nil, true
			}
			var offs []int64
			for _, fld := range bytes.Split(line[tab+1:], []byte{'\t'}) {
				if n, err := strconv.ParseInt(string(bytes.TrimSpace(fld)), 10, 64); err == nil {
					offs = append(offs, n)
				}
			}
			return offs, true
		case k < key:
			next := start + int64(len(line)) + 1
			if next <= lo {
				return nil, false
			}
			lo = next
		default:
			if start <= body {
				return nil, false
			}
			hi = start - 1
		}
	}
	return nil, false
}

// readAt parses the records at the given byte offsets, and nothing else. This
// is the whole point: one line instead of 2.85M.
func readAt[T any](path string, offs []int64) ([]T, error) {
	if len(offs) == 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make([]T, 0, len(offs))
	r := bufio.NewReaderSize(f, 64*1024)
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

// ---- public lookup API (fail-safe: index if current, full scan otherwise) ----

// NodesByLabel returns EVERY node whose label matches, CASE-INSENSITIVELY —
// the same comparison analyze.ResolveVia uses (strings.EqualFold), so a
// resolver built on this cannot disagree with the scan it replaces. Uses the
// index when current, otherwise a full parse; identical answers, different cost.
//
// Returns all matches, never the first: 20.3% of linux records share a label
// with at least one other node, and returning one of 14,989 "description" nodes
// as if it were the answer is the under-report this project exists to avoid.
func (s *Store) NodesByLabel(label string) ([]schema.Node, error) {
	if f, body, size, ok := openIndex(filepath.Join(s.indexDir(), labelsIndex), s.nodesPath()); ok {
		offs, found := lookupOffsets(f, body, size, strings.ToLower(label))
		f.Close()
		if !found {
			return nil, nil // index is current and the label is genuinely absent
		}
		return readAt[schema.Node](s.nodesPath(), offs)
	}
	nodes, err := s.Nodes()
	if err != nil {
		return nil, err
	}
	var out []schema.Node
	for _, n := range nodes {
		if strings.EqualFold(n.Label, label) {
			out = append(out, n)
		}
	}
	return out, nil
}

// EdgesFrom returns every edge whose source is exactly id.
func (s *Store) EdgesFrom(id string) ([]schema.Edge, error) {
	return s.edgesByEndpoint(id, edgesBySrc, func(e schema.Edge) string { return e.Source })
}

// EdgesTo returns every edge whose target is exactly id — the reverse lane that
// `affected` and card's caller list walk.
func (s *Store) EdgesTo(id string) ([]schema.Edge, error) {
	return s.edgesByEndpoint(id, edgesByTgt, func(e schema.Edge) string { return e.Target })
}

func (s *Store) edgesByEndpoint(id, idxName string, pick func(schema.Edge) string) ([]schema.Edge, error) {
	if f, body, size, ok := openIndex(filepath.Join(s.indexDir(), idxName), s.edgesPath()); ok {
		offs, found := lookupOffsets(f, body, size, id)
		f.Close()
		if !found {
			return nil, nil
		}
		return readAt[schema.Edge](s.edgesPath(), offs)
	}
	edges, err := s.Edges()
	if err != nil {
		return nil, err
	}
	var out []schema.Edge
	for _, e := range edges {
		if pick(e) == id {
			out = append(out, e)
		}
	}
	return out, nil
}

// IndexCurrent reports whether every index is present and matches the graph on
// disk. Used by status/tests; callers of the lookup API never need to ask.
func (s *Store) IndexCurrent() bool {
	// EVERY node index must be present, not just the labels one. A store built
	// by an older binary has labels.idx but no ids.idx; reporting "current"
	// there sent NodeByID down its full-scan fallback on every lookup, so the
	// "fast" path quietly loaded 2.85M nodes and took ~1s instead of ~2ms.
	// Partial is not current.
	if _, err := os.Stat(s.nodesPath()); err == nil {
		for _, n := range []string{labelsIndex, idsIndex} {
			f, _, _, ok := openIndex(filepath.Join(s.indexDir(), n), s.nodesPath())
			if !ok {
				return false
			}
			f.Close()
		}
	}
	if _, err := os.Stat(s.edgesPath()); err == nil {
		for _, n := range []string{edgesBySrc, edgesByTgt} {
			f, _, _, ok := openIndex(filepath.Join(s.indexDir(), n), s.edgesPath())
			if !ok {
				return false
			}
			f.Close()
		}
	}
	return true
}

// IndexState names the lookup index in one word, for `status` to print (ADR 18
// D3): "current" (lookups take the fast path), "stale" (index files exist but
// do not match the graph, so every lookup falls back to a full scan), "absent"
// (never built).
//
// The fallback is silent by design — it costs speed, never correctness — which
// is exactly how a 270x regression shipped unnoticed. Being able to SEE it is
// the whole point of this function.
func (s *Store) IndexState() string {
	if s.IndexCurrent() {
		return "current"
	}
	for _, n := range []string{labelsIndex, idsIndex, edgesBySrc, edgesByTgt} {
		if _, err := os.Stat(filepath.Join(s.indexDir(), n)); err == nil {
			return "stale"
		}
	}
	return "absent"
}

// NodeByID returns the node with exactly this id (case-sensitive), if any.
func (s *Store) NodeByID(id string) (*schema.Node, error) {
	if f, body, size, ok := openIndex(filepath.Join(s.indexDir(), idsIndex), s.nodesPath()); ok {
		offs, found := lookupOffsets(f, body, size, id)
		f.Close()
		if !found {
			return nil, nil
		}
		got, err := readAt[schema.Node](s.nodesPath(), offs)
		if err != nil || len(got) == 0 {
			return nil, err
		}
		return &got[0], nil
	}
	nodes, err := s.Nodes()
	if err != nil {
		return nil, err
	}
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i], nil
		}
	}
	return nil, nil
}

// ResolveExact reproduces analyze.ResolveVia's first two tiers — "exact-id"
// then "exact-label" — using the index, and REFUSES anything it cannot decide
// there (ok=false), leaving last-segment and fuzzy to the full-scan path.
//
// It is deliberately a partial resolver. The later tiers rank against every
// node in the store (token overlap, tie detection, did-you-mean), which no
// point lookup can reproduce; pretending otherwise would change answers for the
// hard cases. Refusing costs a full load on those and keeps them exact.
//
// The tie-break rules below are copied from ResolveVia and MUST stay identical:
// an import stub never beats a real declaration, and among equals the smallest
// id wins. Divergence here is a silently different answer, not a crash.
func (s *Store) ResolveExact(name string) (n *schema.Node, via string, ok bool) {
	if hit, err := s.NodeByID(name); err == nil && hit != nil {
		return hit, "exact-id", true
	}
	cands, err := s.NodesByLabel(name)
	if err != nil || len(cands) == 0 {
		return nil, "", false
	}
	best := &cands[0]
	for i := 1; i < len(cands); i++ {
		c := &cands[i]
		switch {
		case labelRankNode(c) < labelRankNode(best):
			best = c
		case labelRankNode(c) == labelRankNode(best) && c.ID < best.ID:
			best = c
		}
	}
	return best, "exact-label", true
}

// isImportStubID mirrors analyze.isImportStub. Duplicated rather than imported
// because store must not depend on analyze; the index_equivalence test pins
// them together so a change to one that skips the other fails the build.
func isImportStubID(id string) bool { return strings.HasPrefix(id, "module://") }

// labelRankNode mirrors analyze.labelRank — a DEFINITION beats a MENTION when
// several nodes share a label. Lower wins. Same duplication rule as
// isImportStubID: store must not depend on analyze, so the equivalence test is
// what keeps the two honest.
//
// This function is why the fast path stays a SPEED optimisation and not a
// behaviour change. `card Flask` resolves by exact label, so it never reaches
// analyze at all — fixing the tiebreak only there left the index answering
// `README.md::flask` while the slow path answered `src/flask/app.py::Flask`.
// The equivalence test did not catch it, because no fixture node paired prose
// with a declaration; one now does.
var declKindsIdx = map[string]bool{
	"function": true, "method": true, "class": true, "type": true,
	"interface": true, "struct": true, "enum": true, "trait": true,
	"file": true,
}

func labelRankNode(n *schema.Node) int {
	switch {
	case isImportStubID(n.ID), n.Kind == "dependency", strings.HasPrefix(n.ID, "dep://"):
		return 3
	case n.Kind == "section" || n.Kind == "document":
		return 2
	case declKindsIdx[n.Kind]:
		return 0
	default:
		return 1
	}
}

// EdgesTouching returns every edge with id as source or target, deduplicated —
// exactly the edges analyze.CardFor inspects, and nothing else. A self-edge
// appears in both directions and must be returned ONCE, or the card would count
// it twice.
func (s *Store) EdgesTouching(id string) ([]schema.Edge, error) {
	from, err := s.EdgesFrom(id)
	if err != nil {
		return nil, err
	}
	to, err := s.EdgesTo(id)
	if err != nil {
		return nil, err
	}
	out := make([]schema.Edge, 0, len(from)+len(to))
	seen := make(map[string]bool, len(from)+len(to))
	for _, e := range append(from, to...) {
		k := e.Source + "\x00" + e.Target + "\x00" + e.Relation + "\x00" + e.Confidence
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out, nil
}

// EdgesTouchingOrdered returns every edge with id as source or target, in the
// order they appear in edges.ndjson — the order a full scan would produce.
//
// EdgesTouching cannot serve this: it concatenates from-edges then to-edges and
// deduplicates by CONTENT, which reorders a node's edges and collapses a
// self-edge. `query` caps a node's neighbours at 12, so order decides WHICH
// twelve survive; an indexed lane that returns a different order returns a
// different answer. This one merges the two offset lists and sorts by offset,
// so the indexed and scanning lanes agree byte for byte (ADR 25 slice 0, I1).
//
// A self-edge appears ONCE here — it is one line — and callers that treat
// source and target separately expand it, exactly as a scan over the file does.
//
// ok=false means no index, or an index stale against the graph. The caller
// falls back to the scan and must SAY so (I3): a silent fallback is a 56x
// regression that looks like ordinary slowness.
func (s *Store) EdgesTouchingOrdered(id string) ([]schema.Edge, bool, error) {
	fs, bs, zs, oks := openIndex(filepath.Join(s.indexDir(), edgesBySrc), s.edgesPath())
	ft, bt, zt, okt := openIndex(filepath.Join(s.indexDir(), edgesByTgt), s.edgesPath())
	if !oks || !okt {
		if oks {
			fs.Close()
		}
		if okt {
			ft.Close()
		}
		return nil, false, nil
	}
	from, _ := lookupOffsets(fs, bs, zs, id)
	to, _ := lookupOffsets(ft, bt, zt, id)
	fs.Close()
	ft.Close()

	seen := make(map[int64]bool, len(from)+len(to))
	offs := make([]int64, 0, len(from)+len(to))
	for _, o := range append(from, to...) {
		if seen[o] {
			continue // a self-edge is one line, listed in both indexes
		}
		seen[o] = true
		offs = append(offs, o)
	}
	sort.Slice(offs, func(i, j int) bool { return offs[i] < offs[j] }) // file order
	edges, err := readAt[schema.Edge](s.edgesPath(), offs)
	if err != nil {
		return nil, false, err
	}
	return edges, true, nil
}
