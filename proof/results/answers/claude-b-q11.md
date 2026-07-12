I have enough to trace the full lifecycle. Here's the onboarding overview:

## Bio lifecycle: submission → driver

**1. Entry / sanity checks**
- `submit_bio()` — `block/blk-core.c:952` — sets I/O priority, hands off to `submit_bio_noacct()`.
- `submit_bio_noacct()` — `block/blk-core.c:817` — validates the bio (`bio_check_eod`, `bio_check_ro`, zone-append checks, partition remap via `blk_partition_remap`), then calls `submit_bio_noacct_nocheck()` (`block/blk-core.c:762`).
- `__submit_bio_noacct()` / `__submit_bio_noacct_mq()` (`block/blk-core.c:705`/`749`) loop over a possible bio chain and call `__submit_bio()` (`block/blk-core.c:670`) per bio, which enters the queue (`bio_queue_enter`) and dispatches to `blk_mq_submit_bio()`.

**2. Limits / split**
- Inside `blk_mq_submit_bio()` (`block/blk-mq.c:3093-3217`), `__bio_split_to_limits()` (`block/blk.h:429`) is called first. It dispatches by op type to `bio_split_rw`, `bio_split_discard`, `bio_split_write_zeroes`, or `bio_split_zone_append` (all in `block/blk-merge.c`), which check the queue's `queue_limits` (max sectors, segments, discard granularity, etc.) and chain-split the bio if it's too big. The public wrapper for stacking drivers is `bio_split_to_limits()` (`block/blk-merge.c:479`).

**3. Request formation + front-end (early) merge attempt**
- `blk_mq_submit_bio()` builds/gets a `struct request` (`blk_mq_get_cached_request` / `blk_mq_get_new_requests`), converts the bio into it (`blk_mq_bio_to_request`), and tries a cheap merge first via `blk_mq_attempt_bio_merge()` — this covers software-queue/plug-list merges before a scheduler is even consulted.

**4. Plug (deferred batching)**
- If merge didn't consume the bio, and a `current->plug` exists, the request is queued onto it via `blk_add_rq_to_plug()` (`block/blk-mq.c:1377`), rather than being dispatched immediately. `blk_start_plug()`/`blk_start_plug_nr_ios()` (`block/blk-core.c:1214`/`1165`) establish the plug at the start of a batching context (e.g. a syscall like `io_submit`/`writeback`), and `blk_finish_plug()` (`block/blk-core.c:1290`) tears it down, calling `__blk_flush_plug()` to flush pending requests — which for mq queues means `blk_mq_flush_plug_list()` (`block/blk-mq.c:2938`).

**5. Merge (scheduler-level)**
- When a plug flushes or there's no scheduler-bypass, per-request merge attempts happen via `attempt_back_merge()`/`attempt_merge()` (`block/blk-merge.c:862`) calling into `bio_attempt_back_merge()` (`block/blk-merge.c:941`) which validates via `ll_back_merge_fn` and folds the bio into an adjacent existing request (updating segment/byte counts, handling "mixed merge" for failfast flags). `blk_mq_sched_try_insert_merge()` (`block/blk-mq-sched.c:370`) is the scheduler-aware entry point used when inserting into the I/O scheduler's own merge tracking (e.g. rbtree in mq-deadline/bfq).

**6. Scheduler dispatch selection**
- `blk_mq_flush_plug_list()` → `blk_mq_dispatch_list()` / `blk_mq_dispatch_queue_requests()` or, off the plug path, `blk_mq_run_hw_queue()` → `blk_mq_sched_dispatch_requests()` (`block/blk-mq-sched.c:317`) → `__blk_mq_sched_dispatch_requests()` (`block/blk-mq-sched.c:268`), which chooses between pulling from the hardware-context's own list (`blk_mq_do_dispatch_ctx`) and asking the active elevator for requests (`blk_mq_do_dispatch_sched`, which calls the scheduler's `->dispatch_request` op if one is attached; plain FIFO if none).

**7. Dispatch to driver**
- Everything funnels into `blk_mq_dispatch_rq_list()` (`block/blk-mq.c:2085-2211`), which pulls requests off the list, checks/gets dispatch budget, and calls the driver's queue callback: `q->mq_ops->queue_rq(hctx, &bd)` (`block/blk-mq.c:2117`, and the batched form `q->mq_ops->queue_rqs(rqs)` at `block/blk-mq.c:2827` for drivers supporting batched submission). This is the actual handoff to the block driver/hardware.

**Summary chain:** `submit_bio` → `submit_bio_noacct` → `__submit_bio` → `blk_mq_submit_bio` → [`__bio_split_to_limits`] → [`blk_mq_attempt_bio_merge`] → [`blk_add_rq_to_plug` / plug] → `blk_mq_flush_plug_list` → `blk_mq_sched_dispatch_requests` → `blk_mq_dispatch_rq_list` → `q->mq_ops->queue_rq`.