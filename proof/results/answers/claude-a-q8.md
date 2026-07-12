Files containing calls to `blk_mq_run_hw_queue()` (excluding comment-only mentions):

- `block/blk-mq.c` — definition (line 2321) plus many call sites (e.g. 1430, 1492, 1878, 2201, 2404, 2485, 2511, 2589, 2744, 2914, 3206, 3823, etc.)
- `block/blk-mq-sched.c:44,331`
- `block/blk-mq-tag.c:175`
- `block/kyber-iosched.c:688`

`block/mq-deadline.c` only references it in a comment (line 445), no actual call.