Based on my ctx-optimize store queries and code inspection, here's the answer:

**wbt_wait function:** `block/blk-wbt.c:646-662`
- Signature: `static void wbt_wait(struct rq_qos *rqos, struct bio *bio)`
- Calls `__wbt_wait()` which invokes `rq_qos_wait()` to actually sleep

**rq_qos hook:** `throttle` hook in `struct rq_qos_ops` (defined in `block/blk-rq-qos.h:37-49`)
- The `throttle` field is registered to point to `wbt_wait` for the writeback throttling module

**How a bio reaches it:**
1. `blk_mq_submit_bio()` calls `rq_qos_throttle()` inline function (`block/blk-rq-qos.h:160-166`)
2. That calls `__rq_qos_throttle()` (`block/blk-rq-qos.c:62-69`) which iterates the rq_qos chain
3. The chain invokes each module's `throttle` callback, including `wbt_wait()`

**Throttling decision:** Made in `wbt_should_throttle()` (`block/blk-wbt.c:601+`), which evaluates:
- Current inflight I/O counters
- Read vs. write ratios (tracks window statistics)
- Configured latency targets
- Current writeback window and inflight limits

**Files involved:** `block/blk-wbt.c`, `block/blk-rq-qos.c`, `block/blk-rq-qos.h`, `block/blk-mq.c`