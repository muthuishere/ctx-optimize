The mq-deadline elevator callback is `dd_dispatch_request()` in `block/mq-deadline.c:452`; it is registered as `.dispatch_request = dd_dispatch_request` at `block/mq-deadline.c:989`.

Selection order:

1. Existing scheduler dispatch list first: `dd->dispatch` head, if non-empty (`block/mq-deadline.c:461`).
2. Priority-aged lower-priority requests next via `dd_dispatch_prio_aged_requests()` (`block/mq-deadline.c:468`). That checks `DD_BE_PRIO` then `DD_IDLE_PRIO` (`block/mq-deadline.c:434`).
3. Normal priority order: `DD_RT_PRIO`, `DD_BE_PRIO`, `DD_IDLE_PRIO`, because enum values are `0, 1, 2` (`block/mq-deadline.c:48`) and the loop runs `prio = 0; prio <= DD_PRIO_MAX` (`block/mq-deadline.c:476`).

Within each priority, the actual request chooser is `__dd_dispatch_request()` in `block/mq-deadline.c:325`.

Direction order inside `__dd_dispatch_request()`:

1. Continue current read/write batch in `dd->last_dir` if there is a next request and `batching < fifo_batch` (`block/mq-deadline.c:334`).
2. Otherwise prefer reads if any reads exist (`block/mq-deadline.c:349`).
3. But dispatch writes first if writes exist and writes have been starved enough: `dd->starved++ >= dd->writes_starved` (`block/mq-deadline.c:352`).
4. If no reads, dispatch writes (`block/mq-deadline.c:365`).
5. For the chosen direction, pick expired FIFO or FIFO if no next sorted request; otherwise continue sector-sorted next request (`block/mq-deadline.c:382`).