That's just a comment reference, not an actual call. The real call sites are in:

- `block/blk-mq.c` — many callers (`blk_execute_rq`, `blk_mq_dispatch_list`, `blk_mq_dispatch_rq_list`, `blk_mq_run_hw_queues`, `blk_mq_submit_bio`, `blk_mq_start_hw_queue`, etc.)
- `block/blk-mq-sched.c` — `__blk_mq_sched_restart`, `blk_mq_sched_dispatch_requests`
- `block/blk-mq-tag.c` — `blk_mq_get_tag`
- `block/kyber-iosched.c` — `kyber_domain_wake`
- `drivers/block/rnbd/rnbd-clt.c` — `rnbd_clt_dev_requeue`

`block/mq-deadline.c` only mentions it in a comment, not an actual call.