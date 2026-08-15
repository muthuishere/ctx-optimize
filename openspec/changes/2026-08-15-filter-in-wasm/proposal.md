# ADR 9 — filter inside the wasm, don't match inside it

Status: DRAFT — owner review pending 2026-08-15. No product code until agreed.
Owner question: *"cant we add the boundary inside the wasm itself instead of
doing seperately"*.
Scope: `internal/grammar/assets/shim.c` (+ a `treesitter.wasm` rebuild) and the
decode loop in `internal/extract/code/wasm.go`. No rule-schema change, no
producer change, no emitted-fact change.

## Short answer to the question as asked: no — and it would not pay

Moving **boundary rule matching** into the wasm is the wrong move on three
counts:

1. **There is almost nothing left to win.** After ADR 7 the boundary lane's own
   cost, measured by interleaving it on and off on the same binary, is
   ts-typescript **+0.6%**, java-spring **+1.3%**, go-kubernetes **within
   noise**. Rule matching is an O(1) map probe on a node the walk already
   visits. Optimising ~1% by rewriting it in C is a bad trade at any price.
2. **It would break the property the owner asked for in the first place.**
   Rules are *data* — users drop them into `.ctxoptimize/boundaries.json` and
   the ladder merges them. A C matcher means the rule engine lives inside a
   committed 19 MB binary artifact; extending it stops being "edit a JSON file"
   and becomes "rebuild the wasm with zig".
3. **It would strand grammar packs.** A user-built pack carries its own wasm; a
   shim that matched rules would leave every pack behind at its old shim
   version.

## But the instinct is right, and it points at something bigger

The question behind the question — *do more inside the wasm, move less across
the boundary* — is correct, and there is a large measured win available. It is
just not about boundaries.

**`co_parse` emits every node in the tree, named and anonymous.** Read
`shim.c:69` — the cursor walks the whole tree and calls `put_u32` six times per
node, unconditionally. Anonymous nodes are the punctuation and operator tokens:
every `;`, `{`, `}`, `(`, `,`, `=`, `+`.

**Every consumer throws them away.** Every single use of the `Named` flag in
`internal/extract/code` is a skip — `if !n.Named { continue }` at `code.go:936`
and `:883`, `raw[j].Named && …` in `routes.go:117`, `routepacks.go:199`,
`boundarysites.go:162`, `frontend_routes.go:113`. Nothing reads an anonymous
node's bytes, type, or range.

**Measured on go-kubernetes:**

| | records |
|---|---|
| total emitted | **24,056,337** |
| named (actually used) | 15,105,411 |
| **anonymous (copied, decoded, discarded)** | **8,950,926 — 37.2%** |

At 24 bytes per record that is **≈215 MB** copied out of wasm linear memory,
decoded into Go structs, and dropped — per kubernetes gather.

This lines up with the pprof profile in ADR 8, which found 15.6% of CPU in
`memclrNoHeapPointers` (23% of it wazero `MemoryInstance.Grow`) and 5.4% in
`memmove` — i.e. roughly a fifth of the time is moving and zeroing bytes whose
volume this change cuts by more than a third.

## D1 — `co_parse` emits named nodes only

One condition in the emit loop: keep walking the full tree (the cursor must, to
maintain depth), but only `put_u32` when `ts_node_is_named(node)`.

Depth arithmetic is unaffected because **every record already carries its own
absolute depth** — consumers navigate by `raw[j].Depth == d+1`, never by array
adjacency, and they already skip anonymous entries. Dropping them shortens the
scan without moving any named node.

## D2 — this needs a compatibility decision, and that is the real cost

`treesitter.wasm` is a **committed ~19 MB artifact** (`go:embed`), and grammar
packs are separate user-built `.wasm` files discovered at add time. A shim
change means:

- the embedded bundle is rebuilt (dev-only script, fine); **but**
- an existing user-built **pack** still contains the OLD shim and will keep
  emitting anonymous nodes.

So the decode side must tolerate both — which it already does, since it filters
on `Named` anyway. **That makes this change backward-compatible by
construction**: new shim emits less, old packs emit more, both decode
correctly. Say it explicitly in the ADR so nobody "optimises" the decoder into
assuming the new shape.

## Spike before implementing — three questions

1. **Real saving.** Rebuild the bundle with the filter and measure gather
   wall-time and allocations on kubernetes, ts-typescript, java-spring. The
   record count must fall ~37%; the *time* saving will be smaller (parsing is
   58% and unaffected) and must be measured, not projected.
2. **Correctness.** Output must be **byte-identical** on every corpus. If a
   single fact moves, some consumer depended on anonymous nodes and the audit
   above was wrong.
3. **Is there more?** Named nodes still include a lot nobody queries
   (expression wrappers, block nodes). A symbol-class allowlist passed *into*
   the shim would cut further — but that couples the shim to consumer needs and
   breaks the "one parse, many consumers" property. Measure the ceiling before
   deciding whether to go further than D1.

## Gates

- Byte-identical output on all corpora (the whole point).
- `task ci`, hermetic + corpus + judged golden; scores may not move.
- The perf baseline (`internal/golden/testdata/perf-baseline.json`) should
  ratchet DOWN; it is a reviewed diff if it does not.
- `scripts/wasm/build.sh` remains dev-only — no user ever needs zig for this.

## Kill criterion

If the measured wall-time saving is under 5% on kubernetes, do not ship it: a
wasm rebuild has a real maintenance and pack-compatibility cost, and a 215 MB
memcpy that costs 2% is not worth spending that on. Record the number either
way so the question is closed.
