Based on my search, there is **only one call site** of `bio_split()` within the `block/` directory that would need to be updated:

| Function | File:Line |
|----------|-----------|
| `bio_submit_split_bioset()` | `block/blk-merge.c:122` |