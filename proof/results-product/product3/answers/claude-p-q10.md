## How wbt makes a writer wait

**Hook path (bio → wbt):**
- `submit_bio`/`blk_mq_submit_bio` → `rq_qos_throttle(q, bio)` (`block/blk-rq-qos.h:160-166`)
- → `__rq_qos_throttle(rqos, bio)` (`block/blk-rq-qos.c:62-69`), which walks the `rq_qos` chain calling each qos's `.throttle` op
- wbt registers `wbt_wait` as that `.throttle` callback (in the `rq_qos_ops` for wbt, `block/blk-wbt.c`), so `__rq_qos_throttle` invokes `wbt_wait(rqos, bio)` — `block/blk-wbt.c:646-662`.

**Wait decision:**
- `wbt_wait()` (`block/blk-wbt.c:646-662`) computes `flags = bio_to_wbt_flags(rwb, bio)`. If `WBT_TRACKED` isn't set (e.g. reads/non-tracked ops), it just timestamps and returns — no wait.
- Otherwise it calls **`__wbt_wait(rwb, flags, bio->bi_opf)`** (`block/blk-wbt.c:588-599`) — this is the actual wbt wait function.
- `__wbt_wait` gets the per-class `rq_wait` queue via `get_rq_wait()` and blocks the writer with `rq_qos_wait(rqw, &data, wbt_inflight_cb, wbt_cleanup_cb)` (`block/blk-rq-qos.c`), where `wbt_inflight_cb` decides admission based on current inflight count vs. the computed latency-based limit.

**Files:** `block/blk-wbt.c` (wbt policy, `wbt_wait`/`__wbt_wait`), `block/blk-rq-qos.h` and `block/blk-rq-qos.c` (the generic `rq_qos` throttle hook/chain).