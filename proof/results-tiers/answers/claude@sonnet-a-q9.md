Now I have all the pieces to answer.

## What gets buffered

Between `blk_start_plug()` and `blk_finish_plug()`, the per-task `struct blk_plug` (attached at `current->plug` in `blk_start_plug_nr_ios`, block/blk-core.c:1165) buffers:

- **`plug->mq_list`** — a singly-linked list of not-yet-issued `struct request`s, appended by `blk_add_rq_to_plug()` (block/blk-mq.c:1377) via `rq_list_add_tail()`, with `plug->rq_count` tracking depth.
- **`plug->cached_rqs`** — pre-allocated but unused request tags cached for reuse by the next `bio` (block/blk-core.c:1177).
- **`plug->cb_list`** — a list of `blk_plug_cb` callbacks registered by drivers/MD/DM (`blk_check_plugged()`, block/blk-core.c:1237) to run their own deferred unplug logic.

## When it's flushed early (before `blk_finish_plug`)

1. **Request-count / size threshold while adding to the plug** — `blk_add_rq_to_plug()` (block/blk-mq.c:1377-1401) calls `blk_mq_flush_plug_list(plug, false)` (block/blk-mq.c:2938) when `plug->rq_count` reaches `blk_plug_max_rq_count()` (`BLK_MAX_REQUEST_COUNT`, doubled if `plug->multiple_queues`) or the last request's size hits `BLK_PLUG_FLUSH_SIZE` (block/blk-mq.c:1383-1386).

2. **Task blocking/scheduling** — `io_schedule_prepare()`/`__schedule()` in kernel/sched/core.c:7292 and kernel/sched/core.c:8123 call `blk_flush_plug(tsk->plug, true)` before the task goes to sleep, to avoid deadlocks (e.g. reclaim waiting on a page pinned by a plugged request).

3. **Polling for I/O completion** — `bio_poll()` (block/blk-core.c:993) calls `blk_flush_plug(current->plug, false)` before entering the queue to poll, so cookie/completion state is consistent.

All these routes funnel into **`__blk_flush_plug()`** (block/blk-core.c:1262), which runs the callbacks (`flush_plug_callbacks()`), dispatches `plug->mq_list` via `blk_mq_flush_plug_list()` (block/blk-mq.c:2938), and frees any leftover `plug->cached_rqs`. `blk_finish_plug()` (block/blk-core.c:1290) itself just calls `__blk_flush_plug(plug, false)` and clears `current->plug` — same underlying flush function, just the "normal"/non-early path.