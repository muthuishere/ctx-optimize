Only one direct caller inside `block/` needs updating:

- `bio_submit_split_bioset()` in `block/blk-merge.c` at line 122: calls `bio_split(bio, split_sectors, GFP_NOIO, bs)`

Store source: `bio_split` is `block/bio.c::bio_split` at `block/bio.c:1823`, and its only `called_by` edge inside `block/` is `block/blk-merge.c::bio_submit_split_bioset` at `block/blk-merge.c:119`. A narrow source lookup confirmed the exact call line at `block/blk-merge.c:122`.