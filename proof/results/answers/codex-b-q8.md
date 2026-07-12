Files containing calls to `blk_mq_run_hw_queue()`:

- `block/blk-mq.c` — callers include `blk_execute_rq`, `blk_mq_dispatch_list`, `blk_mq_run_hw_queues`, etc. Caller set shown on `blk_mq_run_hw_queue` card at `block/blk-mq.c:2321`.
- `block/blk-mq-sched.c` — callers include `__blk_mq_sched_restart` and `blk_mq_sched_dispatch_requests`, shown on `blk_mq_run_hw_queue` card at `block/blk-mq.c:2321`.
- `block/blk-mq-tag.c` — caller `blk_mq_get_tag`, shown on `blk_mq_run_hw_queue` card at `block/blk-mq.c:2321`.

Source used: `ctx-optimize card blk_mq_run_hw_queue`.