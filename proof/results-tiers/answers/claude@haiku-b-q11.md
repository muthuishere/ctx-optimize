Based on the ctx-optimize store, here's the bio lifecycle through the modern blk-mq I/O layer:

## Entry → Limits/Split

**`submit_bio_noacct()` (block/blk-core.c:817)**
Entry point from FS/application. Handles accounting bypass and routes to `__submit_bio_noacct()`.

**`__bio_split_to_limits()` (block/blk.h:429)** 
Enforces queue limits (max segment count, max request size, etc.). Splits oversized bios via `bio_split_rw()`, `bio_split_discard()`, etc. Submits tail recursively.

## Router

**`__submit_bio()` → `blk_mq_submit_bio()` (block/blk-mq.c:3093)**
Routes to modern multi-queue handler. Checks queue state, gets hardware context mapping.

## Merge

**`blk_mq_attempt_bio_merge()` (block/blk-mq.c:3003)**
Tries to merge bio into existing requests:
- `blk_attempt_plug_merge()` — merges with requests in local CPU plug list
- `blk_mq_sched_bio_merge()` (block/blk-mq-sched.c:335) — merges with scheduler's pending requests via `blk_bio_list_merge()`

## Request Creation & Plug

**`blk_mq_bio_to_request()` (block/blk-mq.c)**
Converts bio to `struct request`. Gets cached or allocates new request.

**`blk_add_rq_to_plug()` (block/blk-mq.c:1377)**
Batches request into current CPU's `struct blk_plug`. On overflow (> `blk_plug_max_rq_count`), flushes via `blk_mq_flush_plug_list()`.

## Scheduler Insert

**`blk_mq_insert_request()` (block/blk-mq.c:2592)**
Inserts request into scheduler's queue tree. Marks hardware queue as pending via `blk_mq_hctx_mark_pending()`.

## Dispatch

**`blk_mq_run_hw_queue()` → `blk_mq_dispatch_rq_list()` (block/blk-mq.c:2085)**
Scheduler work handler. Pulls requests from elevator via `__blk_mq_sched_dispatch_requests()`, allocates driver budget, calls driver's `.queue_rq()` hook to send to hardware.

**Unplug path** (on syscall boundary): `blk_mq_flush_plug_list()` triggers dispatch immediately if plug accumulates requests.

---
**Key data structures:** `struct bio`, `struct request`, `struct blk_mq_hw_ctx` (per-CPU queue context), `struct blk_plug` (batching container), `struct elevator_queue` (scheduler state).