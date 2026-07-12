This repo (a Linux kernel tree) has three pluggable block-layer (blk-mq) I/O schedulers under `block/`:

1. **mq-deadline** (`block/mq-deadline.c`) — enforces per-request read/write expiration deadlines (soft limits, default 500ms reads / 5s writes) so no request starves indefinitely, while still batching sequential requests for throughput. Dispatch entry point: `dd_dispatch_request()` at `block/mq-deadline.c:452` (delegates to `__dd_dispatch_request()` at `block/mq-deadline.c:325`).

2. **kyber** (`block/kyber-iosched.c`) — controls tail latency by dynamically throttling per-domain (read/write/discard/other) in-flight queue depths to hit target latencies, rather than using explicit deadlines. Dispatch entry point: `kyber_dispatch_request()` at `block/kyber-iosched.c:789`.

3. **BFQ** (`block/bfq-iosched.c`) — a proportional-share scheduler (Budget Fair Queueing, B-WF2Q+) that assigns sector-based budgets to per-process queues by weight, with extra privileging of interactive/soft-real-time queues for low latency. Dispatch entry point: `bfq_dispatch_request()` at `block/bfq-iosched.c:5298` (delegates to `__bfq_dispatch_request()` at `block/bfq-iosched.c:5157`).

Each is registered as an `elevator_type` with its function wired to the `.ops.dispatch_request` field (`mq-deadline.c:994`, `kyber-iosched.c:1008`, `bfq-iosched.c:7597`), which `blk_mq_run_hw_queue()` → `__blk_mq_sched_dispatch_requests()` calls into.