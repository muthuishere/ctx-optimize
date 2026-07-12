`bio_split()` at block/bio.c:1823-1856 refuses to split, returning `ERR_PTR(-EINVAL)` (with `WARN_ON_ONCE` for the first three) for:

- `sectors <= 0` (bio.c:1828)
- `sectors >= bio_sectors(bio)` — i.e. you can't split off the whole bio or more (bio.c:1830)
- `bio_op(bio) == REQ_OP_ZONE_APPEND` — zone-append commands can't be split (bio.c:1834)
- `bio->bi_opf & REQ_ATOMIC` — atomic writes can't be split (bio.c:1838, no `WARN_ON_ONCE` here)

On success, it allocates a clone (`bio_alloc_clone`, sharing `bio`'s `bi_io_vec`) sized to the requested `sectors` for the returned front piece, trims its integrity payload if present (`bio_integrity_trim`), then calls `bio_advance(bio, split->bi_iter.bi_size)` on the **original** bio — advancing its iterator forward by that many bytes so `bio` now represents only the remaining (back) portion. It also propagates the `BIO_TRACE_COMPLETION` flag to the new split bio if the original had it set.