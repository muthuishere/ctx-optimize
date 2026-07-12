Now I have the full picture. Summary:

**What gets buffered** — `struct blk_plug` (per-`task_struct`, set via `current->plug` in `blk_start_plug_nr_ios`, block/blk-core.c:1165-1189) holds:
- `mq_list` — a singly-linked list of not-yet-issued `struct request`s (the actual queued I/O)
- `cached_rqs` — pre-allocated request(s) for reuse
- `cb_list` — plug callbacks registered by other layers (e.g. dm/md) to run at flush time
- bookkeeping: `rq_count`, `multiple_queues`, `has_elevator`, `nr_ios`

`blk_finish_plug` (block/blk-core.c:1290-1296) calls `__blk_flush_plug` (block/blk-core.c:1262-1278), which drains `cb_list` via `flush_plug_callbacks` and dispatches `mq_list` via `blk_mq_flush_plug_list` (block/blk-mq.c:2938-2968).

**Early flush (before blk_finish_plug):**

1. **`blk_add_rq_to_plug`** — block/blk-mq.c:1377-1401. When adding a new request to the plug, if `plug->rq_count >= blk_plug_max_rq_count(plug)` (32, or 64 if `multiple_queues` is set — `BLK_MAX_REQUEST_COUNT`, block/blk.h:351) **or** the queue disallows merges and the last queued request's size already reached `BLK_PLUG_FLUSH_SIZE` (128KB, block/blk.h:352), it calls `blk_mq_flush_plug_list(plug, false)` right there to flush the list early.

2. **Task scheduling out** — per the doc comment on `blk_start_plug` (block/blk-core.c:1189-1213): if the task blocks/sleeps between `blk_start_plug()` and `blk_finish_plug()`, the pending plug is auto-flushed (via the scheduler's `blk_flush_plug()` hook, called with `from_schedule=true`) to avoid deadlocks such as reclaim waiting on a page pinned by a request sitting in the plug.