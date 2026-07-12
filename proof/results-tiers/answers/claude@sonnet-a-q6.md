I have enough to answer.

## Budget timeout

**Value:** `bfq_timeout = HZ / 8` (block/bfq-iosched.c:204), i.e. 125ms, adjustable per-scheduler via `bfqd->bfq_timeout` (settable through the `timeout_sync` sysfs attr, block/bfq-iosched.c:7523).

**Set when a queue enters service:** `bfq_set_budget_timeout()` (block/bfq-iosched.c:3285-3299) computes
```
bfqq->budget_timeout = jiffies + bfqd->bfq_timeout * timeout_coeff;
```
(`timeout_coeff` scales the timeout up for weight-raised queues.)

**Checked by:** `bfq_bfqq_budget_timeout()` (block/bfq-iosched.c:4460-4463):
```c
static bool bfq_bfqq_budget_timeout(struct bfq_queue *bfqq)
{
	return time_is_before_eq_jiffies(bfqq->budget_timeout);
}
```
Per the comment above it (block/bfq-iosched.c:4455-4459), there's no dedicated timer — this is polled on request arrival/completion and idle-timer expiration. It's wrapped by `bfq_may_expire_for_budg_timeout()` (block/bfq-iosched.c:4473-4485), which additionally requires the queue not be usefully idling for more budget.

## What happens when it fires

The main call site is in the dispatch-completion path, block/bfq-iosched.c:6465-6467:
```c
} else if (bfq_may_expire_for_budg_timeout(bfqq))
    bfq_bfqq_expire(bfqd, bfqq, false, BFQQE_BUDGET_TIMEOUT);
```

`bfq_bfqq_expire()` (block/bfq-iosched.c:4335 onward):
1. Determines if the process was "slow" (`bfq_bfqq_is_slow()`), and for a budget-timeout expiration where the queue consumed ≥2/3 of its budget, charges it by elapsed *time* rather than service consumed (`bfq_bfqq_charge_time()`), to penalize seeky/slow processes.
2. Recomputes/adjusts `bfqq`'s budget for next time via `__bfq_bfqq_recalc_budget()` (line 4415).
3. Calls `__bfq_bfqq_expire()` (block/bfq-iosched.c:3890), which either marks the queue idle-since-now (if empty) and dequeues it (`bfq_del_bfqq_busy()`), or requeues it in the scheduling tree (`bfq_requeue_bfqq()`) if it still has pending requests — then calls `__bfq_bfqd_reset_in_service()`.
4. `__bfq_bfqd_reset_in_service()` (block/bfq-wf2q.c:1547-1585) is what actually clears the in-service queue: it cancels the idle-slice timer, sets `bfqd->in_service_queue = NULL`, resets `in_service_entity` up the scheduling tree, and drops the service reference to the queue (freeing it if that was the last reference).

After this, `bfqd->in_service_queue` is `NULL` and BFQ's next dispatch cycle must select a new queue via `bfq_select_queue()`/`__bfq_set_in_service_queue()`, which calls `bfq_set_budget_timeout()` again to arm a fresh timeout.