`bio_split()` in <ref_file file="/private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-nexus-workspace-brain/dc4417dd-0040-4de7-82a4-10f10843dc78/scratchpad/proof/linux/block/bio.c" lines="1823-1856" /> refuses to split:

1. **Bios with invalid sector counts** (lines 1828-1831):
   - `sectors <= 0`
   - `sectors >= bio_sectors(bio)` (splitting entire bio or more)

2. **Zone append commands** (lines 1833-1835):
   - `bio_op(bio) == REQ_OP_ZONE_APPEND`

3. **Atomic writes** (lines 1837-1839):
   - `bio->bi_opf & REQ_ATOMIC`

When it succeeds, the original bio is **advanced** by the split size via `bio_advance(bio, split->bi_iter.bi_size)` at line 1850, which moves its iterator forward past the split portion.