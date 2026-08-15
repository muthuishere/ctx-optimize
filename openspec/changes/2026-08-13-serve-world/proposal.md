# ADR 3 — the world view in serve: render the doors, not the hairball

Status: **KILLED BY ITS OWN CRITERION** (2026-08-15). The §8 mockup was built
and looked at before any endpoint shipped; the wall does not survive contact
with a real 273-port service. See §10 for the measurement and the fallback.
No product code was written. The prior status was DRAFT, owner review pending.

Revision note: the first draft of this file was written on 2026-08-13, before
the boundary lane existed. It planned against ADR 1/ADR 2 as *proposals* and
named transports (`storage.db`, `messaging.*`) that the shipped vocabulary does
not contain, and an `internal` scope that has never been produced. The lane
shipped in v0.14.0, so this revision is written against what the binary
actually emits. Every number below was taken by running `./bin/ctx-optimize`
(v0.14.0, commit d29c6e0) and `curl` against a live `serve` on 2026-08-15, on
two stores: this repo (`wkdemo`) and `reqsume`. Nothing here is quoted from a
spec.

## 1. What the dashboard renders today, measured

`serve` binds 127.0.0.1:4747 and serves an embedded React app over these read
routes, all of which answered 200: `/api/modules`, `/api/graph`, `/api/query`,
`/api/usage`, `/api/stores`, `/api/setup`, `/api/audit`, `/api/token`. The
store root held 875 module keys.

`GET /api/graph?module=wkdemo` returned:

| field | value |
|---|---|
| `nodes` served | 708 |
| `total_nodes` | 4834 |
| `edges` served | 2804 |
| `total_edges` | 10438 |
| `truncated` | `true` |
| payload | 755,206 bytes |
| wall time | 0.015 / 0.017 / 0.024 s (3 runs) |

The payload is a flat node/edge list. Node kinds served, in order:
`function` 240, `file` 207, `dependency` 59, `section` 50, `method` 44,
`module` 33, `config` 29, `task` 22, `document` 12, `image` 7, `type` 2,
`resource` 2, **`port` 1**. Edge relations served: `imports` 1205, `calls` 958,
`contains` 310, `resolves_to` 128, `co_changed_with` 119, `declares` 59,
**`consumes` 13**, `references` 6, `depends_on` 4, `uses_image` 2.

`?center=<id>` works on a port id. `center=port:process.exec:>git` returned 15
nodes / 14 edges (one port, fourteen files). `center=port:config.env:>OPENROUTER_API_KEY`
returned 3 nodes / 2 edges. So expansion is not the problem.

## 2. The gap, verified

The boundary layer does not reach the UI in any form.

**No route.** `GET /api/boundaries` → **404**. There is no server-side boundary
surface at all; the CLI's `boundaries`, `drift` and `services` verbs have no
HTTP counterpart.

**No client code.** Grepping `dashboard-ui/src` for `boundar|consumes|provides|
transport` returns 21 matches. All 21 are React `ErrorBoundary` (`App.tsx`,
`ErrorBoundary.tsx`, `styles.css`, `ForceGraph.tsx:307`). Zero are about ports.

**No visual identity.** `SPECIAL_COLORS` (`dashboard-ui/src/App.tsx:78-85`)
assigns fixed hues to `route`, `dependency`, `resource`, `task`, `image`,
`config`. `port` is not among them, so a port node draws from the rotating
generic `PALETTE` and is indistinguishable from a `function`. The Viewer reads
only `n.metadata?.producer` (`screens/Viewer.tsx:42`) — `direction`,
`transport`, `scope`, `sensitive` and `resolved` are transported to the browser
and then dropped on the floor.

**And the budget hides almost all of it anyway.** Port nodes have low degree, so
the degree-ranked budget starves them:

| module | port nodes in store | port nodes served by `/api/graph` |
|---|---|---|
| wkdemo (this repo) | 73 | 1 (1.4%) |
| reqsume/apps/api | 273 | 60 (22%) |
| reqsume/apps/ui | 37 | 4 (11%) |
| reqsume (root) | 8 | 8 (100%) |

The single port that survives the budget on this repo is
`port:process.exec:>git` — it survives because 13 files call `git`, not because
it matters. That is the whole boundary layer of this codebase, reduced to one
dot the UI cannot name.

## 3. The data a world view would actually render

`ctx-optimize boundaries --json` on this repo, in full:

- **73 port nodes**, 56 `consumes` / 17 `provides`.
- **93 `consumes` edges, 17 `provides` edges**, each carrying a `site` of the
  form `internal/gitinfo/gitinfo.go:L64` and a `rule` id (`process-go`,
  `routes-go`, …).
- Transports present: `network.http` 33, `config.env` 28, `process.exec` 12.
- Tier over all 73: `INFERRED` 58, `AMBIGUOUS` 15, **`EXTRACTED` 0**.
- `sensitive`: 1 of 73 (`OPENROUTER_API_KEY`).
- `resolved: dynamic`: 10 of 73 (`os.Getenv(varName)` and friends — site
  certain, value not).
- Whole payload: **17,869 bytes**, produced in **0.02 s** on three consecutive
  runs.

The rule vocabulary is 16 rules in `internal/boundaries/defaults.json`:
`network.http` 8, `config.env` 3, `process.exec` 3, `network.ws` 1,
`storage.local` 1 — **five transports, not eleven**. There is no `storage.db`
and no `messaging.*`. `internal/boundaries/services.json` carries 30 known
services (anthropic, aws, github, razorpay, …), matched by host glob and env
prefix.

Two more verbs already produce world-shaped output. `drift` reports "73 ports
in 73 contracts — 0 finding(s), 17 observation(s)", every observation a
`lower-tier-orphan` on a dashboard route. `boundaries verify` reports 16 rules,
7 exercised here, 9 with no sites, and **3 regressed** (`env-go` recall
0.92 → 0.71, `http-url-literal` 0.77 → 0.66).

**A world view is a rendering of 18 KB of data that already exists and is
already fast.** It is not a new extraction.

## 4. Limits, stated before the design

These are load-bearing. A view that hides them is worse than no view.

1. **`scope` is always `external` on a consumed port.** Measured: 56 external,
   17 none, **0 internal**, on this repo; 163/176/0 on reqsume. The join
   compares a consumed *host* to a provided *route path*, which is
   unsatisfiable by construction — see
   `openspec/changes/2026-08-15-scope-join-broken/`. **The world view must not
   draw an internal/external distinction from `scope`**, because it would be
   drawing a constant. If the owner wants ui→api rendered as an internal road,
   ADR 16 has to land first.
2. **`EXTRACTED` never appears in the wild.** 0 of 73 here. A three-way tier
   legend where one value never draws is decoration. Two values: INFERRED and
   AMBIGUOUS.
3. **A port list is a floor, not a census.** `boundaries verify` says
   `env-go` recall is 0.71 locally with 14 unmatched ground-truth sites. The
   view must say "at least N doors", never "the doors".
4. **`process.exec` is entirely AMBIGUOUS.** All 12. A rendering that draws
   `sh` and `cmd` as confidently as `api.github.com` is lying about 15 of 73
   ports.
5. **Every port node in the store today is a leaf hanging off files, not
   modules.** The `consumes` edges point file → port. Any module-level
   aggregation is computed by the renderer or the endpoint, and is therefore a
   new claim that needs its own test.

## 5. The design

One screen, additive. New endpoint, new file: `World.tsx` beside
`ForceGraph.tsx`. `/api/graph` and the existing Viewer are untouched.

**A module is a walled place.** One rounded enclosure per module key from
`/api/modules`, sized by node count, labelled with the key. Interior is
deliberately empty at first — the world view is about the walls, not the
furniture. `ForceGraph` already owns the interior question.

**A port is a door in the wall.** Not a node in a force layout — a fixed
position on the enclosure's perimeter, `provides` doors on one side,
`consumes` doors on the other. Doors are ordered deterministically by
`(transport, identifier)` so two machines draw the same wall. A door's label is
the `identifier` verbatim: `/api/graph`, `api.github.com`, `CTX_OPTIMIZE_STORE`.

**Transport is the door's shape, and shape is the only channel it owns.**

| transport | door | present here |
|---|---|---|
| `network.http` | archway, open | 33 ports |
| `network.ws` | archway with a standing bridge (persistent, not a trip) | 0 |
| `config.env` | slot in the wall (a value passes, nothing travels) | 28 |
| `process.exec` | gate to a yard | 12 |
| `storage.local` | cellar hatch | 0 |

Five shapes, matching the five transports the rule file actually defines. A
sixth transport appearing in the store draws as an unlabelled plain door and
the legend says "unknown transport", rather than the renderer inventing a
meaning.

**Tier is the door's edge.** INFERRED draws solid; AMBIGUOUS draws with a
broken outline and is counted separately in the legend ("15 of 73 ports are
AMBIGUOUS"). Never hidden, never silently promoted.

**Sensitive is a sealed door, and the name is all there is to seal.** This is
the one rule with no exceptions: **the graph contains no secret values, and the
renderer must have no code path that could display one.** The store holds
`{"identifier": "OPENROUTER_API_KEY", "sensitive": "true"}` and nothing more —
verified on the one sensitive port in this repo. So the seal is a visual
marker, not a redaction: there is nothing to redact. The door renders with a
seal glyph and the env-var NAME. Clicking it shows the name, the transport, the
tier, and the cite (`proof/agent/agent.mjs:L28`, 2 sites). It never shows, and
has no field from which it could show, a value. A test asserts the endpoint's
JSON contains no key named `value`.

**Movement is one thing only: traffic weight.** Each `consumes` door carries a
site count (`git` has 13, `CTX_OPTIMIZE_STORE` has 4, most have 1). Animate that
and nothing else — a slow pulse whose period is a function of site count, so
the busy doors read as busy from across the map. No walkers, no ravens, no
ships. Those were invented for edge classes that do not exist in the data, and
one honest animated channel beats eleven fictional ones. `prefers-reduced-motion`
replaces the pulse with the number.

**Unresolved doors are drawn ajar.** The 10 `resolved: dynamic` ports get a
half-open door and the label `${varName}` as stored. The site is certain, the
identifier is not, and the drawing should say exactly that.

**Nothing is drawn without a fact.** Every door traces to a `consumes` or
`provides` edge with a `site`; clicking any door yields a `file:line` the user
can pass to `ctx-optimize verify`.

## 6. API surface

One new read route.

```
GET /api/boundaries?module=<key>
```

Returns the same structure `ctx-optimize boundaries --all --json` returns
(17,869 bytes / 0.02 s on this repo), plus the per-port `sites` list needed to
place doors, plus the store's totals so the payload can state its own
truncation the way `/api/graph` does (`total_ports`, `truncated`).

It reads the same ndjson the CLI reads. **The read path must not create store
dirs** — the same rule `/api/graph` follows today; a request for a module with
no store returns an empty payload, not a mkdir.

**This is read-only, and therefore needs none of the mutation machinery.**
The dashboard's mutations (onboard, re-gather, config set, store delete, remote
push/pull) are loopback-only by `RemoteAddr` even when `--host` widened the
listener, require the per-process `X-Ctx-Token` from the loopback-only
`GET /api/token`, route through the same cmd closures the CLI dispatches, and
write to `audit.ndjson`. `/api/boundaries` changes nothing, dispatches no cmd
func, and writes no audit row. It sits beside `/api/graph` under the same read
posture and must not acquire a token check, because a token check on a read
route would imply the route can change something. If the world view ever wants
a button that re-gathers, that button goes through the existing mutation door
and gets audited like every other one — but D1 has no such button.

## 7. D1 — the smallest shippable slice

**One repo, one wall, doors, no animation.**

- `GET /api/boundaries?module=<key>` returning ports grouped by direction and
  transport, with sites and cites. Server-side; no new extraction.
- `World.tsx`: for the selected module, one enclosure, doors on the perimeter,
  five transport shapes, solid/broken edge for INFERRED/AMBIGUOUS, seal glyph
  for `sensitive`, ajar for `resolved: dynamic`.
- A click panel showing identifier, transport, direction, tier, site count and
  the first cite.
- A header line stating the floor: "73 ports found (at least — `boundaries
  verify` reports 3 regressed rules)".

That is one endpoint, one screen, one legend. On this repo it renders 73 doors
from 18 KB.

**Deliberately deferred**, and named so nobody thinks they were forgotten:

- Multiple modules on one canvas, and roads between them. Requires a
  module-to-module relation the store does not have (see limit 1) — it would
  have to be invented by the renderer, which is the failure this ADR is
  replacing.
- The four-level L0→L3 recursion from the first draft. It presumed the
  aggregation was free; it is not.
- Any internal/external distinction. Blocked on ADR 16.
- Hand-dragged, persisted layout.
- `drift` and `boundaries verify` as screens. Both are good data; both are
  their own ADR.
- Call/import edges in the world. The Viewer already draws them.

## 8. Kill criterion

Build nothing if either holds:

1. **The doors are not legible at real scale.** Measure before building the
   renderer: take the port counts from three real stores and lay out the
   perimeter. reqsume/apps/api has **273 ports on one module**. If 273 doors on
   one enclosure is an unreadable ring — and it may well be — then the wall
   metaphor does not survive contact with a real service, and the honest
   product is the `boundaries` CLI table plus a plain grouped list in the
   dashboard. Decide this with a static mockup of the 273, not after the
   endpoint ships.
2. **The rendered picture teaches something the CLI table does not.** Put
   `ctx-optimize boundaries` output beside the mockup and ask what question the
   picture answers first. If the answer is "none", the CLI already won, and the
   right move is a dashboard screen that renders the existing table.

A third, softer signal: if ADR 16 concludes that `scope` should be removed
rather than fixed, the world view loses the ui→api story that motivated it, and
D1 should be re-scoped to "the doors of one module" permanently rather than as
a first slice.

## 9. Gates

`task ci` green, and `task golden` for anything that touches extract, query or
analyze. D1 touches neither, but the golden net still runs because the
endpoint reads the store the golden fixtures pin.

The new route needs a test **that can fail**, and the repo has a standing
lesson about this: the boundary fixture's `scope` expectations were recorded
from actual output, so they pass while asserting a bug (see ADR 16 §2), and the
perf gate had the same shape. So the gate here is written the other way round:

- **`TestBoundariesEndpointShape`** asserts against the boundary fixture's
  known port set — the same fixture `boundaries verify` uses — with the
  expected counts written by hand from the fixture source, not captured from
  the endpoint. **Prove red**: delete one `consumes` rule from the fixture's
  expectations and confirm the test fails with a count mismatch, then restore.
- **`TestBoundariesEndpointHasNoValues`** walks the response JSON and fails on
  any key named `value`, `secret`, or any string matching the fixture's planted
  fake secret. **Prove red**: temporarily add a `"value"` field to the response
  struct and confirm the test fails, then remove.
- **`TestBoundariesEndpointDoesNotCreateStore`** requests a module key with no
  store dir and asserts the dir still does not exist afterwards. **Prove red**:
  temporarily call `store.Ensure` in the handler and confirm failure.
- **`TestBoundariesEndpointNoToken`** asserts the route answers 200 without
  `X-Ctx-Token` and writes zero rows to `audit.ndjson`. This pins the read
  posture so a later refactor cannot quietly make a read route mutable.

Each of the four must be demonstrated failing before the PR is opened, and the
demonstration named in the commit body. A gate that records what it measures is
not a gate.

## 10. Kill-criterion result — measured 2026-08-15: **KILLED**

§8 said to decide with a static mockup of the 273 before the endpoint ships.
The mockup was built from the real port nodes of three stores
(`ctx-optimize` 73, `reqsume/apps/api` 273, `reqsume/apps/ui` 37), laid out
exactly as §5 specifies: rounded enclosure, doors ordered by
`(transport, identifier)`, `provides` on one half of the perimeter and
`consumes` on the other, transport as the door's channel, dashed edge for
AMBIGUOUS, seal glyph for `sensitive`, faded for `resolved: dynamic`, labels
verbatim. Rendered headless in Chrome and looked at.

**Criterion 1 — legibility: passes only by growing the canvas.** At 1600x1000
the 273-door wall is legible on the left and right walls (horizontal labels)
and unreadable on the top and bottom, where 150-odd labels stand vertically at
~13px pitch and run off the canvas. Every label becomes readable at 2600x1700
— 2.9x the pixel area of a laptop viewport — which means the wall is read by
panning. 73 doors are comfortably legible at 1400x900, so this is a scale
failure, not a design failure. On its own this would be survivable.

**Criterion 2 — does the picture teach anything the CLI table does not: NO.**
This is the one that kills it. Put side by side on one 1900x1050 screen, the
`boundaries` table is fully readable and the wall scaled to the same screen is
a picket fence of sub-pixel text. And even at full legibility, every visual
channel in the design maps 1:1 onto a column the table already prints:

| wall channel | table column |
|---|---|
| which half of the perimeter | `CONSUMES` / `PROVIDES` heading |
| door shape | the `transport` group heading |
| solid vs broken edge | `INFERRED` / `AMBIGUOUS` |
| seal glyph | `SECRET` |
| ajar | the `${var}` identifier, printed verbatim |
| pulse period (D2) | `(+N sites)` |
| position on the perimeter | *nothing* — it is the sort order |

The last row is the finding. **Position carries no information, and no edges
are drawn**, so the picture has no adjacency, no locality and no topology — it
is the same sorted list, bent into a rectangle. The one thing a wall would earn
that a table cannot is *roads between walls*, and §7 defers those while §4
limit 1 shows the data cannot support them until ADR 16 lands. A map with no
routes is a list in a costume.

Evidence (headless Chrome renders, kept out of the repo):
`api273.png` (1600x1000, top/bottom illegible), `api273-big.png` (2600x1700,
fully legible), `ctx73.png`, `side.png` (table vs wall on one screen).

**Consequence.** D1 as written is not built. The gap in §2 is real and
unchanged — the dashboard still has no boundary surface of any kind, and the
degree-ranked budget still starves ports to 1 of 73 — so the fallback §8 names
is the right product: **`GET /api/boundaries` unchanged as specified in §6/§7,
and a dashboard screen that renders the existing grouped table** rather than a
wall. The endpoint is identical in both branches; only the renderer changes.
That is its own (small) ADR, and no product code is written until the owner
agrees to it.

## 11. Dependencies

None blocking for D1: the boundary lane shipped in v0.14.0 and the data is in
the store today. ADR 16 (`2026-08-15-scope-join-broken`) blocks any
internal/external rendering. ADR 15's `drift` and `boundaries verify` stay CLI
verbs here.
