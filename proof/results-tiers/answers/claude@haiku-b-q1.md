**Function:** `__dd_dispatch_request` (**block/mq-deadline.c:325–414**)

**Priority/Direction Order:**

1. **Continue current batch direction** — if the previous batch's direction still has requests and `batching < fifo_batch` (lines 337–342)
2. **READ** — if available, attempt to dispatch reads first (lines 349–359)
   - *Unless* writes are starved (checked via `dd->writes_starved` counter; line 352–354), then switch to write
3. **WRITE** — if no reads or writes were starved (lines 365–374)

The scheduler enforces starvation prevention: if writes have been starved too long, it prioritizes them over reads despite reads being the default preference.