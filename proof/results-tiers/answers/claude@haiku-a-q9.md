The block plugging mechanism buffers **pending I/O requests** in `plug->mq_list` (a linked list of `struct request` objects), along with cached requests in `plug->cached_rqs` and callbacks in `plug->cb_list`.

**Early flush triggers (before `blk_finish_plug`):**

1. **Request count threshold** — when `plug->rq_count >= BLK_MAX_REQUEST_COUNT` (32, or 64 if `multiple_queues`)
2. **I/O size threshold** — when accumulated request size `blk_rq_bytes(last) >= BLK_PLUG_FLUSH_SIZE` (128KB)
3. **Task scheduling** — when the task blocks/sleeps before `blk_finish_plug` returns
4. **Polling** — when `bio_poll()` is called

**Flush functions and locations:**

- **`blk_add_rq_to_plug()`** (block/blk-mq.c:1377-1389) — checks conditions at lines 1383-1386, calls `blk_mq_flush_plug_list(plug, false)` at line 1386
- **`__blk_flush_plug()`** (block/blk-core.c:1262-1278) — main flush handler, calls `blk_mq_flush_plug_list()` at line 1266
- **`blk_mq_flush_plug_list()`** (block/blk-mq.c:2938-2968) — dispatches buffered requests to hardware queues
- **Task schedule flush** (kernel/sched/core.c:7292, 8123) — `blk_flush_plug(tsk->plug, true)` with `from_schedule=true`