**Budget Timeout Value:** `bfqd->bfq_timeout * timeout_coeff`, set in `bfq_set_budget_timeout()` at block/bfq-iosched.c:3297-3298. The timeout coefficient is based on whether the queue is in a real-time weighting class; otherwise it's derived from the queue's weight ratio.

**Checking Function:** `bfq_bfqq_budget_timeout()` at block/bfq-iosched.c:4460-4463. It returns `time_is_before_eq_jiffies(bfqq->budget_timeout)`, checking if the current time has passed the deadline.

**When Timeout Fires:** In `bfq_select_queue()` (block/bfq-iosched.c:4818-4820), if `bfq_may_expire_for_budg_timeout()` returns true and the queue doesn't need to idle, execution jumps to the `expire` label (line 5016-5017), which calls `bfq_bfqq_expire(bfqd, bfqq, false, BFQQE_BUDGET_TIMEOUT)` to expire the in-service queue. Then a new queue is selected via `bfq_set_in_service_queue(bfqd)` at line 5019.

The comment at line 3290-3293 explains the purpose: the budget timeout "prevents seeky processes from lowering the throughput. In practice, a time-slice service scheme is used with seeky processes."