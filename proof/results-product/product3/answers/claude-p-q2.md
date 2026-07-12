`bio_split()` at `block/bio.c:1823-1856`:

**Refuses to split (returns `ERR_PTR(-EINVAL)`):**
- `sectors <= 0` (block/bio.c:1829-1830)
- `sectors >= bio_sectors(bio)` — i.e. split point at/beyond the end (block/bio.c:1831-1832)
- Zone-append bios, `bio_op(bio) == REQ_OP_ZONE_APPEND` (block/bio.c:1834-1836) — zone-append can't be split since the device chooses the write offset
- Atomic writes, `bio->bi_opf & REQ_ATOMIC` (block/bio.c:1838-1839)

**On success:**
- Clones `bio` via `bio_alloc_clone()` into `split`, sized to `sectors << 9` bytes (block/bio.c:1841-1845)
- Trims integrity data on `split` if present (block/bio.c:1847-1848)
- Calls `bio_advance(bio, split->bi_iter.bi_size)` — advances the *original* `bio`'s iterator forward by the split-off size, so `bio` now represents only the remaining sectors (block/bio.c:1850)
- Propagates `BIO_TRACE_COMPLETION` flag to `split` if set on `bio` (remainder of function past line 1856)

So the original `bio` is not copied but mutated in place to become the "tail", while `split` (new bio, sharing `bio`'s `bi_io_vec`) becomes the "head" returned to the caller.