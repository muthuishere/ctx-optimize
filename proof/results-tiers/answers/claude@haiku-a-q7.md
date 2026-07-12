**Partition table detection functions:**

1. **`check_partition()`** — `block/partitions/core.c:119`
   - Tries each partition parser in the `check_part[]` array (lines 141–143: `while (!res && check_part[i]) { ... res = check_part[i++](state);`)

2. **`add_partition()`** — `block/partitions/core.c:295`
   - Adds discovered partitions to the disk (allocates block device, sets start sector/length, registers with the kernel)

The partition parsers themselves are configured in the `check_part[]` array at `block/partitions/core.c:17`, which includes functions like `adfspart_check_*`, `cmdline_partition`, `efi_partition`, `msdos_partition`, etc.—one per supported partition table format.