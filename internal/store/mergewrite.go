package store

import (
	"sort"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// Rewriting the graph used to re-SORT it, and that was the whole cost.
//
// Phase split of ReplaceAll on go-kubernetes (334,002 nodes / 646,312 edges,
// 280MB of ndjson):
//
//	read+parse nodes  95ms
//	read+parse edges 116ms
//	build maps       124ms
//	sort           1,375ms   <- 59%
//	marshal+write    593ms
//
// The sort is pure waste: every write orders the file, so what we read back is
// ALREADY sorted. Re-sorting it is O(n log n) work to rediscover an invariant
// we ourselves maintain. The survivors of a replace stream out in their
// existing order; only genuinely NEW ids need ordering, and in the case that
// matters — a re-gather after one file changed — that set is nearly empty.
//
// So: walk the old sorted slice, keep whoever survived, sort only the
// additions, and 2-way merge. Output is identical by construction — same
// elements, same comparator, just not rediscovered from scratch.
//
// Safety valve: if the input is NOT sorted (a store written by an older
// version, or hand-edited), mergeOrdered falls back to sorting everything. The
// check is one linear pass and it costs less than being wrong.

// sortedByFunc reports whether xs is already non-decreasing under key.
func sortedByFunc[T any](xs []T, key func(*T) string) bool {
	for i := 1; i < len(xs); i++ {
		if key(&xs[i-1]) > key(&xs[i]) {
			return false
		}
	}
	return true
}

// mergeOrdered produces the final, sorted set from:
//   - old:   what was on disk, expected sorted by key
//   - final: id/key -> value for everything that survives (may hold entries
//     not present in old, and updated values for entries that are)
//
// It returns them in key order without sorting the whole set.
func mergeOrdered[T any](old []T, final map[string]T, key func(*T) string) []T {
	if !sortedByFunc(old, key) {
		// Foreign or legacy layout — do it the slow, safe way.
		out := make([]T, 0, len(final))
		for _, v := range final {
			out = append(out, v)
		}
		sort.Slice(out, func(i, j int) bool { return key(&out[i]) < key(&out[j]) })
		return out
	}

	out := make([]T, 0, len(final))
	seen := make(map[string]struct{}, len(old))
	for i := range old {
		k := key(&old[i])
		if _, dup := seen[k]; dup {
			continue // defensive: a legacy file could repeat a key
		}
		v, ok := final[k]
		if !ok {
			continue // pruned
		}
		seen[k] = struct{}{}
		out = append(out, v)
	}

	// Whatever the batches introduced that was not already on disk.
	var added []T
	for k, v := range final {
		if _, ok := seen[k]; !ok {
			added = append(added, v)
		}
	}
	if len(added) == 0 {
		return out
	}
	sort.Slice(added, func(i, j int) bool { return key(&added[i]) < key(&added[j]) })

	// 2-way merge of two sorted runs.
	merged := make([]T, 0, len(out)+len(added))
	i, j := 0, 0
	for i < len(out) && j < len(added) {
		if key(&out[i]) <= key(&added[j]) {
			merged = append(merged, out[i])
			i++
		} else {
			merged = append(merged, added[j])
			j++
		}
	}
	merged = append(merged, out[i:]...)
	merged = append(merged, added[j:]...)
	return merged
}

// nodeKey / edgeKeyOf are the comparators the graph files are ordered by. They
// exist as named functions so the merge and any fallback sort can never drift
// apart — the bug that produces is a file that looks sorted and is not.
func nodeKey(n *schema.Node) string { return n.ID }
func edgeKeyOf(e *schema.Edge) string {
	return e.Source + "\x00" + e.Target + "\x00" + e.Relation
}

// Raw-line passthrough was tried here and REJECTED on measurement, recorded so
// it is not re-attempted: pairing every stored record with the bytes it was
// parsed from, and writing those back verbatim for records no batch touched,
// made ReplaceAll SLOWER — 1.801s against 1.741s on go-kubernetes. Building the
// 980k-entry index costs more map work than the json.Encoder work it avoids,
// and the encoder is already fast. The write is not where the remaining time is.
