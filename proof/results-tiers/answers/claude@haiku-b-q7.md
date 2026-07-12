**Partition table detection when a disk is scanned:**

1. **Function that tries each partition parser**: `check_partition()` · **block/partitions/core.c:119**
   - Loops through `check_part[]` array (a static array of partition parser function pointers, lines 17–91)
   - Calls each parser in sequence: `res = check_part[i++](state)` (line 143)

2. **Function that adds discovered partitions**: `blk_add_partition()` · **block/partitions/core.c:531**
   - Called once per partition found by the parsers
   - Invoked from `blk_add_partitions()` in a loop (line 635): `if (!blk_add_partition(disk, state, p))`

The flow: `check_partition()` probes each enabled parser (EFI, MSDOS, LDM, etc.) until one successfully parses the partition table, then `blk_add_partition()` is called for each partition slot to register it with the block layer.