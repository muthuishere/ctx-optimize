Three main I/O schedulers exist in this repo:

| Scheduler | Philosophy | Dispatch Entry Point |
|-----------|-----------|----------------------|
| **mq-deadline** | Ensures requests don't miss deadlines by expiring reads/writes after time limits and preventing writer starvation. | `dd_dispatch_request()` — block/mq-deadline.c:452 |
| **kyber** | Controls latency by throttling queue depths using scalable techniques and dividing work into request-type domains. | `kyber_dispatch_request()` — block/kyber-iosched.c:789 |
| **bfq** | Proportional-share scheduler that assigns sector-based budgets to processes and guarantees throughput proportional to weight while optimizing low latency for interactive applications. | `bfq_dispatch_request()` — block/bfq-iosched.c:5298 |

All three are registered as `.dispatch_request` callbacks in their respective `elevator_mq_ops` structures (confirmed at mq-deadline.c:994, kyber-iosched.c:1008, bfq-iosched.c:7597).