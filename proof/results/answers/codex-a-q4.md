Inside `block/`, there is one `bio_split()` caller to update:

- `bio_submit_split_bioset()` in [block/blk-merge.c](/private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-nexus-workspace-brain/dc4417dd-0040-4de7-82a4-10f10843dc78/scratchpad/proof/linux/block/blk-merge.c:117): calls `bio_split(bio, split_sectors, GFP_NOIO, bs)` at line 122.

I found no other call sites under `block/`; [block/bio.c](/private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-nexus-workspace-brain/dc4417dd-0040-4de7-82a4-10f10843dc78/scratchpad/proof/linux/block/bio.c:1823) is the function definition, not a caller.