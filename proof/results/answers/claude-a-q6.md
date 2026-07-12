## Budget timeout in BFQ

**Default value:** `bfq_timeout = HZ / 8` (block/bfq-iosched.c:204) — 125 ms, tunable via `timeout_sync` sysfs attr and scaled per-queue by a weight-raising coefficient.

**Set when a queue enters service:** `bfq_set_budget_timeout()` (block/bfq-iosched.c:3285-3299) computes
```c
bfqq->budget_timeout = jiffies + bfqd->bfq_timeout * timeout_coeff;
```
called from `__bfq_set_in_service_queue()` right after a `bfqq` is selected to run.

**Checked by:** `bfq_bfqq_budget_timeout()` (block/bfq-iosched.c:4460-4463):
```c
return time_is_before_eq_jiffies(bfqq->budget_timeout);
```
Per the comment above it (4455-4458), there's no dedicated timer — it's polled on request arrival/completion (`bfq_bfqq_may_expire`/dispatch paths at 6086, 6125) and on idle-timer expiration (`bfq_idle_slice_timer_body`, block/bfq-iosched.c:7025).

**What happens when it fires:** the queue is expired via `bfq_bfqq_expire(bfqd, bfqq, ..., BFQQE_BUDGET_TIMEOUT)` (block/bfq-iosched.c:4335, reason set at 3038/6127/6467/7031):
1. `__bfq_bfqq_recalc_budget()` doubles the queue's budget (capped at `bfq_max_budget`) for the *next* time it's served — reasoning at 4024-4032: give it a chance in case the timeout was a fluke (e.g. ZBR) rather than seekiness.
2. `__bfq_bfqq_expire()` (3890) either removes the queue from the active tree (`bfq_del_bfqq_busy`) if it has no backlog/outstanding requests, or requeues it (`bfq_requeue_bfqq`) if it still has pending I/O — then calls `__bfq_bfqd_reset_in_service()` (3946) to clear `bfqd->in_service_queue`, so BFQ picks a new queue to serve.
3. Because `reason == BFQQE_BUDGET_TIMEOUT`, `entity->service` is reset to 0 (4430-4431) rather than being preserved for a future non-blocking wait.