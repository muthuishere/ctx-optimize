The block layer decides whether to split a bio using the **`bio_may_need_split`** check in `block/blk.h:397-414`. For read/write operations, this check is invoked from **`__bio_split_to_limits`** in `block/blk.h:429-451`.

**Deciding check**: `bio_may_need_split` (block/blk.h:397-414) evaluates:
- `lim->chunk_sectors` being set
- Multi-segment bios (size > single vector length)
- DMA alignment constraints  
- Segment size exceeding `max_fast_segment_size`

**Splitting function**: The actual split is performed by **`bio_split`** in `block/bio.c:1823-1856`, called through the chain:
- `__bio_split_to_limits` (blk.h:429) → `bio_split_rw` (blk-merge.c:421) → `bio_submit_split` (blk-merge.c:142) → `bio_submit_split_bioset` (blk-merge.c:119) → `bio_split` (bio.c:1823)