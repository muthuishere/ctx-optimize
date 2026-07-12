Confirmed by grep — only one call site inside `block/`.

**Caller to update:**
- `bio_submit_split_bioset()` — `block/blk-merge.c:122`
  `struct bio *split = bio_split(bio, split_sectors, GFP_NOIO, bs);`

That's the only direct invocation of `bio_split()` within `block/`. Its own caller, `bio_submit_split()` (also in `block/blk-merge.c`), doesn't call `bio_split` directly and wouldn't need changes unless the signature change propagates through `bio_submit_split_bioset`'s own signature.