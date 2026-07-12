Based on the kernel source:

1. **Function implementing the merge:** `bio_attempt_back_merge()` at **block/blk-merge.c:941**
   - Merges a new bio into the back of an existing request

2. **Gatekeeper function:** `ll_back_merge_fn()` at **block/blk-merge.c:573**
   - Called on line 946 within `bio_attempt_back_merge()` to decide if the merge is allowed
   - Checks for gaps, integrity constraints, crypto context, and sector limits
   - Returns 0 (fail) or 1 (succeed)