Confirmed. Here's the flow, called from `bdev_disk_changed()`:

- **`blk_add_partitions()`** — `block/partitions/core.c:583-642`. Calls `check_partition()` (line 591) to run the detection, then loops `for (p = 1; p < state->limit; p++)` calling **`blk_add_partition(disk, state, p)`** (line 634-636) to actually register each discovered partition with the block layer.

- **`check_partition()`** — `block/partitions/core.c:119-176`. This is the function that "tries each partition parser": it loops `while (!res && check_part[i])` calling `check_part[i++](state)` (lines 141-143), where `check_part[]` is the array of per-format parser functions (msdos, gpt, mac, amiga, acorn, etc.) — the first one that recognizes the table wins.

So: parser dispatch = `check_partition()` in `block/partitions/core.c`; partition registration = `blk_add_partition()` (invoked from `blk_add_partitions()`) in the same file.