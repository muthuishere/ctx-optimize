`blk_start_plug()` buffers per-task block-layer work in `current->plug`: mainly built `struct request`s on `plug->mq_list`, cached unused requests on `plug->cached_rqs`, and plug callbacks on `plug->cb_list` (`blk_start_plug_nr_ios`, `block/blk-core.c:1165`; fields initialized at `block/blk-core.c:1175`).

Requests enter the plug from:

- `blk_mq_submit_bio()` after a bio is converted to a request, via `blk_add_rq_to_plug()` (`block/blk-mq.c:3093`, `block/blk-mq.c:3197`).
- `blk_execute_rq_nowait()` for non-head insertion, via `blk_add_rq_to_plug()` (`block/blk-mq.c:1415`, `block/blk-mq.c:1424`).

Flush implementation:

- `blk_finish_plug()` calls `__blk_flush_plug()` (`block/blk-core.c:1290`).
- `__blk_flush_plug()` runs callbacks, flushes `mq_list` through `blk_mq_flush_plug_list()`, and frees `cached_rqs` (`block/blk-core.c:1262`).
- `blk_mq_flush_plug_list()` dispatches the buffered request list (`block/blk-mq.c:2938`).

Early flush before `blk_finish_plug()` happens when:

- Plugged request count reaches `blk_plug_max_rq_count()` (`BLK_MAX_REQUEST_COUNT`, doubled for multiple queues): `blk_add_rq_to_plug()` calls `blk_mq_flush_plug_list()` (`block/blk-mq.c:1370`, `block/blk-mq.c:1383`).
- The last plugged request is at least `BLK_PLUG_FLUSH_SIZE` and merges are enabled: same call path (`block/blk-mq.c:1384`).
- The task schedules/blocks; documented in `blk_start_plug()` comments, with `from_schedule` handled by the flush path (`block/blk-core.c:1202`, `block/blk-mq.c:2942`).
- `bio_poll()` explicitly flushes the current plug before polling (`block/blk-core.c:993`).