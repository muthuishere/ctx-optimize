I have everything needed. This repo has three pluggable blk-mq I/O schedulers plus the implicit "none" (no-scheduler) fallback.

## I/O schedulers in this repo

| Scheduler | Philosophy | Dispatch entry point |
|---|---|---|
| **mq-deadline** | Adaptation of the legacy deadline scheduler to blk-mq: enforces per-request expiration deadlines (separate read/write FIFOs) to bound worst-case latency while still batching by sector order for throughput. | `dd_dispatch_request()` — `block/mq-deadline.c:452` (delegates to `__dd_dispatch_request()` at `block/mq-deadline.c:325`) |
| **bfq** (Budget Fair Queueing) | Proportional-share scheduling: assigns each process/queue a budget (in sectors, not time), scheduled via the B-WF2Q+ algorithm, so device throughput is distributed by configurable weight while still giving low latency to interactive/soft-real-time flows. | `bfq_dispatch_request()` — `block/bfq-iosched.c:5298` (delegates to `__bfq_dispatch_request()` at `block/bfq-iosched.c:5157`) |
| **kyber** | Controls tail latency by throttling queue depths per request-type "domain" (read/write/discard/other) using scalable token-bucket-style techniques, adjusting depth dynamically to hit target latencies rather than reordering by deadline or fairness weight. | `kyber_dispatch_request()` — `block/kyber-iosched.c:789` |
| **none** | Implicit no-op elevator: bypasses scheduling policy entirely and dispatches requests directly in submission order for lowest overhead (used for fast NVMe-class devices). | No `dispatch_request` op — requests go straight through `blk_mq_sched.c`'s dispatch path (`block/blk-mq-sched.c`) without an elevator hook. |

Each scheduler registers its ops via a `struct elevator_type` (`mq_deadline` at `block/mq-deadline.c:989`, `iosched_bfq_mq` at `block/bfq-iosched.c:7589`, `kyber_sched` at `block/kyber-iosched.c:993`), and dispatch is invoked from `blk_mq_run_hw_queue()` → `__blk_mq_sched_dispatch_requests()` in `block/blk-mq-sched.c`.