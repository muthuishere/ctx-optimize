Partition-table detection during a disk scan happens under the scan path:

`disk_scan_partitions()` in `block/genhd.c:363` opens/claims the disk, which leads into partition rescan handling.

The parser loop is:

`check_partition(struct gendisk *hd)` in `block/partitions/core.c:119`  
This is the function that tries the registered partition parsers and returns a `struct parsed_partitions`.

The discovered partitions are materialized by:

`blk_add_partitions(struct gendisk *disk)` in `block/partitions/core.c:583`  
It calls `check_partition()` and then calls `blk_add_partition()` for each discovered entry.

Per-partition add helper:

`blk_add_partition(struct gendisk *disk, struct parsed_partitions *state, int p)` in `block/partitions/core.c:531`  
This wraps the actual `add_partition()` call.