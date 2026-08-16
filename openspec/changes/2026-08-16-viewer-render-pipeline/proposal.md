# ADR 20 — The viewer render pipeline: worker, WASM, and what actually scales

Status: ACCEPTED 2026-08-16 (owner). Phase 0 IMPLEMENTED; Phases 1-3 deferred
pending the numbers below.
Date: 2026-08-16

> **Read the post-spike section first.** The browser spike overturned the
> culprit named in the draft below, and then the scaling run overturned the
> *phasing*. The draft is kept intact because the wrong turns are the argument:
> three separate "obvious" bottlenecks measured innocent before the real one
> appeared, and it only appeared above 5,000 nodes — the size at which we were
> testing.

## The ask

> "canvas important, bg worker wasm important for performance"
> "we have billion nodes and performance should not be degraded, use whatever
> best kind of like used in gaming"

Canvas stays. The question this ADR answers is **where the time actually goes**,
because the first three things everyone blames turned out to be innocent.

## What was measured, not assumed

All on this machine, 2026-08-16, against real stores served by `serve`.

### 1. The scale is not a billion. It is 2.85M, in one store.

```
875 stores · 4,338,305 nodes total
  linux    2,848,839
  k8s        479,308
  cpython    134,011
```

`AI-company-master` — the store in the screenshots — is **7,663**. This matters
more than it sounds: the right architecture is different at 7k, at 500k and at
2.8M, and picking the 2.8M answer for a 7k screen would be a rewrite that buys
nothing. Where a number below is extrapolated rather than measured, it says so.

### 2. `JSON.parse` is innocent — 8.8 ms

| payload | bytes | nodes | edges | JSON.parse |
|---|---|---|---|---|
| `/api/graph` (budgeted) | 566 KB | 833 | 1,865 | **1.6 ms** |
| `/api/graph?limit=8000` | 4.29 MB | 5,232 | 12,477 | **8.8 ms** |

A binary wire format was the obvious first idea. It would save ~8 ms once, at
load. Not the problem — **dropped from this proposal.**

### 3. The physics is innocent — 137 fps of headroom

Faithful port of `ForceGraph.tsx`'s `step()`, best of 30 ticks after warmup:

| store | current (objects + Map grid) | typed arrays + flat grid |
|---|---|---|
| 833 nodes | 0.74 ms/tick (1356 fps) | 0.38 ms/tick |
| 5,232 nodes | **7.28 ms/tick (137 fps)** | **4.51 ms/tick** |

Typed arrays win only **1.6×**, and the current sim already has 2× headroom at
60 fps. **Rewriting the physics in WASM would fix a bottleneck that does not
exist at this scale.** This is the single most important finding in this ADR,
because it is the work we were about to do.

### 4. The renderer is the culprit — and for unforced reasons

`ForceGraph.tsx:225-290`, per frame:

- **per-edge state change + draw call** (`:252-257`) — `strokeStyle` and
  `lineWidth` reassigned, then `beginPath`/`moveTo`/`lineTo`/`stroke`, 12,477
  times. In Canvas2D the state change costs more than the line.
- **`ctx.font` assigned per node** (`:287`) — re-parses a CSS font string
  thousands of times a frame. A known pathology.
- **`shadowBlur` per node** (`:267`) — the most expensive Canvas2D feature
  there is, applied per primitive.
- **`strokeText` + `fillText` per label** (`:288-290`) — text is the most
  expensive primitive, drawn twice for the halo.
- **`neighborSet()` called inside `draw()`** (`:230`) — an O(E) scan every
  frame; plus `Array.from(st.sim.values())` allocating every frame.

`MAX_SIM_NODES` (`:76`) caps the view at 600 nodes of 7,663. That cap is not a
budget decision — it is this draw loop's confession.

**The one number still missing** is browser-side frame time for `draw()`. It
cannot be measured in Node, and it decides Phase 0 versus jumping straight to
WebGL. It is the first spike, not a guess to build on.

## What games actually do (and the honest mapping)

The instinct is right, but the lesson from games is not "faster physics" — it is
**never submit work proportional to the whole world**:

| game technique | the graph-viewer equivalent |
|---|---|
| batch draw calls; minimise state changes | one path per *style class*, not per edge |
| instancing (one call, N transforms) | one instanced call for all nodes, one for all edges |
| SDF text atlas | labels as instanced textured quads — never `fillText` |
| frustum culling | draw only what is inside the viewport transform |
| LOD / virtualized geometry (Nanite) | cluster distant nodes; labels only past a zoom threshold |
| streaming the world in tiles | server-side layout + spatial tiles, stream the viewport |
| fixed-timestep sim decoupled from render | physics on a worker at its own rate |

At 2.85M nodes, points 5-6 are the *only* ones that matter. No renderer makes
2.85M labelled nodes legible; a map does not draw every street in the country.

## Prior art (2026)

- **GraphWaGu** — WebGPU compute shaders, Fruchterman-Reingold + Barnes-Hut,
  built for exactly this problem in the browser.
- **d3-force-webgpu** — d3-force API with WebGPU compute; reports 10-100×.
- **GraphGPU** — WebGPU pipelines for nodes, edges, halos AND labels, with
  physics on CPU or GPU.
- Reported thresholds: SVG/Canvas fall below 30 fps past ~10k elements while
  instanced WebGL holds; WebGL comfortably handles 50k+ instanced primitives.

Support, which decides whether any of it can be the foundation:

- **OffscreenCanvas ~95%** (Chrome 69+, Firefox 105+, Safari 16.4+) — safe.
- **WebGPU 84.68%**, and **Firefox still ships it off by default**. So WebGPU
  can be an accelerator, never the only path — a WebGL2 fallback is mandatory
  regardless. That is an argument for building the WebGL2 path *first*.

## Proposal — four phases, each independently shippable

**Phase 0 — fix the draw loop (Canvas2D, no new tech).** Batch edges into one
path per style class; hoist `ctx.font`; delete per-node `shadowBlur`; cache
`neighborSet` on selection change, not per frame; keep the node array instead of
rebuilding it. Expected large multiple at current scale for roughly a day.
Gated on the browser frame-time spike above, and **it must be measured before
and after** — this repo does not ship an unmeasured speedup.

**Phase 1 — OffscreenCanvas + worker.** Transfer the canvas to a worker; the
worker owns simulation *and* rendering, so no per-frame `postMessage` of
positions. The main thread does input only and can never jank. 95% support, with
a main-thread fallback on the same code path.

**Phase 2 — WebGL2 instancing + SDF labels.** One draw call for all nodes, one
for all edges, text from a signed-distance-field atlas. This is what lifts the
600-node cap honestly rather than by budget.

**Phase 3 — layout in Go, streamed by tile.** The browser must never receive
2.85M nodes; at that size the 4.29 MB/5k payload extrapolates to ~2.3 GB, which
is not a tuning problem. The server computes layout once, deterministically,
cached beside the store — the same move that already makes the Flow viewer fast
(7,663 nodes and 14,887 edges become a **4.4 KB** payload in Go, and the client
draws ~30 shapes). Then serve spatial tiles at an LOD for the viewport.

### Where WASM genuinely earns its place

Not as a faster `step()` — measurement 3 kills that. Its real value is
**one implementation of layout, compiled twice**: the Go layout that Phase 3
runs server-side, compiled to WASM for the worker when a user drags a node and
wants live re-settling. Same code, same determinism doctrine as the rest of the
store ("same input → byte-identical output"), and testable in Go, which a
hand-written JS force sim can never be. We already compile Go→WASI and host it
in wazero, so the road is known.

## Kill criteria

- **Phase 0** — if the browser spike shows `draw()` is *not* dominant, this ADR
  is wrong about the culprit and Phases 1-3 must be re-argued from that number.
- **Phase 2** — if fixed Canvas2D holds 60 fps at 10k nodes, WebGL buys nothing
  below the tile threshold and should wait for Phase 3.
- **WASM layout** — if the Go→WASM layout is not byte-identical to the native
  Go layout on the golden corpora, it does not ship; two layouts that disagree
  are worse than one that is slow.

## Open question for the owner

Phase 3 changes what the graph viewer *is*: from "the whole graph, budgeted" to
"a map you navigate". That is the same argument that killed the world view
(`2026-08-15` — "a map with no routes is a list in a costume"), and it deserves
the same scrutiny before we build it.

---

# POST-SPIKE — what the browser actually said

Headless Chrome 151, CDP, `devicePixelRatio` 2, best of N frames after warmup.
The renderer under test is `ForceGraph.tsx`'s `draw()` ported line for line,
against a batched rewrite drawing **the same picture** at the same camera.

## The spike killed the draft's own hypothesis, twice

**First: at the size we test, nothing is slow.** At 5,232 nodes `draw()` is
**2.97 ms** against a 7.28 ms sim — a ~10 ms frame, ~97 fps. The draft said the
draw dominated; it does not. Phase 0's kill criterion fired.

**Then the scaling run found the cliff — and it is above where we look.**
Zoom-to-fit, so culling can save nothing:

| nodes | edges | per-primitive (current) | batched | win |
|---|---|---|---|---|
| 833 | 1,865 | 0.49 ms | 0.39 ms | 1.3× |
| 5,232 | 12,477 | 1.60 ms | 1.20 ms | 1.3× |
| 20,000 | 48,000 | **360.7 ms** | **5.05 ms** | **71×** |
| 100,000 | 240,000 | **2,043.7 ms** | **35.4 ms** | **58×** |
| 500,000 | 1,200,000 | **10,481 ms** | **455.6 ms** | 23× |

Between 5k and 20k the current renderer goes from 1.6 ms to 360 ms — a **225×
cost for a 4× graph.** That is a wall, not a slope, and `MAX_SIM_NODES = 1200`
is exactly why nobody had hit it: the cap sits just below the cliff.

## The win is batching. It is NOT culling.

The draft assumed viewport culling would carry this. Decomposed on the 20k
graph: batched **5.34 ms** with culling, **5.05 ms** without. Culling is worth
nothing here, and would be worth nothing exactly when you need it most — zoomed
out to see the whole graph. **The entire win is not submitting per-primitive
state changes:** 5 strokes a frame instead of 48,000, one fill per colour, the
font string assigned once instead of per label, `shadowBlur` per batch instead
of per node.

This is the same lesson the ADR drew from games, arriving from the other
direction: the enemy is the draw call and the state change, not the geometry.

## What this changes about the plan

- **Phase 0 is not a tune-up, it is the whole answer up to ~100k nodes.**
  20k nodes goes 360 ms → 5 ms (**198 fps**); 100k goes 2.0 s → 35 ms (**28
  fps**). In plain Canvas2D. No worker, no WASM, no WebGL, no new dependency.
- **Phase 1 (worker/OffscreenCanvas) is demoted** to a jank fix, not a
  throughput fix. Worth doing so input never stalls, not worth doing for speed.
- **Phase 2 (WebGL) is deferred** — its own kill criterion said to wait if
  fixed Canvas2D holds up, and at 100k it does.
- **Phase 3 (Go layout + tiles/LOD) is the only remaining requirement**, and
  only above ~500k, where batched Canvas2D is still 456 ms/frame. linux at
  **2.85M nodes** is a Phase 3 problem and always was; no renderer makes 2.85M
  labelled nodes legible, and the payload alone extrapolates to ~2.3 GB.
- **WASM has no measured case anywhere in this ADR.** The sim it would have
  replaced runs at 137 fps. Its only remaining justification is the one the
  draft named — *one* Go layout compiled twice so the server and the browser
  cannot disagree — and that belongs to Phase 3, not to performance.

## Correctness gate

A speedup that changes what the user sees is not a speedup. The two renderers
are compared **pixel by pixel** at the same camera, and the difference is
reported as a share of inked pixels rather than of the canvas. The one genuine
behavioural change is compositing: overlapping translucent nodes composited
twice per-primitive and composite once when batched, so dimmed clusters read
slightly lighter. That is a real change and is recorded here rather than
discovered later.

## Status

- Phase 0 — **implemented**, `ForceGraph.tsx`, with the measurement table in
  the code so the next person does not re-derive it.
- Phases 1-3 — deferred, each with its trigger stated above.
