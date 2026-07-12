**Deciding check:** `bio_may_need_split()` at `block/blk.h:397`

This function checks whether a bio must be split by evaluating:
- `lim->chunk_sectors` (line 402)
- bio_io_vec presence (line 405)
- bio size vs. buffer vector length (line 409)
- DMA alignment constraints (line 411)
- max_fast_segment_size violations (line 413)

**Splitting function:** `bio_split()` at `block/bio.c:1823`

This is the core function that actually performs the split. It's called via `bio_submit_split_bioset()` at `block/blk-merge.c:122`.

**Decision flow:** The orchestration happens in `__bio_split_to_limits()` at `block/blk.h:429-451`:
- Line 435: calls `bio_may_need_split(bio, lim)` to decide
- Line 436: if true, calls `bio_split_rw()` at `block/blk-merge.c:421` which chains through `bio_submit_split()` → `bio_submit_split_bioset()` → `bio_split()`