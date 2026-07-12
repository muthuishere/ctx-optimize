I have enough to give a solid onboarding trace. Here's the bio lifecycle through the block layer:

## Bio lifecycle: submission → driver

**1. Entry — `submit_bio()`** `block/blk-core.c:952`
File systems/callers hand a fully-built `bio` here. Sets ioprio (`bio_set_ioprio`), then calls `submit_bio_noacct()`.

**2. Sanity checks — `submit_bio_noacct()`** `block/blk-core.c:817`
Validates the bio: `bio_check_eod` (end-of-device), `bio_check_ro`, `blk_check_zone_append`, `blk_partition_remap`, `should_fail_bio` (fault injection). Then calls `submit_bio_noacct_nocheck()` → `__submit_bio_noacct_mq()` → `__submit_bio()` `block/blk-core.c:670`, which enters the queue (`bio_queue_enter`) and dispatches into the mq layer via `blk_mq_submit_bio()`.

**3. Split (limits) — `__bio_split_to_limits()`** `block/blk.h:429`, called from `blk_mq_submit_bio()` at `block/blk-mq.c:3146`
Checks the bio against `queue_limits` (max sectors, segments, etc.) and splits it if needed via `bio_split_rw` / `bio_split_discard` / `bio_split_write_zeroes` / `bio_split_zone_append`. The remainder is re-submitted; only a limits-compliant bio proceeds.

**4. Merge attempt — `blk_mq_attempt_bio_merge()`** `block/blk-mq.c:3003`
Tries to fold the bio into an *existing* request instead of allocating a new one, two ways:
   - `blk_attempt_plug_merge()` `block/blk-merge.c:1082` — merge into the current task's plug list (cheap, no locks).
   - `blk_mq_sched_bio_merge()` `block/blk-mq-sched.c:335` — ask the I/O scheduler (`blk_bio_list_merge`) to merge into its queued requests.
   If either succeeds, the bio is absorbed and submission ends here (`goto queue_exit`).

**5. Request allocation — `blk_mq_get_new_requests()`** (or a cached request from the plug via `blk_mq_get_cached_request()`), called at `block/blk-mq.c:3170`
Builds a fresh `struct request` and populates it from the bio (`blk_mq_bio_to_request()`).

**6. Plug — `blk_add_rq_to_plug()`** `block/blk-mq.c:1377`, called at `block/blk-mq.c:3198`
If the current task has an active plug (`current->plug`, started by `blk_start_plug()`), the request is queued there instead of being dispatched immediately — batches submissions for the same task. Flushed later by `blk_finish_plug()` `block/blk-core.c:1290` → `__blk_flush_plug()` → `blk_mq_flush_plug_list()` `block/blk-mq.c:2938`, which fans out to `blk_mq_dispatch_list` / `blk_mq_dispatch_queue_requests`.

**7. Scheduler / direct issue — end of `blk_mq_submit_bio()`** `block/blk-mq.c:3202-3209`
Without a plug, the request either:
   - goes through the I/O scheduler: `blk_mq_insert_request()` + `blk_mq_run_hw_queue()` → `blk_mq_sched_dispatch_requests()` `block/blk-mq-sched.c:317` (when `RQF_USE_SCHED` or the hw queue is busy), or
   - is issued directly: `blk_mq_try_issue_directly()` `block/blk-mq.c:2737`.

**8. Dispatch to driver — `blk_mq_dispatch_rq_list()`** `block/blk-mq.c:2085`
The common dispatch loop (used by both scheduler and plug-flush paths): pulls requests off the list, preps them (`blk_mq_prep_dispatch_rq`), and hands each to the driver via `.queue_rq` (e.g. `drivers/block/rnull/rnull.rs::queue_rq` in the example null-block driver), handling `BLK_STS_RESOURCE`/busy backoff (`blk_mq_handle_dev_resource`) and requeuing (`blk_mq_delay_run_hw_queue`) as needed. The direct-issue path instead calls `__blk_mq_issue_directly()` `block/blk-mq.c:2679`, which invokes the same `.queue_rq` driver callback.

**Chain summary:** `submit_bio` → `submit_bio_noacct` → `__submit_bio` → `blk_mq_submit_bio` (split → merge-attempt → alloc/plug → scheduler-or-direct) → `blk_mq_dispatch_rq_list` / `__blk_mq_issue_directly` → driver `.queue_rq`.