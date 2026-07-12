**Writeback throttling (wbt) wait function and hook:**

**wbt_wait function:** `wbt_wait` — **block/blk-wbt.c:646**
- Calls `__wbt_wait` (line 588) for actual throttling logic
- Uses `wbt_inflight_cb` callback (line 572) to decide wait via `rq_wait_inc_below(rqw, get_limit(...))`

**How throttling decides to wait:**
- Compares inflight request counter against a per-bio-type limit (from `get_limit` at line 537)
- The limit varies based on request type/flags (REQ_BACKGROUND, REQ_HIPRIO, etc.) and recent I/O patterns
- If `atomic_inc_below(&rqw->inflight, limit)` fails (counter at/above limit), the caller waits on `rqw->wait` queue via `rq_qos_wait` (block/blk-rq-qos.c:254)

**rq_qos hook:** `.throttle` — **block/blk-rq-qos.h:38**
- Implemented as `wbt_wait` in `wbt_rqos_ops` — **block/blk-wbt.c:947**

**Bio submission path (hook entry point):** `rq_qos_throttle(q, bio)` call in `blk_mq_get_new_requests` — **block/blk-mq.c:3032**

**Files:**
- `block/blk-wbt.c` — throttling implementation
- `block/blk-rq-qos.h` — ops struct & inline throttle caller
- `block/blk-rq-qos.c` — `rq_qos_wait` wait mechanism
- `block/blk-mq.c` — bio submission calls the hook