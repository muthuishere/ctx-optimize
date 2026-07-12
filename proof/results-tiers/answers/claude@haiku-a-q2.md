**Refuses to split (returns `ERR_PTR(-EINVAL)`):**

1. **Zero or negative sectors** (line 1828)
2. **Sectors >= bio_sectors(bio)** — attempting to split at or beyond the bio's total size (line 1830)
3. **Zone append commands** — `REQ_OP_ZONE_APPEND` ops (line 1834)
4. **Atomic writes** — bios with `REQ_ATOMIC` flag (line 1838)

**What it does to the original bio on success:**

Calls `bio_advance(bio, split->bi_iter.bi_size)` at **block/bio.c:1850** — this advances the original bio's iterator to skip past the sectors that were split off to the new bio. The original bio is modified in-place to point to the remaining data after the split point.