**Refuses to split:**
1. **Zone append commands** — `bio_op(bio) == REQ_OP_ZONE_APPEND` (line 1834)
2. **Atomic writes** — `bio->bi_opf & REQ_ATOMIC` (line 1838)
3. **Invalid sector counts** — sectors ≤ 0 or ≥ bio's total sectors (lines 1828–1831)

**What it does to the original bio on success:**
- **Advances the original bio** via `bio_advance(bio, split->bi_iter.bi_size)` (line 1850), which updates the bio's iterator to skip the sectors that were split off. The original bio now represents the remaining sectors after the split.

In brief: it carves out a new bio representing the first N sectors (pointing to the same `bi_io_vec`), and modifies the original bio to represent only what remains.

File: **block/bio.c:1823–1856**