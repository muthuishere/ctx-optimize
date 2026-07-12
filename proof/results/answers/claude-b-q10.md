## Writeback throttling wait path

**Hook:** `wbt_wait()` (block/blk-wbt.c:646-662) is registered as the `.throttle` callback of the wbt `rq_qos_ops` (`block/blk-wbt.c:947`). It's invoked generically via `rq_qos_throttle(q, bio)` (block/blk-rq-qos.h:160-166), which every bio submission goes through: `blk_mq_submit_bio()` / `blk_mq_get_new_requests()` in block/blk-mq.c call `rq_qos_throttle()` → `__rq_qos_throttle()` (block/blk-rq-qos.c:62-69) → iterates the `rq_qos` chain calling each qos's `.throttle` op, which for wbt is `wbt_wait`.

**Decision + wait:** `wbt_wait()` computes `bio_to_wbt_flags()` for the bio, and if the bio should be tracked (`blk_stat_is_active`) it calls `__wbt_wait(rwb, wb_acct, bio->bi_opf)` (block/blk-wbt.c:588-599). `__wbt_wait()` looks up the right `rq_wait` bucket via `get_rq_wait()` (block/blk-wbt.c:175-184, selects read/write/kswapd/discard queue based on `wbt_flags`) and calls `rq_qos_wait()` (block/blk-rq-qos.c) — this is the actual function that puts the writer to sleep until the in-flight-request limit allows it through, using the `wbt_wait_data` callback to test whether to proceed.

**Files involved:**
- `block/blk-wbt.c` — `wbt_wait`, `__wbt_wait`, `get_rq_wait`, and the `.throttle = wbt_wait` hookup (line 947)
- `block/blk-rq-qos.h` / `block/blk-rq-qos.c` — the generic `rq_qos_throttle()` / `__rq_qos_throttle()` dispatch and `rq_qos_wait()` sleep primitive
- `block/blk-mq.c` — `blk_mq_submit_bio()` / `blk_mq_get_new_requests()`, where a submitted bio actually reaches the `rq_qos_throttle()` call