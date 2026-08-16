# ADR 22 — The repo is the unit: a module-grain level, and the edge that joins modules

Status: DRAFT — owner sign-off required before any code
Date: 2026-08-16

## The ask

> "we need mastra-main to be selected and show all the graph from there and then
> if there is connection it will connect if not it will [not] … i dont need any
> repo root, the repo should be selected and then from there they can go in"

and, separately:

> "i am sure the apps/ui calls apps/api, i dont know why its not put in there —
> do we need a separate adapter there?"

Both are the same question at different grains: **what connects two modules of
one repo, and can we draw it?**

## What is measured, on reqsume

### The store already holds both halves — at incompatible grains

```
apps/api   provides 176 network.http  →  identifiers are PATHS      /admin/ai-models …
apps/ui    consumes  23 network.http  →  identifiers are HOSTS      api.reqsume.com …
```

A path never equals a host, so nothing joins. The consumer port carries
`otel.server.address` and nothing else — no `url.path`, and `location: null`.
The provider port carries the route template and no host.

**So no adapter is needed.** Both sides are already extracted from the code by
the boundary lane; we are discarding the half of each that would let them meet.

### A repo level WOULD have arrows — but not the ones expected

Port node ids are global (`port:network.http:>api.openai.com`), so they join
across separately-gathered module stores. Every module pair in reqsume that
shares one:

```
apps/api  <-> apps/ui          12 shared   api.openai.com, api.example.com, firebase …
apps/api  <-> apps/home         4 shared   app.reqsume.com, reqsume.com, www.google.com
apps/api  <-> apps/extension    2 shared   api.reqsume.com, localhost
apps/home <-> apps/ui           3 shared
apps/extension <-> apps/ui      2 shared
apps/extension <-> apps/home    1 shared
                                6 module pairs would draw an arrow
```

That relation is **"both call the same external service"**. It is real and it
is worth knowing. It is NOT "ui calls api", and an arrow that is read as a call
when it is a coincidence of dependencies is exactly the failure the world view
was killed for. It must be drawn as its own relation, labelled as its own
relation, or not at all.

### Code edges do NOT cross modules, and cannot

Checked directly: zero cross-module code edges in every store. Each module is
gathered separately and its node ids are paths relative to that module, so
merging the graphs would not connect them even in principle. Ports are the only
identity in the store that is global — which is why they are the whole answer
here.

## Proposal

**D1 — the consumer side keeps the path.** OTel already names both halves:
`server.address` AND `url.path` / `url.template`. Recording the path turns
`ui consumes api.reqsume.com/admin/ai-models` and
`api provides /admin/ai-models` into an exact match, with a weight (how many of
the 176 routes a consumer actually uses) and a `file:line` on each end. No
configuration, no adapter, no new producer — one extractor field.

**D2 — the provider side keeps its host when the code states one.** A route
registered under a declared base URL should carry it. Where the code does not
say, D1 alone still matches on path, which is the discriminating half.

**D3 — the store select lists REPOS, not modules.** `mastra-main`, once, rather
than forty of its packages. This is ADR 2026-07-19's decision applied to the
viewer; the grouped `optgroup` shipped today is the halfway house.

**D4 — a MODULE grain, above directory grain.** Selecting a repo derives a
scene whose cards are its modules, and clicking one enters that module's own
scene. It is the same drill as ADR 21 with one more level on top, and it is the
level the reader actually starts from. No "(repo root)" entry: the residual
store becomes a card like any other.

## What each edge at module grain would MEAN

Only relations that can be defended get drawn, each labelled distinctly:

| relation | evidence | available |
|---|---|---|
| `calls` / `imports` | a code edge crossing modules | never — ids are module-relative |
| `provides`→`consumes` | one module's route matched by another's request | **after D1** |
| `shares` | both modules touch the same external port | today, 6 pairs on reqsume |

`shares` is drawn dashed and named, never as an arrow of causation.

## Kill criterion

If, after D1, a real monorepo yields no `provides`→`consumes` matches, then the
module grain has only `shares` to draw and D4 is a chooser wearing a diagram —
in which case it ships as a plain list and this ADR closes. The test is
reqsume: `apps/ui` must resolve at least one of `apps/api`'s 176 routes, and
the number must be printed on screen rather than asserted here.

## Cost note

D4 needs the server to read several module stores for one scene. The scene
cache added today already keys on the graph files' size+mtime, so the cost is
paid once per repo per re-gather — but the key must cover EVERY module store in
the repo, or a stale module would silently survive a re-gather of one part.
