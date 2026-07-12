**Function that implements merging a new bio into the back of an existing request:**
- `bio_attempt_back_merge` in block/blk-merge.c:941

**Gatekeeper function that decides the merge is allowed:**
- `blk_mq_sched_allow_merge` in block/blk-mq-sched.h:81