## Answer

`bio_split()` at block/bio.c:1823 refuses (returns `ERR_PTR(-EINVAL)`, via `WARN_ON_ONCE`) to split:

1. **`sectors <= 0`** — split length must be positive (block/bio.c:1828)
2. **`sectors >= bio_sectors(bio)`** — split length must be strictly smaller than the whole bio (block/bio.c:1830)
3. **Zone-append bios** — `bio_op(bio) == REQ_OP_ZONE_APPEND` (block/bio.c:1833-1835)
4. **Atomic writes** — `bio->bi_opf & REQ_ATOMIC` (block/bio.c:1837-1839)

On success, it allocates a clone `split` (via `bio_alloc_clone`) representing the first `sectors` sectors, trims its integrity payload if present, then calls `bio_advance(bio, split->bi_iter.bi_size)` on the **original** bio — advancing its `bi_iter` forward by that many bytes, so `bio` now represents only the *remaining* sectors (block/bio.c:1841-1855). It also propagates the `BIO_TRACE_COMPLETION` flag to the new split bio if set on the original.