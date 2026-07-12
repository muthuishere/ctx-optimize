Actual call sites appear in these files:

- `block/blk-mq-sched.c`: `__blk_mq_sched_restart()` at `block/blk-mq-sched.c:44`; `blk_mq_sched_dispatch_requests()` at `block/blk-mq-sched.c:331`
- `block/kyber-iosched.c`: `kyber_domain_wake()` at `block/kyber-iosched.c:688`
- `block/blk-mq.c`: calls from `blk_execute_rq_nowait()` `:1430`, `blk_execute_rq()` `:1492`, `blk_mq_dispatch_wake()` `:1878`, `blk_mq_dispatch_rq_list()` `:2201`, `blk_mq_run_hw_queues()` `:2404`, `blk_mq_start_hw_queue()` `:2485`, `blk_mq_start_stopped_hw_queue()` `:2511`, `blk_mq_insert_requests()` `:2589`, `blk_mq_try_issue_directly()` `:2744`, `blk_mq_request_issue_directly()` `:2775`, `blk_mq_issue_direct()` `:2810`, `blk_mq_dispatch_list()` `:2914`, `blk_mq_try_issue_list_directly()` `:2990`, `blk_mq_submit_bio()` `:3206`, `blk_mq_hctx_notify_dead()` `:3823`
- `block/blk-mq-tag.c`: `blk_mq_get_tag()` at `block/blk-mq-tag.c:175`

Excluded comment-only mentions and the definition of `blk_mq_run_hw_queue()` itself.