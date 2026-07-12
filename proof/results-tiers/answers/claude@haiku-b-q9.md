Based on the code analysis, here's what the block plugging mechanism buffers and when it flushes early:

## What the plug buffers:

The `struct blk_plug` buffers three types of items (block/blk-core.c L1165-1189):

1. **`mq_list`** – multiqueue requests waiting to be submitted
2. **`cached_rqs`** – cached requests
3. **`cb_list`** – registered callbacks

## Early flush conditions (before `blk_finish_plug`):

**In `blk_add_rq_to_plug` (block/blk-mq.c L1377-1401):**

1. **Line 1383**: Request count exceeds limit:
   - `plug->rq_count >= blk_plug_max_rq_count(plug)` 
   - Max is `BLK_MAX_REQUEST_COUNT` (or 2× for multiple queues)

2. **Line 1384-1385**: Last request size exceeds threshold:
   - `blk_rq_bytes(last) >= BLK_PLUG_FLUSH_SIZE`

3. **Task schedule (kernel/sched/core.c L7292, L8123)**: 
   - `blk_flush_plug()` called when task goes to sleep
   - Via context switch handler and `io_schedule_prepare()`

## Flush functions:

- **`__blk_flush_plug(plug, from_schedule)`** — block/blk-core.c L1262-1278
  - Flushes callbacks via `flush_plug_callbacks(plug, from_schedule)` (L1265)
  - Flushes request list via `blk_mq_flush_plug_list(plug, from_schedule)` (L1266)
  - Frees cached requests via `blk_mq_free_plug_rqs(plug)` (L1274)