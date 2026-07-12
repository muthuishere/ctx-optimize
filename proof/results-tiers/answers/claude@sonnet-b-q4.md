Confirmed by direct grep as well as the ctx-optimize graph. Within `block/`, `bio_split()` has exactly one call site:

- **`bio_submit_split_bioset()`** — `block/blk-merge.c:122`

That's the only caller of `bio_split()` inside `block/`. (Other callers exist outside `block/` — `btrfs_split_bio` in fs/btrfs/bio.c, `iomap_split_ioend` in fs/iomap/ioend.c, `xfs_buf_submit_bio` in fs/xfs/xfs_buf.c, `xlog_write_iclog` in fs/xfs/xfs_log.c, `xfs_zone_gc_split_write` in fs/xfs/xfs_zone_gc.c — but those are out of scope per your question.)