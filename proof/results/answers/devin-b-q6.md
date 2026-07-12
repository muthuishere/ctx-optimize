Based on the ctx-optimize store and source code:

**Budget timeout**: Set by `bfq_set_budget_timeout` (block/bfq-iosched.c:3285-3299) as `jiffies + bfqd->bfq_timeout * timeout_coeff`, where the coefficient is based on the queue's weight ratio.

**Function that checks it**: `bfq_bfqq_budget_timeout` (block/bfq-iosched.c:4460-4463) checks whether `time_is_before_eq_jiffies(bfqq->budget_timeout)`.

**What happens when it fires**: The in-service queue is expired. This occurs via `bfq_may_expire_for_budg_timeout` (block/bfq-iosched.c:4473-4485) called from `bfq_select_queue` (block/bfq-iosched.c:4818-4820). If the timeout has fired and the queue is not actively waiting for device idling (or has ≥1/3 budget remaining), `bfq_bfqq_expire` is invoked (block/bfq-iosched.c:5017) with reason `BFQQE_BUDGET_TIMEOUT`, which resets the in-service queue via `__bfq_bfqd_reset_in_service` and requeues it for future service.