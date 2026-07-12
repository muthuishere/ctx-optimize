Based on the BFQ scheduler code:

**Budget timeout value:** `HZ / 8` jiffies (line 204 `block/bfq-iosched.c`), multiplied by a timeout coefficient based on queue weight. It's calculated in `bfq_set_budget_timeout()` at line 3297–3298 as:
```c
bfqq->budget_timeout = jiffies + bfqd->bfq_timeout * timeout_coeff;
```

**Function that checks it:** `bfq_bfqq_budget_timeout()` at line 4460 `block/bfq-iosched.c`:
```c
static bool bfq_bfqq_budget_timeout(struct bfq_queue *bfqq)
{
    return time_is_before_eq_jiffies(bfqq->budget_timeout);
}
```

**What happens when it fires:** The in-service queue is **expired** (removed from service). When a new request arrives and the budget timeout has expired, `bfq_bfqq_expire()` is called with the `BFQQE_BUDGET_TIMEOUT` reason (lines 6125–6127 `block/bfq-iosched.c`):
```c
if (budget_timeout)
    bfq_bfqq_expire(bfqd, bfqq, false,
            BFQQE_BUDGET_TIMEOUT);
```