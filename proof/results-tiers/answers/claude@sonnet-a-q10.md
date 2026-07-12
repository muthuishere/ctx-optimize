## Answer

**Wait function:** `wbt_wait()` in `block/blk-wbt.c:646` — checks `bio_to_wbt_flags()`; if the bio is `WBT_TRACKED` (writes/discards needing throttling, per `wbt_should_throttle()` at `block/blk-wbt.c:601`), it calls `__wbt_wait()` (`block/blk-wbt.c:588`), which calls the generic `rq_qos_wait()` (`block/blk-rq-qos.c:254`) with the `wbt_inflight_cb`/`wbt_cleanup_cb` callbacks — `rq_qos_wait` is what actually puts the task on a waitqueue until `get_limit()`'s inflight limit allows it through.

**rq_qos hook:** `wbt_wait` is registered as the `.throttle` callback in `rq_qos_ops` at `block/blk-wbt.c:947` (`struct rq_qos_ops wbt_rqos_ops = { .throttle = wbt_wait, ... }`).

**How a bio reaches it:** `blk_mq_submit_bio()` (`block/blk-mq.c:3093`) → `blk_mq_get_new_requests()` (`block/blk-mq.c:3015`) calls `rq_qos_throttle(q, bio)` at `block/blk-mq.c:3032`. `rq_qos_throttle()` is the inline wrapper in `block/blk-rq-qos.h:160`, which calls `__rq_qos_throttle(q->rq_qos, bio)` (declared `block/blk-rq-qos.h:107`, defined in `block/blk-rq-qos.c`), and that walks the `rq_qos` chain invoking each qos module's `ops->throttle`, landing on `wbt_wait`.

**Files involved:** `block/blk-wbt.c`, `block/blk-wbt.h`, `block/blk-rq-qos.c`, `block/blk-rq-qos.h`, `block/blk-mq.c`.