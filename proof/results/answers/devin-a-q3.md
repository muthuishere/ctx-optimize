The block layer decides whether to split a bio through the following flow:

**Deciding check:** `bio_may_need_split()` in <ref_file file="/private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-nexus-workspace-brain/dc4417dd-0040-4de7-82a4-10f10843dc78/scratchpad/proof/linux/block/blk.h" lines="397-414" />

This function checks:
- If `lim->chunk_sectors` is set (line 402)
- If the bio has multiple segments that exceed the first bvec's length (line 409)
- If DMA alignment requirements are violated (line 411)
- If the segment exceeds `max_fast_segment_size` (line 413)

**Splitting function:** `bio_split()` in <ref_file file="/private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-nexus-workspace-brain/dc4417dd-0040-4de7-82a4-10f10843dc78/scratchpad/proof/linux/block/bio.c" lines="1823-1857" />

This function:
- Clones the bio using `bio_alloc_clone()` (line 1841)
- Sets the split bio's size to the requested sectors (line 1845)
- Advances the original bio past the split point (line 1850)

**Call path:** `blk_mq_submit_bio()` → `__bio_split_to_limits()` → `bio_may_need_split()` check → `bio_split_rw()` → `bio_submit_split_bioset()` → `bio_split()`