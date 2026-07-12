## `bio_split()` — block/bio.c:1823-1856

**Refuses to split (returns `ERR_PTR(-EINVAL)` via `WARN_ON_ONCE`):**
- `sectors <= 0` (line 1828)
- `sectors >= bio_sectors(bio)` — i.e. the split point isn't strictly inside the bio (line 1830)
- `REQ_OP_ZONE_APPEND` bios — zone-append commands cannot be split (line 1834)
- Bios with `REQ_ATOMIC` set — atomic writes cannot be split (line 1838)

**On success, effect on the original `bio`:**
- Allocates `split` as a clone of `bio` via `bio_alloc_clone()` (line 1841), sized to `sectors << 9` bytes (line 1845), with integrity data trimmed if present (line 1847-1848).
- Calls `bio_advance(bio, split->bi_iter.bi_size)` (line 1850) — this **advances the original `bio` forward** by the split-off size, consuming those sectors from its front so `bio` now represents only the remaining tail.
- Propagates `BIO_TRACE_COMPLETION` flag to `split` if set on `bio` (line 1852-1853).

So `bio` is mutated in place to become "the rest after the front `sectors` were split off," while `split` is a new bio representing that front portion (sharing `bio`'s `bi_io_vec` unless it's a discard, per the doc comment).