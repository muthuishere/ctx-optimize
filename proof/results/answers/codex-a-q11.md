Normal read/write BIO path:

1. Submission entry: `submit_bio()` accounts read/write I/O, sets ioprio, then calls `submit_bio_noacct()` at `block/blk-core.c:952`.

2. Validation and throttling: `submit_bio_noacct()` checks operation support, RO/EOD/remap/flush handling, then `blk_throtl_bio()` before `submit_bio_noacct_nocheck()` at `block/blk-core.c:817` and `block/blk-core.c:919`.

3. MQ handoff: `submit_bio_noacct_nocheck()` traces/enqueues recursive submissions and routes normal mq devices through `__submit_bio_noacct_mq()`, which calls `__submit_bio()`; `__submit_bio()` enters `blk_mq_submit_bio()` at `block/blk-core.c:762`, `block/blk-core.c:749`, `block/blk-core.c:670`.

4. Limits and split: `blk_mq_submit_bio()` enters the queue, checks alignment/polling, then calls `__bio_split_to_limits()` at `block/blk-mq.c:3093` and `block/blk-mq.c:3146`. For read/write, `__bio_split_to_limits()` calls `bio_split_rw()` if needed at `block/blk.h:429`; the actual split/resubmit machinery is `bio_submit_split_bioset()` / `bio_split_io_at()` / `bio_split_rw()` at `block/blk-merge.c:119`, `block/blk-merge.c:335`, `block/blk-merge.c:421`.

5. Merge: still inside `blk_mq_submit_bio()`, `blk_mq_attempt_bio_merge()` first tries current-task plug merging via `blk_attempt_plug_merge()`, then scheduler/software-queue merging via `blk_mq_sched_bio_merge()` at `block/blk-mq.c:3003`. Plug merge is `block/blk-merge.c:1082`; scheduler merge is `block/blk-mq-sched.c:335`; elevator lookup is `elv_merge()` at `block/elevator.c:268`.

6. Request creation: if not merged, `blk_mq_get_new_requests()` allocates/throttles tags/requests, and `blk_mq_bio_to_request()` attaches the BIO to the request at `block/blk-mq.c:3015` and `block/blk-mq.c:2654`.

7. Plug stage: if `current->plug` exists, `blk_mq_submit_bio()` calls `blk_add_rq_to_plug()` and returns at `block/blk-mq.c:3197`; the plug list is maintained at `block/blk-mq.c:1377`. `blk_finish_plug()` flushes through `__blk_flush_plug()` -> `blk_mq_flush_plug_list()` at `block/blk-core.c:1290`, `block/blk-core.c:1262`, `block/blk-mq.c:2938`.

8. Scheduler/queue insertion: unplugged or scheduler-needed requests go through `blk_mq_insert_request()` at `block/blk-mq.c:2592`; it either calls elevator `insert_requests()` or appends to the per-context software queue at `block/blk-mq.c:2633`.

9. Dispatch: `blk_mq_run_hw_queue()` runs `blk_mq_sched_dispatch_requests()` at `block/blk-mq.c:2321`; that drains `hctx->dispatch`, scheduler, or software queues in `__blk_mq_sched_dispatch_requests()` at `block/blk-mq-sched.c:268`.

10. Driver call: final dispatch is `blk_mq_dispatch_rq_list()`, which calls the driver’s `q->mq_ops->queue_rq(hctx, &bd)` at `block/blk-mq.c:2085` and `block/blk-mq.c:2117`. Direct issue can bypass queueing through `blk_mq_try_issue_directly()` -> `__blk_mq_issue_directly()`, which also calls `queue_rq`, at `block/blk-mq.c:2737` and `block/blk-mq.c:2694`.