Good, that's enough to answer.

## Budget timeout in BFQ

**Timeout value:** `bfqd->bfq_timeout` (a jiffies count, default `HZ/8` i.e. 125ms — see `BFQ_DEFAULT_GRP_TIMEOUT`/`bfq_timeout_sync_store` in block/bfq-iosched.c). It's set per-queue by `bfq_set_budget_timeout()` (block/bfq-iosched.c:3285-3299), which stamps `bfqq->budget_timeout = jiffies + bfqd->bfq_timeout * timeout_coeff` when a queue enters service (`__bfq_set_in_service_queue` calls it).

**Checking function:** `bfq_bfqq_budget_timeout()` (block/bfq-iosched.c:4460-4463):
```c
static bool bfq_bfqq_budget_timeout(struct bfq_queue *bfqq)
{
    return time_is_before_eq_jiffies(bfqq->budget_timeout);
}
```
Per the comment above it, there's no dedicated timer — this is polled on request arrivals/completions and idle-timer expirations. It's wrapped by `bfq_may_expire_for_budg_timeout()` (block/bfq-iosched.c:4473-4485), which also requires that the queue isn't actively waiting for a new request (or has used up ≥1/3 of its budget) before allowing expiration, to avoid the timestamp-misalignment problem.

**Callers that act on it:**
- `bfq_select_queue()` (block/bfq-iosched.c:6465-6467) — if `bfq_may_expire_for_budg_timeout(bfqq)` is true for the in-service queue, it calls `bfq_bfqq_expire(bfqd, bfqq, false, BFQQE_BUDGET_TIMEOUT)`.
- `bfq_rq_enqueued()` (block/bfq-iosched.c:6125-6127) — same expiry call on request enqueue if `budget_timeout` fired.
- `bfq_idle_slice_timer_body()` (block/bfq-iosched.c:5436-5439) — if the idle-slice timer fires on the in-service queue, calls `__bfq_bfqq_expire(bfqd, bfqq, BFQQE_BUDGET_TIMEOUT)` directly.

**What happens to the in-service queue when it fires (`bfq_bfqq_expire()`, block/bfq-iosched.c:4335-4453):**
1. `bfq_bfqq_is_slow()` determines if the queue was slow; combined with `reason == BFQQE_BUDGET_TIMEOUT` and having used ≥2/3 of its budget check (block/bfq-iosched.c:4364-4368), it may be charged for elapsed *time* instead of *service* (`bfq_bfqq_charge_time()`), penalizing timed-out/seeky queues in favor of sequential workloads.
2. `__bfq_bfqq_recalc_budget()` recomputes the queue's budget for its next service slot based on the expiration reason.
3. `__bfq_bfqq_expire()` removes it as `bfqd->in_service_queue`, reQs it into the scheduling tree if still busy.
4. Unlike other expiration reasons, `BFQQE_BUDGET_TIMEOUT` (along with `BFQQE_BUDGET_EXHAUSTED`) is excluded from the "mark as waiting a request" path (block/bfq-iosched.c:4421-4424), and in `__bfq_bfqq_recalc_budget` it causes the queue's budget to be **doubled** for future scheduling (block/bfq-iosched.c:4024-4026 comment) rather than shrunk, since timing out isn't necessarily evidence the queue needs less budget.