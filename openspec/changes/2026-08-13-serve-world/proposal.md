# ADR 3 — the world view in serve: the realm, additive only

Status: DRAFT — owner review pending 2026-08-13. No product code until agreed.
Part 3 of 3. Depends on ADR 1 (`2026-08-13-boundary-model-and-defaults`) for
ports/transports/scope and ADR 2 (`2026-08-13-boundary-authoring`) for their
trustworthiness. Pins what a week of rendering spikes settled (reqsume store,
17,695 nodes; final artifact iteration 2026-08-13) so it survives the
conversation that produced it.

## Constraint (owner, 2026-08-13)

**The existing graph view is untouched.** New endpoint, new screen, new file:
`/api/world` + `World.tsx` beside `ForceGraph.tsx`. Same discipline ForceGraph
already proves: canvas-only geometry, color-keyed picking, settle-and-stop rAF,
server-budgeted payloads, `prefers-reduced-motion` snaps everything.

## The level model — one recursion, four depths

```
L0 repo      top-level units (apps/services) + external services + db
L1 dir       a unit's quarters (subtree-aggregated — see Aggregation)
L2 dir       … recursively, folder by folder
L3 file      the symbols within, each citing its L-range
```

Navigation: click to select (narrate), click again to enter; Esc / breadcrumb
climbs. Levels are never replaced destructively — the level you left is as you
left it. Camera: drag pans, wheel zooms; dragging a BUILDING moves the building
(session-local; a persisted hand layout is a later, separate decision).

## Edge classes → movement (the differentiation contract)

One visual grammar, driven entirely by `transport` + `scope` + `tier` from ADR 1 — the renderer adds no classification of its own:

| edge class | source of truth | drawn as |
|---|---|---|
| module call (in-process) | `calls`, aggregated to the visible level | **walker** on a footpath between buildings |
| call leaving the visible yard | `calls` whose target owner is outside | **rider** to the rim, dashed |
| call arriving from outside | reverse of the above | **visitor** from the rim, dotted, slate |
| dependency / import | `imports` + `resolves_to` (needs ADR 1 D7) | **road with signpost**; followable when resolved |
| internal http (ui → api) | `port` net.http, `scope=internal` (ADR 1 join rule) | **caravan** between units at L0 |
| external http / SDK service | `port` net.http, `scope=external` (incl. ADR 1 D5 services) | **ship** to a shore fixture outside the map |
| websocket / SSE | `port` network.ws | **rope bridge** — persistent, unlike riders |
| messaging / queue | `port` messaging.* | **raven** — departs, never returns (async made visible) |
| db read/write | `port` storage.db | **cart** to the granary fixture |
| process spawn | `port` process.exec | **runner** to the yard gate |
| config / env read | `port` config.env | **scroll** fixture; `sensitive` ports render as a sealed strongroom, contents never shown |
| tier on any of the above | `tier` | EXTRACTED solid · INFERRED dashed · AMBIGUOUS fogged — never hidden, never asserted |

Hover any messenger → who (`from → to · n`) and cargo (top carried symbols /
paths / callee names). Roles (handlers/services/db/tests…) tint buildings; test
files and their messengers get the reserved test color.

## Aggregation rules (the two bugs the spikes hit, as law)

1. **Subtree ownership.** A flow touching `pgkit/queue` belongs to the `pgkit`
   building when `pgkit` is the visible spot. Exact-path matching orphaned a
   busy toolkit; never again.
2. **No silent caps.** A yard draws at most N buildings; the remainder folds
   into an explicit "… +K more" that says the store holds them all. Symbol
   counts appear as `·n`, never as invented geometry.

## `/api/world` (all reads; loopback rules as today)

```
GET /api/world?module=…                    L0: units, fixtures, unit-level flows
GET /api/world/level?path=…                one level: spots + intra flows + in/out
GET /api/world/node?id=…                   narration card: facts + citations
```

Server-budgeted like `/api/graph` (cap + "of M" honesty in every payload);
deterministic layout server-side (sunflower + relaxation, seeded by path) so
two machines see the same world. The read path never creates store dirs.

## Honesty rules carried from the spikes

- Every drawn thing traces to a store fact; every narration cites `file:line`
  (`verify`-able). A prop with no fact behind it is not drawn.
- AMBIGUOUS is visible fog, not an omission — clicking it names the command
  that would settle it.
- Where the store lacks the boundary data (pre-ADR repos), the view renders
  structure + calls only and SAYS the boundary layer is absent — it never
  falls back to grep.

## Dependencies

Blocked by ADR 1 D1/D2 (ports in the store) for the boundary rows; the
structure/calls rows work against today's store. ADR 1 D7 unlocks followable
import roads; ADR 1 D5 unlocks named external services with their config
attached; ADR 2's verify keeps what this page shows honest over time.
