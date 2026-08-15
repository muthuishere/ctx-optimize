# ADR 16 — scope-by-join has never produced an `internal` port

Status: DRAFT — owner review pending 2026-08-15. No product code until agreed.
Scope: `internal/boundaries` scope computation. No new node kind; the `scope`
field already exists and is already validated.
Found while building the `boundaries` verb (ADR 15), which renders scope
correctly and revealed that the value is always the same one.

## The defect

ADR 1 D1 defines:

> `scope`  internal | external — decided by JOIN, never by guess:
> internal iff the identifier matches a `provides` port in this workspace

Measured on both real stores:

| store | scope values | direction |
|---|---|---|
| ctx-optimize | **56 external, 17 none, 0 internal** | 56 consumes, 17 provides |
| reqsume (7 modules) | **163 external, 176 none, 0 internal** | 163 consumes, 176 provides |

**Zero internal ports have ever been produced**, on any repo, since ADR 1
shipped. The join runs; it can never match.

## Root cause: the two sides are different namespaces

A `consumes` port's identifier is a **host** — `api.openai.com`,
`api.reqsume.com`. A `provides` port's identifier is a **route path** —
`/orders`, `/api/graph`. String equality between them is unsatisfiable by
construction, so the join is a no-op that looks like a computation.

This is why reqsume — a monorepo whose UI genuinely calls its own API 137 times
through one wrapper, the case that MOTIVATED the whole boundary lane — reports
every one of those calls as `external`.

## Why this matters beyond a wrong label

1. **A documented capability does not exist.** ADR 1 D1 promises internal/
   external classification; ADR 15 used `ui → api  internal` as its headline
   example of "the monorepo architecture answer, already computed". It is not
   computed. That example was written from the spec, not from output — my
   error, and exactly the kind a golden gate on real output would have caught.
2. **It was gated only where it cannot fail.** The boundary fixture (ADR 13 D4)
   pins `scope` per port, and passes — because the fixture's expectations were
   recorded from actual output, which is all `external`. A gate that records
   reality cannot detect that reality is wrong. Same failure shape as the perf
   gate that recorded its own measurement.
3. **`drift` is weaker than it reads.** Its `dead-contract` finding ("provides
   with zero consumes") can never be cancelled by an internal consumer, because
   no consumer is ever internal.

## D1 — join on the resolvable part, or do not claim to join

Options, to be measured before choosing:

- **D1a — path-aware join.** Split a consumed URL into host + path and match the
  PATH against provided routes. `https://api.reqsume.com/orders` → `/orders`
  matches a provided `/orders`. Cheap and uses data we already have. Risk:
  false positives across unrelated hosts — `/health` is provided by everyone.
  Must therefore require the host ALSO be attributable to the workspace, which
  needs D1b.
- **D1b — workspace host registry.** Learn which hosts belong to this workspace
  (from module names, `.ctxoptimize/config.json`, or a declared list) so
  `api.reqsume.com` is known to be ours. Explicit and honest; costs
  configuration, which the project generally avoids but which is exactly what
  "never by guess" implies.
- **D1c — module-to-module edges instead of host matching.** The real question
  is "does `apps/ui` talk to `apps/api`", and the wrapper-call evidence
  (137 sites through `lib/http.ts`) is already in the graph. This may be a
  better shaped answer than a scope FLAG.

**If none is defensible, the honest fix is to stop emitting `scope` for
consumes ports** rather than emit a constant that reads as a computation. A
field that is always `external` teaches the reader something false.

## D2 — the gate must be able to fail

Whatever lands, the boundary fixture gains a case where an internal boundary
genuinely exists — a fixture module that provides a route AND another that
consumes it — and asserts `scope=internal`. Then break it and prove red. The
current gate cannot distinguish "the join worked" from "the join is a no-op",
which is precisely how this shipped.

## Kill criterion

If no join can be made without guessing, remove the field and say so in ADR 1.
"We do not compute this" is a better product than a field that always says
`external`.

## Note

ADR 15's second finding belongs here too: `tier` is almost always `INFERRED` on
real repos (EXTRACTED appears in the fixture, rarely in the wild), which makes
the tier column less discriminating than ADR 1 implies. Worth re-deriving when
the AST rules' verified blocks are re-measured — a tier that is nearly constant
carries little information, for the same reason a scope that is entirely
constant carries none.
