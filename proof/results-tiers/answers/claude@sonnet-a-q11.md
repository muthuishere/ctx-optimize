Now I have the full trace. Here's the onboarding overview.

## Bio lifecycle: submission → driver (blk-mq path)

**1. Submission** — `submit_bio()` (block/blk-core.c:952) sets I/O priority/accounting, calls `submit_bio_noacct()` (blk-core.c:817), which validates the bio (EOD, partition remap, flush filtering, per-op checks), runs it through blk-throttle, then hands off to `submit_bio_noacct_nocheck()` (blk-core.c:762). That function manages the `current->bio_list` recursion-guard (for stacked/bio-based drivers) and ultimately calls `__submit_bio()` (blk-core.c:670), which for a blk-mq queue calls `blk_mq_submit_bio()` (blk-mq.c:3093) — the real entry point for the rest of the pipeline.

**2. Limits / split** — Inside `blk_mq_submit_bio()`, after taking a queue-usage reference (`bio_queue_enter`) and alignment checks, the bio is passed to `__bio_split_to_limits()` → `bio_split_rw()` (blk-merge.c:421), which uses `bio_split_rw_at()`/`get_max_io_size()` to check the bio against queue limits (max sectors, segments, etc.) and, if needed, splits off a front chunk sized to fit via `bio_split()` (blk-merge.c:122), re-submitting the remainder. This also computes `nr_segs` for later use.

**3. Merge attempt** — `blk_mq_attempt_bio_merge()` (blk-mq.c:3003) tries two merge paths before allocating a new request:
   - `blk_attempt_plug_merge()` (blk-merge.c:1082) — checks the tail (or a few) requests on the *current task's* plug list (`plug->mq_list`).
   - `blk_mq_sched_bio_merge()` (blk-mq-sched.c:335) — asks the I/O scheduler's `.bio_merge` op, or falls back to `blk_bio_list_merge()` (blk-merge.c:1113) over the software queue's per-ctx request list.
   
   Both bottom out in `blk_attempt_bio_merge()`, which does the actual front/back sector-adjacency merge. A successful merge ends the submission — no new request is built.

**4. Get/build request** — If no merge occurred, `blk_mq_get_new_requests()` (blk-mq.c:3015) allocates a `struct request` (possibly from the plug's `cached_rqs`), then `blk_mq_bio_to_request()` (blk-mq.c:3182) populates it from the bio (also where subsequent same-request bios get merged via `bio_attempt_back/front_merge` during future submissions).

**5. Plug** — If `current->plug` is set (blk-mq.c:3197), the new request is queued onto it via `blk_add_rq_to_plug()` rather than dispatched immediately — this batches requests issued in one syscall/context so they can be merged/sorted and flushed together. Plugging starts with `blk_start_plug()` (blk-core.c:1214) and ends with `blk_finish_plug()` (blk-core.c:1290) → `__blk_flush_plug()` (blk-core.c:1262) → `blk_mq_flush_plug_list()` (blk-mq.c:2938), which is also the unplug point.

**6. Scheduler insert vs. direct dispatch** — With no plug (or on unplug), the request either goes through the elevator or straight to hardware:
   - If an I/O scheduler is attached (`RQF_USE_SCHED`) or the hctx is busy, `blk_mq_insert_request()` queues it to the scheduler/software queue, then `blk_mq_run_hw_queue()` (blk-mq.c:2321) is called to kick dispatch.
   - Otherwise, `blk_mq_try_issue_directly()` (blk-mq.c:2737) tries to send it straight to the driver, bypassing the scheduler for the fast/no-contention path.
   
   `blk_mq_run_hw_queue()` → `blk_mq_sched_dispatch_requests()` (blk-mq-sched.c:317) → `__blk_mq_sched_dispatch_requests()`, which picks `blk_mq_do_dispatch_sched()` (elevator present) or `blk_mq_flush_busy_ctxs()` + `blk_mq_dispatch_rq_list()` (no elevator) to pull requests off the scheduler/software queues.

**7. Dispatch to driver** — `blk_mq_dispatch_rq_list()` (blk-mq.c:2085) is the final stage: it pulls requests off the hw queue's dispatch list and calls the driver via `q->mq_ops->queue_rq()` (blk-mq.c:2117) (or the batched `queue_rqs()`, blk-mq.c:2827/2871, for `.queue_rqs`-capable drivers). `BLK_STS_RESOURCE`/`DEV_RESOURCE` returns push the request back onto the dispatch list for a retry; anything else completes or ends the request.

**Summary chain:** `submit_bio` → `submit_bio_noacct` → `submit_bio_noacct_nocheck` → `__submit_bio` → `blk_mq_submit_bio` → (`bio_split_rw` split) → (`blk_attempt_plug_merge` / `blk_mq_sched_bio_merge`) → `blk_mq_get_new_requests`/`blk_mq_bio_to_request` → (`blk_add_rq_to_plug` or direct) → `blk_mq_flush_plug_list`/`blk_mq_run_hw_queue` → `blk_mq_sched_dispatch_requests` → `blk_mq_dispatch_rq_list` → `queue_rq()`.