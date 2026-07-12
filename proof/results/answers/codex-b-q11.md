Assumption: “normal” means a blk-mq block device read/write bio, not a stacking driver special path or zoned write-plug detour.

Lifecycle trace:

1. **Submission / validation**
   `submit_bio()` is the public entry; it sets priority then calls `submit_bio_noacct()` for block-layer submission: `block/blk-core.c:952`.
   `submit_bio_noacct()` does basic checks/remap such as EOD, read-only, partition remap, zone append, atomic write sizing, then enters `submit_bio_noacct_nocheck()`: `block/blk-core.c:817`.

2. **Recursive bio submit loop**
   `submit_bio_noacct_nocheck()` selects the blk-mq path via `__submit_bio_noacct_mq()`: `block/blk-core.c:762`, `block/blk-core.c:749`.
   `__submit_bio()` then hands normal blk-mq disks to `blk_mq_submit_bio()`: `block/blk-core.c:670`, call at `block/blk-core.c:673`.

3. **Limits / split**
   `blk_mq_submit_bio()` is the main conversion point from bio to request: `block/blk-mq.c:3093`.
   It applies queue limits with `__bio_split_to_limits()`: call at `block/blk-mq.c:3146`, helper at `block/blk.h:429`.
   The public wrapper for this logic is `bio_split_to_limits()`: `block/blk-merge.c:479`; split submission uses `bio_submit_split_bioset()`, which resubmits the remainder through `submit_bio_noacct_nocheck()`: `block/blk-merge.c:119`.

4. **Merge**
   Before allocating/issuing a fresh request, `blk_mq_submit_bio()` tries `blk_mq_attempt_bio_merge()`: call at `block/blk-mq.c:3155`, function at `block/blk-mq.c:3003`.
   That checks plug merging via `blk_attempt_plug_merge()`: `block/blk-merge.c:1082`, and scheduler-side bio merging via `blk_mq_sched_bio_merge()`: `block/blk-mq-sched.c:335`.

5. **Bio to request**
   If not merged, the bio is attached to a `struct request` by `blk_mq_bio_to_request()`: function at `block/blk-mq.c:2654`, call at `block/blk-mq.c:3182`.

6. **Plug**
   If the task has a plug and the request should be held for batching/merge, `blk_mq_submit_bio()` calls `blk_add_rq_to_plug()`: call at `block/blk-mq.c:3198`, function at `block/blk-mq.c:1377`.
   Plug flushing later feeds requests back into dispatch paths such as `blk_mq_dispatch_queue_requests()` / `blk_mq_dispatch_multiple_queue_requests()`: `block/blk-mq.c:2859`, `block/blk-mq.c:2925`.

7. **Scheduler / queue insertion**
   If not plugged and not issued directly, `blk_mq_submit_bio()` inserts the request with `blk_mq_insert_request()` and runs the hardware queue: calls at `block/blk-mq.c:3205` and `block/blk-mq.c:3206`.
   `blk_mq_run_hw_queue()` is the scheduler entry that calls `blk_mq_sched_dispatch_requests()`: `block/blk-mq.c:2321`, scheduler function at `block/blk-mq-sched.c:317`.

8. **Dispatch**
   `blk_mq_sched_dispatch_requests()` calls `__blk_mq_sched_dispatch_requests()`: `block/blk-mq-sched.c:317`, `block/blk-mq-sched.c:268`.
   That drains scheduler/context queues and calls `blk_mq_dispatch_rq_list()`: call sites around `block/blk-mq-sched.c:299` and `block/blk-mq-sched.c:313`, dispatcher at `block/blk-mq.c:2085`.

9. **Driver handoff**
   `blk_mq_dispatch_rq_list()` prepares budget/tag state through `blk_mq_prep_dispatch_rq()`: `block/blk-mq.c:2014`, then calls the low-level driver callback `q->mq_ops->queue_rq(hctx, &bd)`: `block/blk-mq.c:2117`.

Short form:

`submit_bio()` → `submit_bio_noacct()` → `__submit_bio_noacct_mq()` → `__submit_bio()` → `blk_mq_submit_bio()` → `__bio_split_to_limits()` → `blk_mq_attempt_bio_merge()` → `blk_mq_bio_to_request()` → plug via `blk_add_rq_to_plug()` or queue via `blk_mq_insert_request()` → `blk_mq_run_hw_queue()` → `blk_mq_sched_dispatch_requests()` → `blk_mq_dispatch_rq_list()` → `q->mq_ops->queue_rq()`.