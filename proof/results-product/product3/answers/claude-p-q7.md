This gives the full picture. Here's the flow:

**Entry point:** `disk_scan_partitions()` — `block/genhd.c:363-403` — triggers the scan by opening the device, which drives partition detection (called from `add_disk_final` and `blkdev_common_ioctl`).

**Function that adds the discovered partitions:** `blk_add_partitions()` — `block/partitions/core.c:583-642` — calls `check_partition()` to get the parsed table, then calls `blk_add_partition()` for each entry to actually instantiate it (called from `bdev_disk_changed`).

**Function that tries each partition parser:** `check_partition()` — `block/partitions/core.c:119-176` — iterates the `check_part[]` array of parser functions (msdos, gpt/efi, ldm, etc.), calling `check_part[i++](state)` for each until one succeeds.

**Underlying function that instantiates a partition's `block_device`:** `add_partition()` — `block/partitions/core.c:295-406` — called via `blk_add_partition()`/`bdev_add_partition()` to allocate and register the block device for a discovered partition.