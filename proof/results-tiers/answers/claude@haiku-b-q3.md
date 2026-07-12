**Deciding check:** `bio_may_need_split()` in **block/blk.h:397–414**

This inline function determines if a bio needs splitting by checking queue limits:
- DMA alignment (`dma_alignment`)
- Chunk sector boundaries (`chunk_sectors`)
- Maximum segment size (`max_fast_segment_size`)
- Bio size vs. io_vec length

**Splitting dispatch & execution for read/write bios:** `__bio_split_to_limits()` in **block/blk.h:429–451**

This calls `bio_may_need_split()` at line 435, then dispatches to operation-specific split functions. For reads/writes it calls:

**`bio_split_rw()`** in **block/blk-merge.c:421–427**

This performs the actual split by calling `bio_submit_split()` (block/blk-merge.c:142), which allocates a split bio and submits the front portion while returning the remainder for re-submission.

**Summary:**
- **Check**: `bio_may_need_split(bio, lim)` — block/blk.h:397
- **Splitter**: `bio_split_rw(bio, lim, nr_segs)` — block/blk-merge.c:421 (called from `__bio_split_to_limits()`)