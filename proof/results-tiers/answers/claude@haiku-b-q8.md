Based on the ctx-optimize query, files that contain calls to `blk_mq_run_hw_queue()`:

1. **block/blk-mq.c** — defined here and calls itself internally
2. **block/blk-mq-sched.c** — via `__blk_mq_sched_restart` and `blk_mq_sched_dispatch_requests`
3. **block/blk-mq-tag.c** — via `blk_mq_get_tag`