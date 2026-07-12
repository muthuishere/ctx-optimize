## Answer

**Wait function:** `wbt_wait()` in `block/blk-wbt.c:646` — the wbt-specific work is done by `__wbt_wait()` (`block/blk-wbt.c:588`), which calls the generic blocker `rq_qos_wait()` (`block/blk-rq-qos.c:254`) to actually sleep (`prepare_to_wait_exclusive`/schedule loop) until the inflight-count callback `wbt_inflight_cb()` (`block/blk-wbt.c:572`) succeeds.

**rq_qos hook:** `wbt_wait` is registered as the `.throttle` op in the `rq_qos_ops` for wbt: `block/blk-wbt.c:947` (`.throttle = wbt_wait,`). It's invoked generically through `rq_qos_throttle(q, bio)` → `__rq_qos_throttle(rqos, bio)` (`block/blk-rq-qos.h:160`, `block/blk-rq-qos.c:62`), which walks the `rq_qos` chain and calls `rqos->ops->throttle(rqos, bio)`.

**How a bio reaches it:** `blk_mq_submit_bio()` in `block/blk-mq.c` calls `rq_qos_throttle(rq->q, bio)` at `block/blk-mq.c:3165` (existing-request path) and again inside `blk_mq_get_new_requests()` at `block/blk-mq.c:3032` (new-request-allocation path).

**Files involved:**
- `block/blk-wbt.c` — `wbt_wait()`, `__wbt_wait()`, `wbt_inflight_cb()`, `wbt_rqos_ops` (`.throttle = wbt_wait`)
- `block/blk-rq-qos.c` / `block/blk-rq-qos.h` — `__rq_qos_throttle()`, `rq_qos_throttle()` inline wrapper, `rq_qos_wait()` generic sleep helper
- `block/blk-mq.c` — call sites where `rq_qos_throttle()` is invoked on the submission path