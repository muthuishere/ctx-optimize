Now I have everything needed to answer precisely.

## What the plug buffers

`struct blk_plug` (per-task, `current->plug`, set up in `blk_start_plug` — block/blk-core.c:1214) accumulates I/O across the plugged region rather than issuing it request-by-request:

- **`mq_list`** — blk-mq requests queued via `blk_add_rq_to_plug` (block/blk-mq.c:1377), appended by `blk_mq_submit_bio`.
- **`cached_rqs`** — pre-allocated ("cached") `struct request`s for reuse (`blk_mq_alloc_cached_request`, block/blk-mq.c:630).
- **`cb_list`** — unplug callbacks registered by stacked drivers (e.g. MD/RAID) via `blk_plug_cb`.
- **`rq_count`**, `multiple_queues`, `has_elevator`, `nowait`, `cur_ktime` — bookkeeping used to decide batching/merge behavior.

It's a batching buffer: instead of dispatching each `bio`/`request` immediately, they pile up in the plug so they can be merged/sorted and issued together, cutting per-I/O submission overhead.

## Flush entry points

- **Normal end:** `blk_finish_plug` (block/blk-core.c:1290) → `__blk_flush_plug` (block/blk-core.c:1262), which runs `flush_plug_callbacks`, `blk_mq_flush_plug_list` (block/blk-mq.c:2938), and frees `cached_rqs`.
- **Early flush** (before `blk_finish_plug` is reached) goes through the inline wrapper `blk_flush_plug(plug, async)` → `__blk_flush_plug`, called from:
  - `sched_submit_work()` — kernel/sched/core.c:7292 — flushes plugged I/O before a task voluntarily sleeps (deadlock avoidance).
  - `io_schedule_prepare()` — kernel/sched/core.c:8123 — flushes before `io_schedule()`/`io_schedule_timeout()`.
  - `bio_poll()` — block/blk-core.c:993 — flushes before polling for completion of the bio being waited on.
  - `writeback_sb_inodes()` — fs/fs-writeback.c:2101 — flushes when `need_resched()` is true, to push I/O out before `cond_resched()`.
  - `wakeup_flusher_threads()` — fs/fs-writeback.c:2567 — flushes before waking the flusher threads, so pending writeback is actually submitted.