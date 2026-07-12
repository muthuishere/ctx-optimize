This repo's block layer (`block/`) implements three pluggable I/O schedulers on the blk-mq stack:

**1. mq-deadline** (`block/mq-deadline.c`)
- Philosophy: enforces per-request expiration deadlines (separate read/write, priority-aware) to bound worst-case latency while still allowing some batching/sorting by sector for throughput — a modernized version of the classic deadline scheduler (comment at `block/mq-deadline.c:3-4`).
- Dispatch entry point: `dd_dispatch_request(struct blk_mq_hw_ctx *hctx)` at `block/mq-deadline.c:452`, registered as `.dispatch_request = dd_dispatch_request` (`block/mq-deadline.c:994`). It delegates the actual selection logic to `__dd_dispatch_request()` (`block/mq-deadline.c:325`).

**2. Kyber** (`block/kyber-iosched.c`)
- Philosophy: controls tail latency by throttling per-domain (read/write/discard/other) queue depths using token-bucket-style scalable techniques rather than deadlines or strict fairness accounting (comment at `block/kyber-iosched.c:2-4`).
- Dispatch entry point: `kyber_dispatch_request(struct blk_mq_hw_ctx *hctx)` at `block/kyber-iosched.c:789`, registered as `.dispatch_request = kyber_dispatch_request` (`block/kyber-iosched.c:1008`).

**3. BFQ — Budget Fair Queueing** (`block/bfq-iosched.c`)
- Philosophy: a proportional-share scheduler that assigns each process/cgroup a fair budget of service (à la CFQ lineage) while adding low-latency heuristics and full hierarchical/cgroup scheduling support (comment at `block/bfq-iosched.c:15-19`).
- Dispatch entry point: `bfq_dispatch_request(struct blk_mq_hw_ctx *hctx)` at `block/bfq-iosched.c:5298`, registered as `.dispatch_request = bfq_dispatch_request` (`block/bfq-iosched.c:7597`), delegating to `__bfq_dispatch_request()` (`block/bfq-iosched.c:5157`).

There is also the implicit **"none"** scheduler (no elevator attached — `blk-mq-sched.c`), which does no reordering/deadline/fairness logic at all and just dispatches requests in submission order directly via the hardware queue path (not a separate `.c` file/elevator_type — it's the absence of one).