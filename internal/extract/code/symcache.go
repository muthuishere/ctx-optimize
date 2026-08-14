package code

import "sync"

// Symbol tables are a property of the COMPILED grammar, not of a guest
// instance: the same wasm module always yields the same symbol-id → name
// table. Reading one costs a dedicated instance (a fresh 64 MB linear memory)
// that is spun up and thrown away, and ExtractPaths runs ONCE PER MODULE — so
// a monorepo paid that price per module for a byte-identical answer. On the
// kubernetes corpus symbol loading was 478 MB of allocation; a 6-module repo
// paid it six times.
//
// Cached process-wide, keyed by engine key ("" = the embedded bundle, else the
// pack's wasm path — the same key the engine map uses, so a pack rebuilt at a
// new path is a new key and cannot serve a stale table).
//
// RWMutex rather than sync.Map: this is written a handful of times (once per
// grammar) and read once per module — the read-mostly case a plain RWMutex
// already handles, without sync.Map's interface boxing on every access.
var (
	symCacheMu sync.RWMutex
	symCache   = map[string]map[int][]string{}
)

// cachedSymbols returns a previously loaded table for key, if any. The returned
// map is shared and MUST be treated as read-only — every consumer only indexes
// it, which is what the "read-only after this" contract at the call site means.
func cachedSymbols(key string) (map[int][]string, bool) {
	symCacheMu.RLock()
	defer symCacheMu.RUnlock()
	m, ok := symCache[key]
	return m, ok
}

func storeSymbols(key string, m map[int][]string) {
	symCacheMu.Lock()
	defer symCacheMu.Unlock()
	symCache[key] = m
}
