`bio_split()` in `block/bio.c:1823-1857` refuses to split:

- **`sectors <= 0`** — invalid split size (`block/bio.c:1828`)
- **`sectors >= bio_sectors(bio)`** — split size not strictly smaller than the whole bio (`block/bio.c:1830`)
- **`REQ_OP_ZONE_APPEND` bios** — zone-append commands cannot be split (`block/bio.c:1834`)
- **atomic writes** (`bio->bi_opf & REQ_ATOMIC`) — atomic writes cannot be split (`block/bio.c:1838`)

All these return `ERR_PTR(-EINVAL)` (via `WARN_ON_ONCE` for the first three).

On success, it clones `bio` into a new `split` bio holding the first `sectors` (via `bio_alloc_clone`, `bio_split():1841`), trims the split's integrity payload if present (`:1847-1848`), then calls `bio_advance(bio, split->bi_iter.bi_size)` (`:1850`) — this **mutates the original `bio` in place**, advancing its `bi_iter` (sector offset, size, iovec position) forward past the sectors that were split off, so `bio` now represents only the *remaining* tail. It also propagates the `BIO_TRACE_COMPLETION` flag to `split` if set on the original (`:1852-1853`).