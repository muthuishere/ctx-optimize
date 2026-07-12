I have everything needed. Here's the full trace.

## Bio lifecycle: submission → driver (blk-mq path)

**1. Entry / sanity checks — `submit_bio()` / `submit_bio_noacct()`**
`block/blk-core.c:952` `submit_bio()` sets ioprio, accounts stats, calls `submit_bio_noacct()` (`block/blk-core.c:817`), which validates the op, checks EOD (`bio_check_eod`, :843), remaps partitions (`blk_partition_remap`, :621), filters flush bios, then calls `submit_bio_noacct_nocheck()` (:762) which hands off to `__submit_bio()` (:670) → for a blk-mq (non-`->submit_bio`) device, dispatches straight to `blk_mq_submit_bio()`.

**2. Request-build entry — `blk_mq_submit_bio()`** (`block/blk-mq.c:3093`)
This is the real orchestrator for the mq path. It checks for a cached plug request, enters the queue (`bio_queue_enter`), then drives the remaining stages below.

**3. Limits / split — `__bio_split_to_limits()` → `bio_split_rw()`** (`block/blk-mq.c:3146`, `block/blk-merge.c:421`)
Computes segments and, if the bio exceeds `max_sectors`/segment/gap limits, splits it via `bio_split_rw_at()`/`bio_submit_split()` (`block/blk-merge.c` ~340-420); the remainder is resubmitted, the front part continues down this same function.

**4. Merge attempt — `blk_mq_attempt_bio_merge()`** (`block/blk-mq.c:3003`)
Two merge opportunities, tried in order:
- **Plug merge**: `blk_attempt_plug_merge()` (`block/blk-merge.c:1082`) — checks the tail (or recent entries) of `current->plug->mq_list` for a back/front merge via `blk_attempt_bio_merge()`/`blk_try_merge()`, cheaply, without touching the elevator.
- **Scheduler merge**: `blk_mq_sched_bio_merge()` (`block/blk-mq-sched.c:335`) — if an I/O scheduler is attached, calls its `e->type->ops.bio_merge` (e.g. `dd_bio_merge` for mq-deadline, or bfq's), otherwise falls back to `blk_bio_list_merge()` against the per-ctx software queue.

If either succeeds, the bio is absorbed into an existing request and submission ends here.

**5. Request allocation — `blk_mq_get_new_requests()` / `blk_mq_get_cached_request()`** (`block/blk-mq.c:3015`, :3049)
No merge → allocate (or reuse a plug-cached) `struct request` via `__blk_mq_alloc_requests()`, then populate it from the bio with `blk_mq_bio_to_request()` (:3182).

**6. Plug — insertion or flush**
`blk_add_rq_to_plug()` (`block/blk-mq.c:1377`) appends the new request to `current->plug->mq_list`, deferring actual dispatch — this is the batching stage that lets a syscall submitting many bios build up a request list before hitting hardware. It auto-flushes early via `blk_mq_flush_plug_list()` (`block/blk-mq.c:2938`) if the plug's request-count/byte threshold is hit. Plug is drained explicitly at `blk_finish_plug()`/schedule points, ultimately going through `blk_mq_flush_plug_list()` → `blk_mq_dispatch_queue_requests()`/`blk_mq_dispatch_list()`.

**7. Scheduler dispatch — `__blk_mq_sched_dispatch_requests()`** (`block/blk-mq-sched.c:268`, feeding into `blk_mq_do_dispatch_sched()` :176 and `blk_mq_do_dispatch_ctx()` :213)
When there *is* no plug (or the plug is flushed with an elevator attached), requests go through the scheduler's `dispatch_request` op (mq-deadline/bfq/kyber picking order) or round-robin per-ctx dequeue, respecting `blk_mq_get_dispatch_budget()`.

**8. Dispatch to driver — `blk_mq_try_issue_directly()` / `blk_mq_dispatch_rq_list()` → `queue_rq()`**
- Fast path with no plug and no busy scheduler: `blk_mq_submit_bio()` calls `blk_mq_run_dispatch_ops(q, blk_mq_try_issue_directly(hctx, rq))` (`block/blk-mq.c:3208`) → `blk_mq_try_issue_directly()` (:2737) gets budget+tag then calls `__blk_mq_issue_directly()` (:2679), which invokes `q->mq_ops->queue_rq(hctx, &bd)` — the actual driver callback (e.g. NVMe/SCSI `queue_rq`).
- List/batch path (plug flush, scheduler dispatch): `blk_mq_dispatch_rq_list()` (`block/blk-mq.c:2085`) loops the list, calling `blk_mq_prep_dispatch_rq()` then the same `q->mq_ops->queue_rq()` per request, handling `BLK_STS_RESOURCE`/`BLK_STS_DEV_RESOURCE` by requeuing (`blk_mq_handle_dev_resource`) and re-arming the hw queue via `blk_mq_run_hw_queue()` (`block/blk-mq.c:2321`).

**Summary chain:** `submit_bio` → `submit_bio_noacct` → `blk_mq_submit_bio` → `__bio_split_to_limits`(split) → `blk_mq_attempt_bio_merge`(plug-merge / sched-merge) → `blk_mq_get_new_requests`(alloc) → `blk_add_rq_to_plug`(plug) → `blk_mq_flush_plug_list`/`__blk_mq_sched_dispatch_requests`(scheduler) → `blk_mq_try_issue_directly`/`blk_mq_dispatch_rq_list` → `queue_rq()`(driver).