The back-merge implementation is `bio_attempt_back_merge` in `block/blk-merge.c:941`.

The gatekeeper it calls to decide whether that back merge is allowed is `ll_back_merge_fn` in `block/blk-merge.c:573`. It checks limits/gaps/new segment feasibility before `bio_attempt_back_merge` links the bio onto the request.