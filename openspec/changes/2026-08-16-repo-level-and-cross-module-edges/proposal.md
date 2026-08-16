# ADR 22 — The repo is the unit: a module-grain level, and the edge that joins modules

Status: ACCEPTED — D0, D3 and D4 shipped. D1/D1b/D1c and D2 still open.
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

### The paths are IN THE CODE — measured, 111 of 149

The owner's instinct ("within a monorepo there is a high chance it calls
within — match on relative path, at medium confidence") turned out not to need
a heuristic at all. Taking apps/api's 149 concrete provided routes and grepping
apps/ui's source for each:

```
routes the UI source literally contains:  111 of 149
   /admin/ai-models                 lib/config.ts
   /admin/alert-recipients          lib/config.ts
   /admin/credits/add               lib/config.ts
   /admin/convert                   pages/AdminTemplateTesting.tsx
   …
```

Most sit in ONE route table (`lib/config.ts`) composed against a base URL, which
is why the extractor sees only the host: it resolves the base and discards the
path. There are ZERO relative-path requests in the whole repo — `fetch('/api/x')`
never appears — so a relative-path rule would have matched nothing. The pattern
is `${BASE}/admin/ai-models`, and the discriminating half is the literal.

This also settles the confidence tier. A path literal in one module matching a
route registered in another is a NAME MATCH, exactly like `calls` resolved by
unique name: **INFERRED**, never EXTRACTED. Two modules that both mention
`/health` are the AMBIGUOUS case the existing rules already know how to handle —
capped, excluded from traversals by default, surfaced on demand.

### It is a per-transport RULE GAP, not a missing capability

`websocket-js` (network.ws consumes) already records the whole path template:

```
identifier  /api/recon/${projectid}/partial/${runid}/logs
raw         /api/recon/${projectId}/partial/${runId}/logs
resolved    dynamic
```

`http-url-literal` (network.http consumes) records only `otel.server.address`.
Both are shipped rules, both INFERRED, both matching the same file extensions.
So the machinery to keep a path template, mark it dynamic and preserve the raw
form EXISTS and is proven in the field — it is simply not applied to HTTP.

### The corpus cannot prove generality, and says so

Across all 875 stores, only THREE repos are multi-module AND have a module that
provides HTTP routes:

```
repo             mods  provider-mods  routes  consumer-mods  consumed  path-valued
reqsume             7              1     176              5        65            0
redamon-master      5              3     154              4       477            7
toolnexus          13              1       1             11        46            0
```

That is not evidence of a rare pattern — it is evidence of a skewed corpus.
These stores are mostly open-source clones (linux, cpython, tabby), which are
single products, not API+UI monorepos. reqsume and redamon are the shape this
ADR is about and they are n=2. So the design must not be tuned to either:
the rule ships as a DEFAULT and the criterion is measured per repo and printed,
never baked in here.

### THE SPIKE: HTTP IS NOT THE MECHANISM. THE PACKAGE DEPENDENCY IS.

`scripts/spikes/monorepo-links.py`, run over every multi-module repo in the
local store (30 repos), counting directed module→module links by mechanism:

```
TOTAL: dependency 2168 · shares 513 · http 0 · ws 0 · process 0

deepseek-harness-master  233 mods   1912 dependency links
the-factory              229 mods    217
tabby-master              17 mods     29
agent-proxy                9 mods      4
agentic-nexus             14 mods      2
```

with examples that are exactly the edges a reader wants:

```
deepseek/apps/cli   -> packages/boot/app-boot   (@deepseek-ai/dsh-app-boot)
tabby-master/app    -> tabby-core               (tabby-core)
agentic-nexus/web   -> clients/typescript       (@agentic-nexus/client)
crypto-desk/apps/cryptodesk -> libraries/brain  (github.com/muthuishere/brain)
```

This inverts the ADR. It began with HTTP because that is the case that was in
front of us, and HTTP scores **zero observable links across thirty repos**
while the package dependency scores **2,168**. Worse for the original framing:
a dependency is a DECLARATION — the strongest evidence in the store, not a name
match — so it is EXTRACTED, where the HTTP join would only ever be INFERRED.

Three honest limits on that number:

- **It is a floor.** mastra-main (242 modules) contributes 0 because every one
  of its source paths is gone, so no module's identity could be read. Eight
  repos are in that state and are counted as zero, never dropped.
- **HTTP scoring 0 is not evidence HTTP is unused** — it is this ADR's own D1
  gap measured from the other side: the consumer path is not recorded, so no
  HTTP link is observable yet whether or not it exists.
- **Vendored code inflates it.** agent-proxy's four links are all
  `third_party/goproxywss` declaring `github.com/elazarl/goproxy`: a real
  dependency on a vendored copy, but an upstream package rather than a sibling
  product. A vendored/`third_party` path needs marking, or the count flatters
  itself.

## Proposal

**D0 — record what a module IS. This is now the first change, not the fourth.**
The store records what a module CONSUMES and never what it is: a package.json
is a `config` node with no `name`, a go.mod no `module` line. Every join in
this ADR fails on that same missing half — the HTTP one, and the dependency one
that turns out to be worth 2,168 links. Capturing module identity (npm `name`,
go `module`, crate/dist name) makes `dep:npm/@mastra/core` in one module resolve
to the sibling that publishes it, with no inference at all.

D0 also has to mark vendored trees, or `third_party/` copies of upstream
packages read as intra-product links.

> **D0 SHIPPED** (81d5e36). npm `name`, go.mod `module`, Cargo `[package] name`
> and pyproject/poetry `name` now emit a `publishes` edge to the SAME global
> `dep:<ns>/<name>` node a consumer's `declares` edge already targets, at
> EXTRACTED confidence, with `vendored: true` where the manifest sits under a
> vendored path. Verified end-to-end on a real gather: `ui declares
> dep:npm/@acme/api` meets `api publishes dep:npm/@acme/api`. Re-running the
> spike against the shipped edges reproduces the same 2,168 / 513 / 0 / 0 / 0,
> which is the point — D0 moved the evidence from "read the manifest off disk"
> into the store without moving the number.
>
> Two follow-ons D0 forced, both proven necessary by a test written red first:
> a module importing its OWN package must not resolve to a dependency node
> (deplink now excludes what a module publishes); and `vendor/`+`node_modules/`
> are PRUNED by the walker while `third_party/`+`external/` are walked, so the
> flag matters only for the latter — both behaviours are now pinned.

**D1 — `http-url-literal` keeps the path, the way `websocket-js` already does.**
Same field names (`raw`, `resolved: dynamic`), same tier, same rule file. This
is the default and it ships for every repo without configuration, because the
pattern it captures — a base URL composed with a literal path — is what a
monorepo client looks like everywhere, not just here.

OTel already names both halves:
`server.address` AND `url.path` / `url.template`. Recording the path turns
`ui consumes api.reqsume.com/admin/ai-models` and
`api provides /admin/ai-models` into a match, with a weight (how many of the
176 routes a consumer actually uses) and a `file:line` on each end. No
configuration, no adapter, no new producer — one extractor field.

The literal must be captured where it is WRITTEN, not only where it is
requested: 111 of these live in a route table, one hop from the call. That is
the same shape the boundary rules already handle for env-var names, so it is an
extension of an existing mechanism rather than a new one.

**D1b — matching normalises the template on BOTH sides.** A consumer writes
`/api/recon/${projectId}/logs`; a provider registers `/api/recon/:projectId/logs`
or `/api/recon/*`. Every interpolated or named segment collapses to one wildcard
before comparison, on both sides, or the two halves never meet even once the
path is recorded.

**D1c — per-repo rules are the configuration surface, and already exist.**
A repo whose client is a custom wrapper (`api.get('/x')` on a generated SDK)
adds a rule to `.ctxoptimize/` — the same door adapters and boundary rules
already use. Nothing new to design: the default covers the common shape, the
rule file covers the rest, and `boundaries verify` holds any added rule to its
own recorded ground truth exactly as it holds the shipped ones.

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

### How D4 is built — the design D0 made cheap

A fourth `Level`, above `LevelDir`: `LevelModule`. Everything downstream of
`owner` is unchanged, exactly as ADR 21's file and decl grains were — only the
grain of the unit moves, from a directory to a module store.

- **The cards** come from `modules.json`, which every multi-module repo already
  has: name, path, store key, node and edge counts. No graph read for the
  card itself.
- **The arrows** need one pass over each module's `edges.ndjson` to collect the
  `dep:` ids it declares and publishes, and over `nodes.ndjson` for its `port`
  ids. A module A → B arrow exists when A declares an id B publishes. That is a
  join on two DECLARATIONS: EXTRACTED on both ends, no inference, no heuristic.
- **`shares` stays dashed and separately named** — it is "both call the same
  external service", never a call.
- **Clicking a card enters that module's own store**, which is the existing
  directory-grain scene under a different `module=` key. The drill is one more
  level on top rather than a second mechanism.

**Cost, measured, on the worst case in the corpus.** the-factory is 228 modules
and 321 MB of graph. The estimate here was 1.8s, from a single-threaded `grep`;
the built version reads it in **0.147s** cold and 4.8ms warm, because the reject
filter never parses the ~95% of lines that are neither a manifest declaration
nor a port. mastra-main (241 modules) is 43ms. The cache key is every module
store's size+mtime, not the root's, and a test removes one declaration from one
module and requires the arrow to disappear.

A module store whose graph cannot be read contributes a card with no arrows and
says so in Notes. It is never dropped, because a silently missing module reads
as "nothing depends on it".

## What each edge at module grain would MEAN

Only relations that can be defended get drawn, each labelled distinctly:

| relation | evidence | available |
|---|---|---|
| `depends` | a module declares a sibling's package name — a DECLARATION, so EXTRACTED | **after D0**; 2,168 links across 30 repos |
| `calls` / `imports` | a code edge crossing modules | never — ids are module-relative |
| `provides`→`consumes` | one module's route literal matched by another's registration — INFERRED, never EXTRACTED | **after D1**; 111 pairs already visible on reqsume |
| `shares` | both modules touch the same external port | today, 6 pairs on reqsume |

`shares` is drawn dashed and named, never as an arrow of causation.

### D3/D4 SHIPPED — what the built level actually draws

Measured against the local store, on repos re-gathered so their `publishes`
edges exist:

```
repo             modules  depends  shares   cold     warm
agentic-nexus         13        1       0    2.0ms   0.6ms
crypto-desk           11        1      11    3.1ms
tabby-master          16       29       3    2.8ms
reqsume                6        0       6   10.8ms   0.5ms
the-factory          228        0       0  147.0ms   4.8ms
```

The numbers reproduce the spike exactly where the spike had a number: tabby's
29 links and crypto-desk's 1 are the same 29 and 1 `monorepo-links.py` counted
from the manifests on disk. reqsume draws the 6 `shares` pairs this ADR
measured by hand (12/4/3/2/2/1) and no `depends`, which is the honest answer —
its modules are joined by HTTP, and that is D1's job, not D4's.

the-factory and mastra draw nothing yet and say why on screen: their stores
predate D0, so no module publishes a name and nothing can point at one.

Three decisions the implementation forced, none of them in the draft above:

- **A package published by two modules is dropped, not guessed.** It means a
  broken manifest or two copies of one package; picking an owner would draw a
  confident arrow from no evidence. Counted and printed.
- **A vendored manifest never becomes the repo-wide owner of its package.**
  Otherwise every real consumer of `github.com/elazarl/goproxy` draws an arrow
  into `third_party/goproxywss` — the exact four links this ADR discounted in
  agent-proxy.
- **`shares` gets no arrowhead.** It is symmetric; which end is "from" is an
  artefact of sort order, and a head would turn a coincidence into a call.

**D3 came with a reachability bug of its own making.** Listing repos instead of
modules stranded every module outside the drawn top-12 — the scene samples, and
the chooser had been the way to the rest. The shell now carries a second select
listing every module of the current repo. A ranked sample does not get to decide
what is reachable.

**The browser suite caught what no unit test could.** A module card sent its
`dir` (`apps/ui`) where the click needed its store key (`agentic-nexus/web`),
so the card was clickable and navigated nowhere. Both viewers now resolve the
target through one `enterKey`, and `task e2e` clicks a real card and asserts the
address lands on a different store.

## Kill criterion

If, after D1, a real monorepo yields no `provides`→`consumes` matches, then the
module grain has only `shares` to draw and D4 is a chooser wearing a diagram —
in which case it ships as a plain list and this ADR closes. That criterion now applies to D1 alone, and D1 is no longer what the level
stands or falls on: D0's 2,168 dependency links already clear it by two orders
of magnitude on repos that have nothing to do with HTTP. The D1 test remains
reqsume, where a grep of the sources finds **111 of 149**, so D1 must recover a
number of that order.
Recovering a handful would mean the extractor is finding the calls but not the
route table, which is a different bug wearing this ADR's clothes. Whatever the
number, it is printed on screen, not asserted here.

## Cost note

D4 needs the server to read several module stores for one scene. The scene
cache added today already keys on the graph files' size+mtime, so the cost is
paid once per repo per re-gather — but the key must cover EVERY module store in
the repo, or a stale module would silently survive a re-gather of one part.
