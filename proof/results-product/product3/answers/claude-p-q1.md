## Answer

The dispatch decision is made by two cooperating functions in `block/mq-deadline.c`:

**`dd_dispatch_request(hctx)`** — `block/mq-deadline.c:452-486` — the entry point registered as the scheduler's `.dispatch_request` callback. Its top-level selection order:
1. Serve anything already sitting on `dd->dispatch` (requests explicitly inserted at front/back), if non-empty.
2. Else call `dd_dispatch_prio_aged_requests()` (`block/mq-deadline.c:420-442`) — dispatch any request that has aged past the priority-aging threshold, regardless of I/O priority class.
3. Else walk priority classes in order `prio = 0 .. DD_PRIO_MAX` (i.e. `DD_RT_PRIO` → `DD_BE_PRIO` → `DD_IDLE_PRIO`), calling `__dd_dispatch_request(dd, &dd->per_prio[prio], now)` for each and stopping at the first priority level that either yields a request or still has requests queued (so lower priorities are never touched while a higher one has work).

**`__dd_dispatch_request(dd, per_prio, latest_start)`** — `block/mq-deadline.c:325-414` — picks read vs. write within one priority level:
1. If mid-batch (`dd->batching < dd->fifo_batch`) and `deadline_next_request()` returns a request in `dd->last_dir`, continue that direction (batching).
2. Otherwise pick direction: prefer `DD_READ` if the read FIFO is non-empty — *unless* writes have been starved (`dd->starved++ >= dd->writes_starved`), in which case jump to `dispatch_writes`.
3. If no reads, fall through to `DD_WRITE` (`dispatch_writes:` label, resets `dd->starved = 0`).
4. Within the chosen direction, prefer the sector-ordered `next_rq` (`deadline_next_request`) unless a deadline has expired or direction changed (`deadline_check_fifo`), in which case fall back to the oldest-deadline request via `deadline_fifo_request`.

So overall priority is: **front-inserted dispatch list → aged (priority-boosted) requests → RT → BE → IDLE priority class**, and within each class: **read-biased with write-starvation override, batched by sector order with deadline-expiry fallback**.