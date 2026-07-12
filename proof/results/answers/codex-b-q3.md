For incoming read/write bios, `blk_mq_submit_bio()` runs the bio through `__bio_split_to_limits()` before request submission. That helper decides whether splitting is even needed by calling `bio_may_need_split()`.

- Deciding check: `bio_may_need_split(struct bio *bio, const struct queue_limits *lim)` in `block/blk.h:397`
- Split dispatcher: `__bio_split_to_limits(...)` in `block/blk.h:429`, called by `blk_mq_submit_bio()` in `block/blk-mq.c:3093`
- Read/write split function: `bio_split_rw(struct bio *bio, const struct queue_limits *lim, unsigned *nr_segs)` in `block/blk-merge.c:421`

For normal read/write bios, `__bio_split_to_limits()` dispatches to `bio_split_rw()`, which then calls `bio_submit_split()` after computing the allowed I/O size.