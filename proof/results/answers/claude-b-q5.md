**Implements the merge:** `bio_attempt_back_merge()` — block/blk-merge.c:941-970. Appends `bio` onto the tail of `req` (updates `biotail`, `__data_len`, mixed-merge accounting, etc.) after checks pass.

**Gatekeeper that decides if it's allowed:** `ll_back_merge_fn()` — block/blk-merge.c:573-589. It's called from within `bio_attempt_back_merge` and checks max sectors (`blk_rq_get_max_sectors`), segment gap (`req_gap_back_merge`), and segment count (`ll_new_hw_segment`), returning 0 (and marking the request nomerge) if disallowed.

Call relationship: `bio_attempt_back_merge` → `ll_back_merge_fn` → `req_gap_back_merge`.