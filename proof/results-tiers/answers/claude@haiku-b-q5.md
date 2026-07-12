**Function implementing the merge:** `bio_attempt_back_merge` — **block/blk-merge.c:941**

**Gatekeeper function:** `ll_back_merge_fn` — **block/blk-merge.c:573**

The gatekeeper is called at line 946 of `bio_attempt_back_merge` and must return nonzero to allow the merge. If it returns 0, the merge fails. `ll_back_merge_fn` performs validation checks (gap detection, max sector checks, etc.) before granting permission. Once approved, the actual merge happens at lines 962–964, linking the bio into the request's chain and updating metadata.