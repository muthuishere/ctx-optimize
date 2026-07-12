Confirmed. Here's the answer:

**Where scanning happens:** `check_partition()` in `block/partitions/core.c:119` iterates the `check_part[]` array of partition-type parsers (`block/partitions/core.c:17`, e.g. `msdos_partition`, `efi_partition`, `mac_partition`, `sgi_partition`, etc.), calling each one in turn at `block/partitions/core.c:141-143`:
```c
while (!res && check_part[i]) {
    ...
    res = check_part[i++](state);
```
until one parser reports success (`res > 0`).

**Caller / where discovered partitions get added:** `blk_add_partitions()` in `block/partitions/core.c:583` calls `check_partition()` (`core.c:591`), then loops over `state->parts` calling `blk_add_partition()` (`core.c:635`), which in turn calls `add_partition()` (`core.c:295`, invoked at `core.c:566`) to actually create/register each `struct block_device` partition on the disk.