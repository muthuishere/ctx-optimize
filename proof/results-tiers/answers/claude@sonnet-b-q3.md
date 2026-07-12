## How the block layer decides to split a bio

**Entry point:** `blk_mq_submit_bio()` (block/blk-mq.c) calls `__bio_split_to_limits()` on every incoming bio before it's turned into a request. The public wrapper `bio_split_to_limits()` (block/blk-merge.c:479-484) does the same for callers outside the core submit path (e.g. `drbd_submit_bio`, `nvme_ns_head_submit_bio`).

**Deciding check:** `bio_may_need_split()` (block/blk.h:397-414), called from inside `__bio_split_to_limits()` (block/blk.h:429-451). It's a cheap pre-check based on `bio->bi_io_vec[0]` against the queue's `queue_limits` — if the first segment alone can't obviously satisfy the limits, the bio *may* need splitting, so the real split path runs.

**Splitting functions** (dispatched from `__bio_split_to_limits()` by bio op, all in block/blk-merge.c):
- `bio_split_rw()` (L421) — normal read/write bios, computes split point via segment/sector-limit walk
- `bio_split_discard()` (L195, wraps `__bio_split_discard()` at L159-193) — discard bios
- `bio_split_write_zeroes()` (L449)
- `bio_split_zone_append()` (L437)

Each of these determines `split_sectors` against the `queue_limits`, then hands off to `bio_submit_split_bioset()` (block/blk-merge.c:119-139), which calls `bio_split()` (block/bio.c:1823-1856) to actually carve off the front portion of the bio (via `bio_alloc_clone` + `bio_integrity_trim`), chains it (`bio_chain`), and resubmits the remainder via `submit_bio_noacct_nocheck`.

So: **check** = `bio_may_need_split()` (block/blk.h:397), **split** = `bio_split()` (block/bio.c:1823), orchestrated per-op by `bio_split_rw`/`bio_split_discard`/`bio_split_write_zeroes`/`bio_split_zone_append` (block/blk-merge.c) under `__bio_split_to_limits()` (block/blk.h:429).