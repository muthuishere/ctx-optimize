Files containing calls to `blk_mq_run_hw_queue()`:

- `block/blk-mq.c` — definition (block/blk-mq.c:2321) plus internal callers (e.g. `blk_mq_run_hw_queues`, `blk_mq_insert_requests`, `blk_mq_dispatch_rq_list`, `blk_execute_rq`, `blk_mq_start_hw_queue`, etc.)
- `block/blk-mq-sched.c` — `__blk_mq_sched_restart`, `blk_mq_sched_dispatch_requests`
- `block/blk-mq-tag.c` — `blk_mq_get_tag`
- `block/mq-deadline.c` — deadline I/O scheduler dispatch/insert paths
- `block/kyber-iosched.c` — kyber I/O scheduler dispatch path