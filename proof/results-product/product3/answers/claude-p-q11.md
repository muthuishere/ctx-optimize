I have enough to trace the full pipeline. Here's the onboarding overview.

## Bio lifecycle: submission → driver

**1. Submission entry**
- `submit_bio()` — block/blk-core.c:952 — public API filesystems call; accounts I/O stats, sets ioprio, calls `submit_bio_noacct()`.
- `submit_bio_noacct()` — block/blk-core.c:817 — validates the bio (NOWAIT support, crypto ctx, etc.), resolves `request_queue` from `bio->bi_bdev`, hands off toward `->submit_bio_noacct_nocheck()`.
- `submit_bio_noacct_nocheck()` — block/blk-core.c:762 — traces `block_bio_queue`, uses `current->bio_list` to prevent recursive `->submit_bio` stacking (handles stacked/DM devices), eventually reaches the queue's `submit_bio` op — for blk-mq that's `blk_mq_submit_bio()`.

**2. Split (limits)**
- `bio_submit_split_bioset()` — block/blk-merge.c:119 — checks segment/size limits and splits an oversized bio via `bio_split()` + `bio_chain()`, resubmitting the remainder through `submit_bio_noacct_nocheck()`.
- `bio_submit_split()` — block/blk-merge.c:142 — dispatches to per-op splitters (`bio_split_rw`, `bio_split_write_zeroes`, `bio_split_zone_append`, `__bio_split_discard`) based on request type.

**3. Core mq entry: `blk_mq_submit_bio()`** — block/blk-mq.c:3093-3217 — the central function that builds/sends a request:
  - Checks `current->plug` for a cached request first.
  - Computes segments (`nr_segs`), splits if needed.

**4. Merge**
- `blk_mq_attempt_bio_merge()` — block/blk-mq.c:3003 — tries, in order:
  - `blk_attempt_plug_merge()` — block/blk-merge.c:1082 — merge against the tail request on the *current task's plug list* (`plug->mq_list`), cheapest/no-lock path.
  - `blk_mq_sched_bio_merge()` — block/blk-mq-sched.c:335 — merge via the I/O scheduler's `bio_merge` hook, or the default per-software-queue (`ctx->rq_lists`) reverse-merge scan otherwise.
  - If either succeeds, the bio is folded into an existing `request` and the function returns early — no new request is allocated.

**5. Plug (deferred batching)**
- If no merge, `blk_mq_get_new_requests()` — block/blk-mq.c:3015 — allocates a fresh `struct request` via `__blk_mq_alloc_requests()` (using cached requests from the plug if present).
- `blk_mq_bio_to_request()` — block/blk-mq.c:2654 — copies bio fields into the new request (`rq->bio`, `__sector`, `__data_len`, segment count, integrity/crypto prep, `blk_account_io_start()`).
- The request is added to `current->plug->mq_list` (a `struct blk_plug`, block/blk-mq.c:3096) instead of being dispatched immediately, so subsequent bios in the same submission burst can plug-merge against it.
- Plug is flushed (explicitly via `blk_finish_plug()`/schedule-out, or when full) by `blk_mq_flush_plug_list()` — block/blk-mq.c:2938 — which routes to `blk_mq_dispatch_queue_requests()` / `blk_mq_dispatch_multiple_queue_requests()` for direct dispatch when there's no elevator, or inserts into the scheduler otherwise.

**6. Scheduler insertion**
- If the queue has an active elevator (or dispatch needs queuing), requests go through `blk_mq_insert_request()` — block/blk-mq.c:2592 — which inserts into the elevator's queue (or `hctx->dispatch` for passthrough requests) rather than issuing directly.
- `blk_mq_sched_dispatch_requests()` / `__blk_mq_do_dispatch_sched()` (block/blk-mq-sched.c:319 / :87) later pull requests back out via the elevator's `dispatch_request` op (e.g. `bfq_insert_request` / `kyber_insert_requests` are the per-scheduler insert hooks — block/bfq-iosched.c:6237, block/kyber-iosched.c:576).

**7. Dispatch to hardware queue**
- `blk_mq_run_hw_queue()` — block/blk-mq.c:2321 — entry point to actually run a hardware context; checks quiesced/need-run state, otherwise runs inline or schedules `hctx->run_work` via `blk_mq_delay_run_hw_queue()` (block/blk-mq.c:2285) for async execution.
- `blk_mq_dispatch_rq_list()` — block/blk-mq.c:2085-2211 — the core dispatch loop: pulls requests off the list one by one, gets driver "budget," builds `struct blk_mq_queue_data bd`, and calls the driver.

**8. Driver handoff**
- `q->mq_ops->queue_rq(hctx, &bd)` — called at block/blk-mq.c:2117 (inside `blk_mq_dispatch_rq_list`) — this is the actual driver callback (e.g. NVMe/SCSI/virtio-blk's registered `queue_rq`), the final handoff from the block layer into the device driver.

### Summary chain
`submit_bio` → `submit_bio_noacct` → `submit_bio_noacct_nocheck` → `blk_mq_submit_bio` → [split: `bio_submit_split_bioset`] → [merge: `blk_attempt_plug_merge` / `blk_mq_sched_bio_merge`] → [new req: `blk_mq_get_new_requests` + `blk_mq_bio_to_request`] → [plug: `blk_plug->mq_list`, flushed by `blk_mq_flush_plug_list`] → [scheduler: `blk_mq_insert_request` / `blk_mq_sched_dispatch_requests`] → `blk_mq_run_hw_queue` → `blk_mq_dispatch_rq_list` → `q->mq_ops->queue_rq()` (driver).