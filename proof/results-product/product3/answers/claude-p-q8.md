Files containing calls to `blk_mq_run_hw_queue()`:

- `block/blk-mq.c` — definition site (`block/blk-mq.c:2321`) plus several internal callers (e.g. `blk_mq_run_hw_queues`, `blk_mq_insert_requests`, `blk_mq_dispatch_rq_list`, `blk_mq_start_hw_queue`)
- `block/blk-mq-sched.c` — `__blk_mq_sched_restart`, `blk_mq_sched_dispatch_requests`
- `block/blk-mq-tag.c` — `blk_mq_get_tag`
- `block/mq-deadline.c`
- `block/kyber-iosched.c` — `kyber_domain_wake`
- `drivers/block/rnbd/rnbd-clt.c` — `rnbd_clt_dev_requeue`