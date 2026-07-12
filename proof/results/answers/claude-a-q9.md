## What gets buffered

`struct blk_plug` (defined in `include/linux/blkdev.h`, not in this partial checkout) accumulates two things per-task (`current->plug`), initialized in `blk_start_plug_nr_ios` at `block/blk-core.c:1165`:

- **`plug->mq_list`** — a singly-linked list of not-yet-dispatched `struct request`s (added via `blk_add_rq_to_plug`, `block/blk-mq.c:1377`), enabling request merging before hitting the driver.
- **`plug->cached_rqs`** — pre-allocated free requests cached for reuse.
- **`plug->cb_list`** — deferred callbacks (`blk_plug_cb`) registered via `blk_check_plugged` (`block/blk-core.c:1237`), used e.g. by stacking drivers (md/dm) to batch their own flush logic.

## Where it's flushed early (before `blk_finish_plug`)

1. **Request-count / size threshold** — `blk_add_rq_to_plug` (`block/blk-mq.c:1377-1389`): when adding a new request, if `plug->rq_count >= blk_plug_max_rq_count(plug)` (32, or 64 if `plug->multiple_queues`) or the last queued request's size reaches `BLK_PLUG_FLUSH_SIZE` (128 KB) and the queue isn't `nomerges`, it calls `blk_mq_flush_plug_list(plug, false)` (`block/blk-mq.c:2938`) immediately.

2. **Bio polling** — `bio_poll` (`block/blk-core.c:993`) calls `blk_flush_plug(current->plug, false)` before polling, to make sure any plugged requests on the current task are actually submitted so they can be found/completed.

3. **Task scheduling / blocking** — per the doc comment on `blk_start_plug` (`block/blk-core.c:1191-1213`), if the task blocks (e.g. for memory allocation) between `blk_start_plug()` and `blk_finish_plug()`, the scheduler flushes the plug via `blk_flush_plug()`/`__blk_flush_plug(plug, from_schedule=true)` (`block/blk-core.c:1262`) to avoid deadlocks (e.g. reclaim needing a page pinned by a plugged request). `blk_io_schedule` (`block/blk-core.c:1299`) is the block layer's wrapper around `io_schedule()`/`io_schedule_timeout()` for this path. Note: the actual scheduler hook that invokes `blk_flush_plug()` on `schedule()` lives in `kernel/sched/core.c`, which isn't part of this sparse `block/`-only checkout.

In all early-flush cases the flush routine is `__blk_flush_plug` (`block/blk-core.c:1262`), which runs any `cb_list` callbacks, calls `blk_mq_flush_plug_list` to dispatch `mq_list`, and frees any leftover `cached_rqs`.