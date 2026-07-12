Partition-table detection is in [block/partitions/core.c](/private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-nexus-workspace-brain/dc4417dd-0040-4de7-82a4-10f10843dc78/scratchpad/proof/linux/block/partitions/core.c:119):

- `check_partition()` tries each registered partition parser from `check_part[]`; the loop is at `core.c:140-143`.
- `blk_add_partitions()` adds the discovered partitions after detection; it calls `check_partition()` at `core.c:591`, then iterates discovered entries at `core.c:634-635`.
- Per-partition creation is delegated to `blk_add_partition()` at `core.c:531`, which calls `add_partition()` at `core.c:566`.

The disk scan entry point is `disk_scan_partitions()` in [block/genhd.c](/private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-nexus-workspace-brain/dc4417dd-0040-4de7-82a4-10f10843dc78/scratchpad/proof/linux/block/genhd.c:363).