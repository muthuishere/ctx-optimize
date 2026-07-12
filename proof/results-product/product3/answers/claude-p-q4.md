Callers of `bio_split()` within `block/`:

- `bio_submit_split_bioset()` — block/blk-merge.c

The other callers (`btrfs_split_bio`, `iomap_split_ioend`, `xfs_buf_submit_bio`, `xlog_write_iclog`, `xfs_zone_gc_split_write`) are outside `block/` (in fs/btrfs and fs/xfs) and would also need updating if you change the signature, but per your scope restricted to `block/`, only **`bio_submit_split_bioset` in `block/blk-merge.c`** is in-scope.