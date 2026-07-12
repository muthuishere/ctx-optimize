I have the exact code. Here's the answer:

## Top-level entry: `dd_dispatch_request()` — `block/mq-deadline.c:452-486`

Called from the block-mq scheduler per hardware queue. Its selection order:

1. **Pending `dd->dispatch` list** (requests already forced/inserted directly) — dispatched first if non-empty (line 461-466).
2. **`dd_dispatch_prio_aged_requests()`** (`block/mq-deadline.c:420-442`) — if requests of more than one I/O priority class are queued, walk priorities `DD_BE_PRIO` → `DD_PRIO_MAX`, and dispatch a request that has been waiting longer than `prio_aging_expire` (anti-starvation for lower-priority I/O).
3. Otherwise, walk priority classes in order `prio = 0` (`DD_RT_PRIO`) up to `DD_PRIO_MAX`, calling **`__dd_dispatch_request()`** for each; stop at the first priority that yields a request or still has requests queued (higher priority always wins/blocks lower priority).

## Actual per-priority pick: `__dd_dispatch_request()` — `block/mq-deadline.c:325-414`

Given a priority's `dd_per_prio`, the read/write direction order it tries:

1. **Continue current batch**: if there's a `next_rq` in `dd->last_dir` and `dd->batching < dd->fifo_batch`, keep dispatching in the same direction (line 337-342).
2. Else pick a new direction:
   - If the **READ** fifo is non-empty → prefer **READ**, *unless* writes have been starved (`dd->starved++ >= dd->writes_starved`), in which case switch to **WRITE** (lines 349-359).
   - Else if the **WRITE** fifo is non-empty → **WRITE** (lines 365-374).
   - Else → no request (`NULL`).
3. Within the chosen direction: if a deadline has expired (`deadline_check_fifo()`) or there's no next sorted request, pick the **oldest-deadline (FIFO) request** (`deadline_fifo_request()`); otherwise continue in **sector-sorted order** (`next_rq` from `deadline_next_request()`) — lines 382-396.
4. Final request must satisfy `!started_after(dd, rq, latest_start)` before being returned (used to bound aging dispatch).

So the priority order is: **direct-dispatch list → aged/starved lower-priority requests → RT → BE → IDLE priority classes**, and within each priority: **batch continuation → READ (unless write-starved) → WRITE → within direction: expired-deadline FIFO order, else sector-sorted order**.