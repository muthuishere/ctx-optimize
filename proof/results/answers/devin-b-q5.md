**Merge implementation:** `bio_attempt_back_merge` in block/blk-merge.c:941-970 (with low-level logic in `ll_back_merge_fn` at block/blk-merge.c:573-589)

**Gatekeeper function:** `blk_rq_merge_ok` in block/blk-merge.c:895-919

The call chain is: `blk_attempt_bio_merge` → `blk_rq_merge_ok` (gatekeeper) → `bio_attempt_back_merge` → `ll_back_merge_fn` (actual merge).