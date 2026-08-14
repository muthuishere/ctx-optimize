package boundaries

import "strings"

// Normalize is D6's per-transport identifier fold, applied ONCE at emit and
// reused verbatim by `drift` for comparison — one implementation, so the two
// can never disagree about what joins. Without it provides/consumes never
// join and scope/drift are noise.
//
// network.* transports fold the spellings frameworks disagree on: case,
// the trailing slash, and `{id}` ≡ `:id` ≡ `<id>` path parameters (all
// become `*` — param NAMES differ across provider and consumer, so keeping
// them would still never join). Every other transport is left verbatim after
// a trim: config.env and process.exec identifiers are case-sensitive facts.
// The pre-fold spelling stays recoverable in port metadata `raw`.
func Normalize(transport, raw string) string {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(transport, "network.") {
		return s
	}
	s = strings.ToLower(s)
	if len(s) > 1 {
		s = strings.TrimRight(s, "/")
	}
	segs := strings.Split(s, "/")
	for i, seg := range segs {
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, ":") ||
			(strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")) ||
			(strings.HasPrefix(seg, "<") && strings.HasSuffix(seg, ">")) {
			segs[i] = "*"
		}
	}
	return strings.Join(segs, "/")
}
