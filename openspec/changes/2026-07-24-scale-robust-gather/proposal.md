# ADR — scale-robust gather: one bad node must not discard the index

Status: DRAFT — 2026-07-24. HIGH priority (owner: "this is a bigger quality").
Spec-first; code after sign-off. Motivated by a MEASURED Linux-scale failure.

## The measured failure (repro)

`ctx-optimize add <linux>` on the full kernel (84,402 files / 58,632 C+H):

```
code: 2,627,150 nodes, 4,868,535 edges   ← extracted in 18.9s (perf is excellent)
ctx-optimize: reject batch: node Documentation/admin-guide/aoe/udev.txt::: label is required
store size: 0B                            ← NOTHING persisted
```

Extraction is blazing (2.6M nodes/18.9s). But **one** doc node with an empty
label made `Batch.Validate()` (schema.go:64) fail, and `store.Merge`/`Replace`
(store.go:201/282) returned `reject batch` — **discarding all 2.6M valid nodes.**
ctx-optimize currently **cannot index Linux at all.**

## Two bugs

- **B1 (producer)** — the tier-1 doc/markdown producer emitted a node for
  `Documentation/.../udev.txt` with an **empty label** (id ends `:::` — an empty
  heading slug). A producer must never emit an invalid node.
- **B2 (commit granularity — the real one)** — `Batch.Validate()` fails on the
  FIRST bad node and the store rejects the WHOLE batch. At scale this is
  fragility: P(one weird node) → 1 as repos grow, so the biggest, most valuable
  repos are the most likely to produce *nothing*. **The bigger the repo, the more
  likely total failure** — the exact opposite of what we want.

## Respect the existing rationale (don't just weaken it)

`Validate` fails closed on purpose: *"a partially accepted batch would make
provenance and dedup lie."* Correct — a batch half-applied, with edges pointing
at nodes that never landed, corrupts the graph. The fix must keep that integrity,
not trade it away.

## Fix — partition, don't all-or-nothing (keep integrity, drop the fragility)

Change commit from "reject whole batch on first invalid node" to **coherent
partition**:

1. **Validate collects ALL failures** (not first-fail) and splits the batch into
   `accepted` (fully-valid, deduped nodes) and `quarantined` (invalid nodes +
   the reason each failed).
2. **Cascade to edges**: any edge whose source/target is a quarantined (or
   otherwise-absent) node is also quarantined — so the committed graph has NO
   dangling edges. This is what preserves the provenance/dedup honesty the
   original rationale protects: the store still contains only coherent,
   fully-valid, non-dangling data.
3. **Commit the `accepted` remainder**; write a **loud, structured reject report**
   — counts + first N samples (id + reason) — to stderr and to a
   `<store>/quarantine.ndjson` (git-diffable, so it's auditable, not silent).
4. **Exit policy**: default = succeed with a warning (index the 2.6M, report the
   1); `--strict` restores today's fail-closed-whole for CI that wants zero
   tolerance. (OPEN: default warn vs default nonzero-exit-but-still-commit.)
5. **B1 in parallel**: fix the doc producer so it never emits an empty-label node
   (derive label from filename/heading, or skip the unit). B1 removes *this*
   trigger; B2 makes the system robust to the *next* one — both needed, B2 is the
   structural fix.

## Determinism (non-negotiable — it's our whole brand)

- Partition is deterministic: same input → identical `accepted` set, identical
  `quarantine.ndjson` (sorted, stable), identical store bytes. The quarantine
  report is a plain sorted file like every other artifact.
- Golden net gains a case: a batch containing one invalid node commits the valid
  remainder byte-for-byte AND emits the expected quarantine report; `--strict`
  still rejects whole. Add a Linux-tier smoke: `add <linux>` must now produce a
  non-empty store (with a small, reported quarantine set), not 0 B.

## Success check

- `ctx-optimize add <full-linux>` produces a real store (~2.6M nodes) with a
  small, reported quarantine set — not 0 B. This is the headline: **we index
  Linux.**
- No dangling edges in any committed store (edge cascade holds).
- `--strict` reproduces today's whole-batch rejection for zero-tolerance CI.
- Quarantine is auditable (`quarantine.ndjson`) and deterministic; golden pins it.
- B1: the doc producer emits no empty-label nodes; the udev.txt unit is either
  labeled or skipped, with a test.

## Open questions

1. Default exit code on a non-empty quarantine: 0-with-warning (lean, so agents/
   hooks keep working) or nonzero-but-committed (louder for CI)? `--strict` covers
   the zero-tolerance case either way.
2. Quarantine cap: if >X% of nodes fail, is that a real corruption signal that
   SHOULD abort (a bad extractor, not a stray doc)? Lean: abort if quarantine >
   e.g. 5% of the batch — "one bad apple" is fine, "the extractor is broken" is
   not.
3. Should B1's doc producer emit a labelled node (filename as label) or skip
   labelless doc units entirely? Lean: label from the filename so the file is
   still findable.
