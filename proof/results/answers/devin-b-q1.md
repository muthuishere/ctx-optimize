**Function:** `dd_dispatch_request` (block/mq-deadline.c:452-486)

**Selection order:**

1. **Priority level order** (lines 476-480): Iterates through priority classes from 0 to DD_PRIO_MAX:
   - `DD_RT_PRIO` (0) - Real-time
   - `DD_BE_PRIO` (1) - Best-effort  
   - `DD_IDLE_PRIO` (2) - Idle

2. **Within each priority level** (in `__dd_dispatch_request` at lines 325-414):
   - **Reads first** (lines 349-358): Checks for pending reads before writes
   - **Writes with starvation prevention** (lines 352-354): Switches to writes if writes are starved (`dd->starved >= dd->writes_starved`)
   - **Batching continuation** (lines 337-342): Continues batching in the same direction if `dd->batching < dd->fifo_batch`

The actual request selection within a direction uses either `deadline_next_request` (sorted by sector) or `deadline_fifo_request` (earliest deadline) depending on whether a deadline has expired (lines 383-396).