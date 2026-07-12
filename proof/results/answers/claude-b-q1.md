## Answer

**`dd_dispatch_request()`** — `block/mq-deadline.c:452-486` — is the elevator callback (`.dispatch_request`) that mq-deadline registers to pick the next request for the driver. It delegates the actual per-priority-band selection to `__dd_dispatch_request()` (`block/mq-deadline.c:325-414`).

**Selection order in `dd_dispatch_request()`:**
1. **Software `dd->dispatch` list first** (L461-466) — requests already queued for immediate dispatch (e.g. via `.insert_requests` with `at_head`) are returned before anything else.
2. **Aged requests across priority bands** via `dd_dispatch_prio_aged_requests()` (L468, defined L420-442) — if at least 2 of the 3 priority bands (`DD_RT_PRIO`, `DD_BE_PRIO`, `DD_IDLE_PRIO`) have queued requests, walk from `DD_BE_PRIO` up to `DD_PRIO_MAX` and dispatch any request older than `now - dd->prio_aging_expire`, to prevent low-priority starvation.
3. **Strict priority order** (L476-480) — loop `prio` from `0` (`DD_RT_PRIO`) to `DD_PRIO_MAX` (`DD_IDLE_PRIO`), calling `__dd_dispatch_request()` per band; stop at the first band that yields a request *or* still has requests queued (so lower priorities are never served while a higher one has pending work).

**Within `__dd_dispatch_request()` (per priority band), direction order:**
1. **Continue current batch** — if `dd->batching < dd->fifo_batch`, keep pulling from the current direction (`dd->last_dir`) via `deadline_next_request()` (L337-342).
2. Otherwise pick direction:
   - **Reads preferred**, unless writes have been starved past `dd->writes_starved` (L349-358, `dispatch_writes` goto at L352-354).
   - **Writes** otherwise (L365-374).
3. Within the chosen direction: prefer the **sector-sorted next request** (`deadline_next_request`), but fall back to the **oldest (FIFO/deadline-expired) request** via `deadline_fifo_request()` if a deadline has expired, direction just switched, or no next-sorted request exists (L378-396).