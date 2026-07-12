## What the plug buffers

`struct blk_plug` (initialized in `blk_start_plug_nr_ios`, block/blk-core.c:1165-1189) accumulates:
- `plug->mq_list` — the queued `struct request`s built from submitted bios, appended by `blk_add_rq_to_plug` (block/blk-mq.c:1377-1401), not yet dispatched to the driver/hardware queue.
- `plug->cached_rqs` — pre-allocated request(s) cached for reuse.
- `plug->cb_list` — per-driver "unplug" callbacks (`blk_plug_cb`, registered via `blk_check_plugged`, block/blk-core.c:1207-1219) that also fire on flush.

So plugging defers request *dispatch*, batching several bios into `mq_list` before pushing them into the device's hardware queue(s), improving merging/batching.

## Where it's flushed early (before an explicit `blk_finish_plug`)

1. **Plug overflow / large I/O** — `blk_add_rq_to_plug` (block/blk-mq.c:1383-1389): when `plug->rq_count >= blk_plug_max_rq_count(plug)` (`BLK_MAX_REQUEST_COUNT`, doubled for multi-queue) or the accumulated request size hits `BLK_PLUG_FLUSH_SIZE`, it calls `blk_mq_flush_plug_list(plug, false)` directly.

2. **Task about to sleep** — `blk_flush_plug(tsk->plug, true)` called from:
   - `sched_submit_work` — kernel/sched/core.c:7292
   - `io_schedule_prepare` — kernel/sched/core.c:8123
   
   (comment: "If we are going to sleep and we have plugged IO queued, make sure to submit it to avoid deadlocks.")

3. **Polling a bio** — `bio_poll` (block/blk-core.c:993) calls `blk_flush_plug(current->plug, false)` before entering the frozen-queue wait, so polled requests are actually issued.

4. **Writeback paths** — fs/fs-writeback.c:2101 (before `cond_resched()`, since resched doesn't unplug) and fs/fs-writeback.c:2567 (`wakeup_flusher_threads`), both call `blk_flush_plug(current->plug, ...)`.

All of these route through the same core routine as `blk_finish_plug`: `__blk_flush_plug` (block/blk-core.c:1262-1278), which calls `flush_plug_callbacks`, then `blk_mq_flush_plug_list` (block/blk-mq.c:2938-2968) to dispatch `mq_list`, then `blk_mq_free_plug_rqs` for any leftover `cached_rqs` — the difference is that `blk_finish_plug` (block/blk-core.c:1290-1296) additionally clears `current->plug`, while the early-flush callers leave the plug installed for further use.