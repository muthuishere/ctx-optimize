This gives the full picture. Here's how the block layer decides on split for read/write bios:

**Deciding check:** `bio_may_need_split()` — `block/blk.h:397-414`. For `REQ_OP_READ`/`REQ_OP_WRITE`, `__bio_split_to_limits()` (`block/blk.h:429-451`, specifically the switch case at L432-438) calls this fast-path check first. It returns `true` (needs a closer look) if:
- `lim->chunk_sectors` is set (L402-403), or
- the bio has no `bi_io_vec` (L405-406), or
- the current bvec is shorter than the bio's remaining size (L409-410), or
- the bvec's offset/length violate `lim->dma_alignment` (L411-412), or
- the bvec exceeds `lim->max_fast_segment_size` (L413).

Only if this fast check trips does the full splitter run; otherwise the bio is used as-is (`*nr_segs = 1`).

**Splitting function:** `bio_split_rw()` in `block/blk-merge.c:421-435` (calls into `bio_split_io_at()`, `block/blk-merge.c:335-418`, the real workhorse that walks segments and finds the split point against `max_sectors`/`max_segments`/gaps/etc., then `block/bio.c::bio_split()` at L1823-1856 does the actual bio clone+split).

**Entry point:** `__bio_split_to_limits()` (`block/blk.h:429`) is invoked from `bio_split_to_limits()` (`block/blk-merge.c:479-484`) and directly from `blk_mq_submit_bio()` (`block/blk-mq.c`) during bio submission — this is where the op-type switch (READ/WRITE → `bio_may_need_split` → `bio_split_rw`) happens.