## Answer

**Wait function:** `wbt_wait()` — `block/blk-wbt.c:646-662`. It decides whether to sleep via `wbt_should_throttle(bio)` (`block/blk-wbt.c:601-617`, checks write direction, `REQ_SWAP`/`REQ_IDLE`/discard rules) and, when `blk_stat_is_active()` and the check passes, calls `__wbt_wait()` (`block/blk-wbt.c:588-599`), which does `rq_qos_wait()` against the per-priority `rq_wait` bucket obtained from `get_rq_wait()` (`block/blk-wbt.c:175-184`) — that's where the writer actually blocks.

**rq_qos hook:** `wbt_wait` is registered as the `.throttle` callback in `wbt_rqos_ops` (`block/blk-wbt.c:946-947`), installed via `rq_qos_add(..., RQ_QOS_WBT, &wbt_rqos_ops)` at `block/blk-wbt.c:981`.

**How a bio reaches it:** the block-mq submit path calls the inline hook `rq_qos_throttle(q, bio)` — `block/blk-rq-qos.h:160-166` — from `blk_mq_submit_bio()`/`blk_mq_get_new_requests()` in `block/blk-mq.c`. That inline wraps `__rq_qos_throttle()` (`block/blk-rq-qos.c:62-69`), which walks the queue's `rq_qos` chain and invokes each registered `ops->throttle`, landing on `wbt_wait()` for the wbt policy.

**Files:** `block/blk-wbt.c` (wbt logic/hook registration), `block/blk-rq-qos.c` / `block/blk-rq-qos.h` (generic rq_qos dispatch), `block/blk-mq.c` (submission call site).