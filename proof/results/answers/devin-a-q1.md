**Function:** `dd_dispatch_request` <ref_file file="/private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-nexus-workspace-brain/dc4417dd-0040-4de7-82a4-10f10843dc78/scratchpad/proof/linux/block/mq-deadline.c" lines="452-486" />

**Selection order:**
1. Pre-filled dispatch queue (`dd->dispatch`)
2. Priority-aged requests (non-RT priorities aged past `prio_aging_expire`)
3. Priority loop: `DD_RT_PRIO` → `DD_BE_PRIO` → `DD_IDLE_PRIO` <ref_file file="/private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-nexus-workspace-brain/dc4417dd-0040-4de7-82a4-10f10843dc78/scratchpad/proof/linux/block/mq-deadline.c" lines="476-480" />
4. Within each priority (via `__dd_dispatch_request`): reads preferred unless writes starved (≥ `writes_starved`), then writes <ref_file file="/private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-nexus-workspace-brain/dc4417dd-0040-4de7-82a4-10f10843dc78/scratchpad/proof/linux/block/mq-deadline.c" lines="349-374" />