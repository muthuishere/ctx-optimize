I have the full picture now.

## Deciding check
For read/write bios, the decision of whether a split is needed is `bio_may_need_split()` in `block/blk.h:397-414`, called from `__bio_split_to_limits()` (`block/blk.h:429-450`, switch case `REQ_OP_READ`/`REQ_OP_WRITE` at `block/blk.h:432-438`).

`bio_may_need_split()` returns `true` (needs splitting) if any of:
- `lim->chunk_sectors` is set (chunked/zoned-style limit) — `blk.h:402-403`
- the bio has no `bi_io_vec` — `blk.h:405-406`
- the bio's data size exceeds what's left in the first bvec — `blk.h:408-409`
- the first bvec's offset/len violate `lim->dma_alignment` — `blk.h:411-412`
- the first bvec's offset+len exceeds `lim->max_fast_segment_size` — `blk.h:413`

This is a cheap, single-segment fast-path check — if it's false, the bio is used as-is (`*nr_segs = 1`) with no splitting.

## Split function
If splitting is needed, `bio_split_rw()` (`block/blk-merge.c:421-427`) does the actual work:
- It computes the max allowed size via `get_max_io_size()` (`block/blk-merge.c:229`)
- Calls `bio_split_rw_at()` → `bio_split_io_at()` (`block/blk-merge.c:340-419`, walks all bvecs to determine where a valid split point falls, honoring `max_segments`, `max_fast_segment_size`, SG-gap/virtual-boundary rules, and alignment) to compute the split point in sectors
- Passes that to `bio_submit_split()` (`block/blk-merge.c:142`, which calls `bio_submit_split_bioset()` at `block/blk-merge.c:119` to actually clone/split the bio via `bio_split()` and resubmit the remainder)

## Entry point / callers
`__bio_split_to_limits()` (`block/blk.h:429`) is the dispatcher by `bio_op()`: `REQ_OP_READ`/`REQ_OP_WRITE` → `bio_may_need_split()` check then `bio_split_rw()`; `REQ_OP_ZONE_APPEND` → always `bio_split_zone_append()`; `REQ_OP_DISCARD`/`REQ_OP_SECURE_ERASE` → `bio_split_discard()`; `REQ_OP_WRITE_ZEROES` → `bio_split_write_zeroes()`. It's exposed as `bio_split_to_limits()` (`block/blk-merge.c:479-485`), called from the `submit_bio` path (e.g. `block/blk-core.c`) before a bio is handed to the request queue/mq layer.