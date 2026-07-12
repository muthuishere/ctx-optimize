`blk_start_plug()` sets up a per-task `struct blk_plug` on `current->plug`; the generic block plug buffers **requests**, not raw bios. It initializes `plug->mq_list`, `plug->cached_rqs`, `plug->cb_list`, counters, and flags in `blk_start_plug_nr_ios()` (`block/blk-core.c:1165`). New bios become `struct request`s in `blk_mq_submit_bio()`, then `blk_add_rq_to_plug()` appends those requests to `plug->mq_list` (`block/blk-mq.c:3197`, `block/blk-mq.c:1377`). It also buffers plug callbacks via `blk_check_plugged()` / `cb_list` (`block/blk-core.c:1237`) and cached preallocated requests in `cached_rqs` (`block/blk-mq.c:626`, freed by `blk_mq_free_plug_rqs()` at `block/blk-mq.c:834`).

Early flushes before `blk_finish_plug()` happen when:

- The plug gets too large: `blk_add_rq_to_plug()` calls `blk_mq_flush_plug_list(plug, false)` when `rq_count >= blk_plug_max_rq_count()` (`block/blk-mq.c:1383`).
- The last queued request is too large for plugging: same call path when merges are allowed and `blk_rq_bytes(last) >= BLK_PLUG_FLUSH_SIZE` (`block/blk-mq.c:1384`).
- The task blocks/schedules: documented in `blk_start_plug()` comments (`block/blk-core.c:1202`, `block/blk-core.c:1205`). The actual scheduler caller is outside this reduced checkout, but the flush implementation it reaches is `__blk_flush_plug()` (`block/blk-core.c:1262`), which runs callbacks, dispatches `mq_list` via `blk_mq_flush_plug_list()` (`block/blk-mq.c:2938`), and frees cached requests.
- `bio_poll()` explicitly flushes `current->plug` before polling completions via `blk_flush_plug(current->plug, false)` (`block/blk-core.c:993`).

Normal finish is `blk_finish_plug()` -> `__blk_flush_plug(plug, false)` -> `blk_mq_flush_plug_list()` (`block/blk-core.c:1290`, `block/blk-core.c:1262`, `block/blk-mq.c:2938`).