`bio_split()` is defined in block/bio.c:1823. It has exactly one direct caller inside `block/`:

- **`bio_submit_split_bioset()`** — `block/blk-merge.c:122`

All other `bio_split*` hits in `block/` (e.g. `bio_split_discard`, `bio_split_rw`, `bio_split_io_at`, `bio_split_to_limits`, `bio_split_zone_append`, `bio_split_write_zeroes`, and `disk->bio_split` the bio_set field) are distinct symbols, not calls to `bio_split()` itself.