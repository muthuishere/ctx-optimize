## Answer

For read/write bios, the decision and split happen in two steps in `__bio_split_to_limits()` (`block/blk.h:429-451`):

1. **Deciding check**: `bio_may_need_split(bio, lim)` — `block/blk.h:397-414`. It's a cheap fast-path check: returns `true` (needs splitting) if `lim->chunk_sectors` is set, the bio has no `bi_io_vec`, the bio's size exceeds what's left in the first bvec, the first bvec's offset/len violate `dma_alignment`, or the bvec exceeds `lim->max_fast_segment_size`. If none of these trip, the single-segment bio is accepted as-is (no split needed) — this is the "quick and dirty" optimization noted in the comment.

2. **Splitting function**: if the check trips, `bio_split_rw(bio, lim, nr_segs)` — `block/blk-merge.c:421-427` — performs the actual split. It computes the max allowed size via `get_max_io_size()` (`block/blk-merge.c:229-260`, which factors in `max_sectors`/`atomic_write_max_sectors`/`max_write_zeroes_sectors` and boundary/alignment constraints), determines the split point with `bio_split_rw_at()`, and hands off to `bio_submit_split()` (`block/blk-merge.c:142-157`), which actually clones/chains the bio via `bio_submit_split_bioset()` → `bio_split()` (`block/bio.c:1823-1856`) and resubmits the remainder.

Note: other bio ops go through the same `__bio_split_to_limits` dispatcher but different splitters — `REQ_OP_ZONE_APPEND` → `bio_split_zone_append`, `REQ_OP_DISCARD`/`REQ_OP_SECURE_ERASE` → `bio_split_discard`, `REQ_OP_WRITE_ZEROES` → `bio_split_write_zeroes` — all in `block/blk-merge.c`, all ultimately funneling through `bio_submit_split()`.

`__bio_split_to_limits()` is called from `bio_split_to_limits()` (`block/blk-merge.c`) and `blk_mq_submit_bio()` (`block/blk-mq.c`) — the actual call sites in the submission path.