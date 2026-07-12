`bio_split()` refuses to split these bios (returns `ERR_PTR(-EINVAL)`):

1. **Invalid sector count**: sectors ≤ 0 (line 1828-1829)
2. **Over-split request**: sectors ≥ bio_sectors(bio) (line 1830-1831)  
3. **Zone append commands**: `REQ_OP_ZONE_APPEND` (line 1834-1835)
4. **Atomic writes**: `REQ_ATOMIC` flag set (line 1838-1839)

When it succeeds, `bio_split()`:

1. Clones the bio with `bio_alloc_clone()` (line 1841)
2. Sets the split bio's size to the requested sectors (line 1845)  
3. Trims integrity data if present (line 1847-1848)
4. **Advances the original bio** by the split size using `bio_advance()` (line 1850) — this moves the original bio's iterator forward so it represents the remaining sectors
5. Copies the `BIO_TRACE_COMPLETION` flag if set (line 1852-1853)
6. Returns the split bio

<ref_snippet file="/private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-nexus-workspace-brain/dc4417dd-0040-4de7-82a4-10f10843dc78/scratchpad/proof/linux/block/bio.c" lines="1823-1856" />