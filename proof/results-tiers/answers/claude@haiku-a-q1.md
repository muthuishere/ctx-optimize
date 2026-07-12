The dispatch function is **`dd_dispatch_request()`** at **block/mq-deadline.c:452**.

The function delegates to **`__dd_dispatch_request()`** (line 325) for the core selection logic within each priority level.

**Selection order:**

1. **Dispatch queue** (pre-queued high-priority inserts, line 461)
2. **Priority aged requests** (lower priorities that have aged, line 468)
3. **Priority levels** (line 476): `DD_RT_PRIO` (0) → `DD_BE_PRIO` (1) → `DD_IDLE_PRIO` (2)

Within each priority level (**`__dd_dispatch_request()`**):
4. **Direction within current batch** (line 337): continue if `batching < fifo_batch`
5. **READ direction** (line 349): unless writes have starved (`starved >= writes_starved`), then **WRITE** (line 366)

Within each direction:
6. **Sector-sorted order** (elevator algorithm, line 382: `deadline_next_request()`)
7. **FIFO order** if deadline expired or no higher-sector requests (line 389: `deadline_fifo_request()`)