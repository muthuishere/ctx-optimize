## Budget timeout in BFQ

**What it is:** `bfqq->budget_timeout` is a jiffies-based deadline set each time a queue becomes the in-service queue, by `bfq_set_budget_timeout()` (`block/bfq-iosched.c:3285-3299`):

```c
bfqq->budget_timeout = jiffies + bfqd->bfq_timeout * timeout_coeff;
```

`bfqd->bfq_timeout` is BFQ's base time-slice (tunable via sysfs `timeout_sync`), scaled by `timeout_coeff` (weight-based, used for weight-raised real-time queues). It's the max wall-clock time a queue is allowed to hold the disk, meant to bound seeky/slow processes even if they haven't exhausted their I/O budget (comment at 3280-3283).

**Check function:** `bfq_bfqq_budget_timeout()` (`block/bfq-iosched.c:4460-4463`):

```c
static bool bfq_bfqq_budget_timeout(struct bfq_queue *bfqq)
{
    return time_is_before_eq_jiffies(bfqq->budget_timeout);
}
```

Per the comment above it (4455-4459): there's no dedicated timer — it's polled on request arrivals/completions and idle-timer expirations. It's wrapped by `bfq_may_expire_for_budg_timeout()` (4473-4485), which also requires the queue not be waiting for a request (or to have consumed ≥1/3 of its budget), to avoid the timestamp-misalignment problem. Callers: `bfq_select_queue()` (4818) and `bfq_completed_request()` (6465).

**What fires when it triggers:** at 6465-6467 (and similarly in `bfq_select_queue`):
```c
} else if (bfq_may_expire_for_budg_timeout(bfqq))
    bfq_bfqq_expire(bfqd, bfqq, false, BFQQE_BUDGET_TIMEOUT);
```

`bfq_bfqq_expire()` (`block/bfq-iosched.c:4335-4453`) then:
1. Checks if the queue was "slow" and, for `BFQQE_BUDGET_TIMEOUT` with ≥1/3 budget left, charges it time rather than actual service (favors sequential workloads, doesn't punish unlucky timeouts).
2. Recalculates/adjusts the queue's budget via `__bfq_bfqq_recalc_budget()` per the `BFQQE_BUDGET_TIMEOUT` case (case at line 4024).
3. Calls `__bfq_bfqq_expire()`, which — via `bfq-wf2q.c::__bfq_bfqd_reset_in_service` — clears `bfqd->in_service_queue`, removes the queue from the active tree if it's no longer busy (`bfq_del_bfqq_busy`), or requeues it (`bfq_requeue_bfqq`) if still busy, so a new queue is subsequently selected as in-service.
4. Resets `entity->service` (and parent entities' service) unless the queue may resume with its leftover budget.

Net effect: the in-service queue is de-scheduled from the disk, its budget/weight bookkeeping updated, and BFQ's scheduler picks a different queue to serve next.