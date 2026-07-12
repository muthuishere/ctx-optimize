I have enough to answer.

## Answer

**Top-level dispatch entry point:** `dd_dispatch_request()` — `block/mq-deadline.c:452`
This is the elevator's `dispatch_request` callback (registered at `block/mq-deadline.c:994`), invoked from `blk_mq_run_hw_queue()` → `__blk_mq_sched_dispatch_requests()`.

**Actual selection logic:** `__dd_dispatch_request()` — `block/mq-deadline.c:325`
(called per priority level, plus by `dd_dispatch_prio_aged_requests()` at `block/mq-deadline.c:420` for anti-starvation of aged lower-priority requests)

### Selection order in `dd_dispatch_request()` (`block/mq-deadline.c:452-480`)

1. **Manual/insertion dispatch list** (`dd->dispatch`, line 461) — anything explicitly queued via `dd_dispatch_request`'s front-insert path is returned first, no scheduling logic applied.
2. **Priority-aged requests** via `dd_dispatch_prio_aged_requests()` (line 468) — if requests of more than one priority class are queued, walks `DD_BE_PRIO` → `DD_PRIO_MAX` (i.e. `DD_BE_PRIO`, `DD_IDLE_PRIO`) looking for requests older than `prio_aging_expire`, to stop lower-priority I/O from starving forever under continuous higher-priority load. (`DD_RT_PRIO` is skipped here — real-time requests are never "aged out" of priority since it's already the top tier.)
3. **Strict priority order**: loop `prio = 0` (`DD_RT_PRIO`) → `DD_PRIO_MAX` (`DD_IDLE_PRIO`) at line 476, calling `__dd_dispatch_request()` per priority bucket and stopping at the first priority level that either returns a request or still has requests queued (`dd_queued(dd, prio)`) — i.e. it never dispatches a lower-priority request while a higher-priority one is pending.

### Direction (read/write) order inside `__dd_dispatch_request()` (`block/mq-deadline.c:325-414`), for a given priority bucket:

1. **Continue current batch**: if `dd->batching < dd->fifo_batch`, keep dispatching in `dd->last_dir` via `deadline_next_request()` (sector-order within the batch).
2. Otherwise pick a fresh direction:
   - **Reads preferred by default** — if the read FIFO is non-empty, dispatch `DD_READ`, *unless* a write is FIFO-expired **and** the write-starvation counter has reached `dd->writes_starved` (`dd->starved++ >= dd->writes_starved`, line 353), in which case it jumps to `dispatch_writes`.
   - **Writes** are dispatched (`dispatch_writes:` label, line 366) if there are no reads pending, or the starvation condition above triggered.
3. Within the chosen direction, prefer the **sector-order next request** (`deadline_next_request()`) unless a deadline has expired or there's no next request in sort order, in which case fall back to the **oldest-by-deadline FIFO request** (`deadline_fifo_request()`).

So: dispatch list → aged-priority rescue → strict priority (RT > BE > IDLE) → batch continuation → read-preferred/write-starvation-avoidance → sector order with deadline-expiry fallback.