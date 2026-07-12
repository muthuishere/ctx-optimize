**Budget timeout:** Set in `bfq_set_budget_timeout` (block/bfq-iosched.c:3285-3298) as:
```c
bfqq->budget_timeout = jiffies + bfqd->bfq_timeout * timeout_coeff;
```
where `timeout_coeff` is 1 for soft real-time queues, otherwise `bfqq->entity.weight / bfqq->entity.orig_weight`.

**Function that checks it:** `bfq_bfqq_budget_timeout` (block/bfq-iosched.c:4460-4463):
```c
static bool bfq_bfqq_budget_timeout(struct bfq_queue *bfqq)
{
    return time_is_before_eq_jiffies(bfqq->budget_timeout);
}
```

**What happens to the in-service queue when it fires:** The queue is expired via `bfq_bfqq_expire` with reason `BFQQE_BUDGET_TIMEOUT` (block/bfq-iosched.c:6126-6127, 5438). In `__bfq_bfqq_expire` (block/bfq-iosched.c:3890):
- If empty and not preempted with idling needed: removed from busy list via `bfq_del_bfqq_busy` (line 3927)
- Otherwise: requeued via `bfq_requeue_bfqq` (line 3929)

The budget is then recalculated in `__bfq_bfqq_recalc_budget` (block/bfq-iosched.c:4024-4031) with the `BFQQE_BUDGET_TIMEOUT` case, which doubles the budget to boost throughput for non-seeky processes.