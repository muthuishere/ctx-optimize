Only one call site inside `block/` directly invokes `bio_split()`:

- **`bio_submit_split_bioset()`** — `block/blk-merge.c:122`

Notes to disambiguate from similarly-named symbols also found in `block/`, none of which call `bio_split()` itself:
- `bio_split_discard()`, `bio_split_rw()`, `bio_split_zone_append()`, `bio_split_write_zeroes()`, `bio_split_io_at()`, `bio_split_to_limits()`, `bio_split_rw_at()` — separate functions declared/defined in `block/blk.h` and `block/blk-merge.c`; they don't call `bio_split()`.
- `disk->bio_split` in `block/genhd.c:1295,1451,1502` — a `struct bio_set` field, unrelated to the function.

So if you change `bio_split()`'s signature, the only in-`block/` code to fix is `bio_submit_split_bioset` at `block/blk-merge.c:122`. (Its own callers outside `block/`, e.g. in `drivers/md/`, would also need checking, but the question is scoped to `block/`.)