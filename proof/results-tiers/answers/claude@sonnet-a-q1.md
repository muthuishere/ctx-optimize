I now have the full picture — the top-level entry point and its per-priority helper.

## Answer

**Top-level dispatch entry point:** `dd_dispatch_request()` — `block/mq-deadline.c:452` (registered as `.dispatch_request` at `block/mq-deadline.c:994`)

**Actual selection logic:** `__dd_dispatch_request()` — `block/mq-deadline.c:325`

### Selection order in `dd_dispatch_request()` (block/mq-deadline.c:452-479)
1. **Manual/insert dispatch list first** — if `dd->dispatch` is non-empty, pop from there unconditionally (`block/mq-deadline.c:461-466`).
2. **Priority-aged requests** — `dd_dispatch_prio_aged_requests()` (`block/mq-deadline.c:420`), which promotes any non-`DD_RT_PRIO` request that's been waiting longer than `prio_aging_expire`, to prevent starvation of lower ioprio classes.
3. **Strict priority order** — loop `for (prio = 0; prio <= DD_PRIO_MAX; prio++)` calling `__dd_dispatch_request()` per priority (`DD_RT_PRIO` → `DD_BE_PRIO` → `DD_IDLE_PRIO`), stopping at the first priority level that either yields a request or still has requests queued (so lower priorities are never touched while a higher one has anything pending) — `block/mq-deadline.c:476-479`.

### Within `__dd_dispatch_request()` for a given priority (block/mq-deadline.c:325-414), the read/write direction order is:
1. **Continue current batch** — if still batching (`dd->batching < dd->fifo_batch`), keep dispatching in `dd->last_dir` via `deadline_next_request()` (line 337-342).
2. Otherwise pick a fresh direction:
   - **Reads preferred** — if the read FIFO is non-empty, use `DD_READ`, *unless* writes have been starved past `dd->writes_starved` (i.e. `dd->starved++ >= dd->writes_starved`), in which case jump to writes (lines 349-359).
   - **Writes** — used if there are no reads, or if writes were starved out reads (lines 365-374, label `dispatch_writes`).
3. Within the chosen direction: use the deadline-expired request (`deadline_fifo_request()`, earliest-expiry / FIFO order) if a deadline has expired or there's no next sorted request; otherwise continue in sector (elevator) order via `deadline_next_request()` (lines 378-396).

So the overall priority is: **dispatch-list bypass → priority-aged requests → RT → BE → IDLE priority classes**, and within each priority class: **batch continuation → reads (unless write-starved) → writes → (deadline-expired FIFO order vs. sector-sorted order)**.