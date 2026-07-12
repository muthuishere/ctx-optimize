## BFQ budget timeout

**What it is:** `bfqq->budget_timeout` — a jiffies deadline set whenever a queue becomes in-service, giving it a max time-slice to consume its budget (bounds seeky processes' impact on throughput).

- Set by `bfq_set_budget_timeout()` (`block/bfq-iosched.c:3285-3299`), called from `__bfq_set_in_service_queue()`:
  ```
  bfqq->budget_timeout = jiffies + bfqd->bfq_timeout * timeout_coeff;
  ```
  `timeout_coeff` is 1 normally, or `weight/orig_weight` for weight-raised queues (longer slice for boosted queues).

**Check function:** `bfq_bfqq_budget_timeout()` (`block/bfq-iosched.c:4460-4463`):
```c
static bool bfq_bfqq_budget_timeout(struct bfq_queue *bfqq)
{
    return time_is_before_eq_jiffies(bfqq->budget_timeout);
}
```
Not a real timer — checked opportunistically on request arrival/completion and idle-timer expiration (see comment at 4455-4459). It's wrapped by `bfq_may_expire_for_budg_timeout()` (4473-4485), which also requires the queue not be usefully waiting (or have used less than 1/3 budget), and is called from `bfq_select_queue()` (`block/bfq-iosched.c:6465-6467`) and `bfq_completed_request()`.

**What happens when it fires:** the in-service queue is expired with reason `BFQQE_BUDGET_TIMEOUT`:
```c
bfq_bfqq_expire(bfqd, bfqq, false, BFQQE_BUDGET_TIMEOUT);   // e.g. block/bfq-iosched.c:6465-6467
```
Inside `bfq_bfqq_expire()` (4335-4453):
- `__bfq_bfqq_recalc_budget()` (case `BFQQE_BUDGET_TIMEOUT`, line 4024-4032) **doubles** the queue's next budget (capped at `bfq_max_budget`) — rewarding it in case the timeout was just bad luck (e.g. ZBR) rather than seekiness.
- `__bfq_bfqq_expire()` (3890-3947) requeues/deactivates the queue (`bfq_del_bfqq_busy` or `bfq_requeue_bfqq`), then calls `__bfq_bfqd_reset_in_service()`, which clears `bfqd->in_service_queue` (sets it to `NULL`).
- BFQ's dispatch path then invokes `bfq_select_queue()` again to pick a new in-service queue.