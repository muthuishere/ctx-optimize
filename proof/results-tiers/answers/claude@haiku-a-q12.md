## I/O Schedulers in this Linux kernel repository

**1. Deadline (MQ Deadline)**
- **File:** `block/mq-deadline.c`
- **Philosophy:** Ensures requests are dispatched before their deadlines expire, with separate expiration times for reads and writes to prevent starvation.
- **Dispatch entry point:** `dd_dispatch_request()` — block/mq-deadline.c:452

**2. Kyber**
- **File:** `block/kyber-iosched.c`
- **Philosophy:** Controls latency by throttling queue depths based on target latencies for different request domains (read, write, discard, other).
- **Dispatch entry point:** `kyber_dispatch_request()` — block/kyber-iosched.c:789

**3. BFQ (Budget Fair Queueing)**
- **File:** `block/bfq-iosched.c`
- **Philosophy:** Proportional-share scheduler that assigns budgets (measured in sectors) to processes, distributing device throughput fairly while providing low-latency guarantees for interactive and soft real-time applications.
- **Dispatch entry point:** `bfq_dispatch_request()` — block/bfq-iosched.c:5298