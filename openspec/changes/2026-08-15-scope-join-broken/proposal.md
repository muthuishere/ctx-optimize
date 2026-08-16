# ADR 16 — scope-by-join has never produced an `internal` port

Status: **RESOLVED 2026-08-16.** D1a / D1b / D1c were each measured against both
real stores (§Measurements). None produces `internal` without guessing, so the
kill criterion fires — narrowed: the CONSTANT is removed, the JOIN is kept.
`scope` is now written only when the join actually matches, and `external` is
no longer emitted at all. Implementation and gates in §Decision. The sections
below the rule are the original draft, unedited, so the finding and the choice
can be read against each other.

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

---

## Measurements — 2026-08-16

Every number below was taken by running against the two real stores named in
the table above (`~/ctxoptimize/wkdemo` for this repo, `~/ctxoptimize/reqsume`
plus its six module stores) and, where an option needs data the producer does
not yet emit, by scanning the corresponding real source trees. Nothing here is
quoted from a spec.

### The defect reproduces exactly

| store | consumes | provides | internal |
|---|---|---|---|
| ctx-optimize (`wkdemo`) | 56 external | 17 (no scope) | **0** |
| reqsume, 7 module stores | 163 external | 176 (no scope) | **0** |

reqsume splits as: `apps/api` 97 consumes / 176 provides, `apps/ui` 37, root 8,
`apps/extension` 10, `apps/home` 9, `e2e` 1, `regressiontest` 1.

### Two structural facts found while measuring, neither in the original draft

1. **Every transport except `network.http` has NO provides side at all.**
   `config.env`, `process.exec`, `storage.local` and `network.ws` are
   consumes-only in the shipped rule set, so for 100% of those ports the join
   has no possible input — not a mismatched one, an absent one. Across both
   stores, all 193 provides ports are `network.http` route paths.
2. **The join runs per BATCH, so it cannot cross a module store.** `markScope`
   folds one `schema.Batch`; reqsume's `apps/ui` and `apps/api` are gathered
   into separate stores. Even a perfect identifier match could not see across
   them, and making the join cross-store would make one module's stored
   metadata a function of another store's state — trading a wrong field for a
   broken byte-identity guarantee.

The join mechanism itself is not broken: `TestNormalizationJoinsSpellingsAtEmit`
has always proved it fires when both sides share a namespace. The shipped rule
VOCABULARY is what makes it unsatisfiable.

### D1a — path-aware join

Measured in three forms, weakest assumption first.

**D1a-i, path from an absolute URL literal.** Scanned every `https?://…` literal
across all six reqsume modules: **187 URL literals, 119 with a non-empty path,
7 whose path joins a provided route** (6 distinct).

| host | path | verdict |
|---|---|---|
| `test.local` ×6 | `/resume/build/async`, `/chat/iterate/async`, `/learning-path/generate/async`, `/resume/docx/async`, `/coverletter/docx/async` | a vitest stub host, not a workspace host |
| `api.example.com` | `/health` | **the exact false positive this ADR predicted** — `apps/api/src/pgkit/httpx/httpx.go:L21` |

**Zero hits on a host this workspace owns.** Precision against "is this a call
into our own API" is **0/7**. `/health` on a third-party host matching our
`/health` is the failure mode named in the draft, and it showed up on the first
real repo tried.

**D1a-ii, the defensible form: a bare path literal handed to a KNOWN HTTP
client** (`fetch(…)`, `axios.*(…)`, `http.Get(…)`, …). This is the only form
that involves no guessing — `fetch("/orders")` unambiguously is a same-origin
request to `/orders`.

| store | such call sites | joining a provided route |
|---|---|---|
| reqsume ui + home + extension | **0** | **0** |
| ctx-optimize | 2 (both in tests) | **0** |

**Zero on both stores.** Every reqsume UI call goes through the user-defined
wrapper `apiRequest()` in `apps/ui/src/lib/http.ts`, and this repo's dashboard
does the same through `dashboard-ui/src/api.ts`. The wrapper is exactly why the
honest form finds nothing.

**D1a-iii, the permissive form: treat ANY bare path string literal as a
consumed port.** This is the only form that produces a large number, and the
number is the argument against it.

| store | bare path literals | joining a provided route |
|---|---|---|
| reqsume ui + home + extension | 305 | 188 (97 distinct) |
| ctx-optimize | 2100 | 77 |

Precision, hand-classified against the actual call sites:

- `apps/ui/src/lib/config.ts` alone holds 123 of the 188. Split by the object
  they live in: **113 true positives** under `api.endpoints`, **10 false
  positives** under `routes` — browser-router paths that collide with API
  paths: `/profile`, `/dashboard`, `/knowledgebase`, `/admin/templates`,
  `/admin/ai-models`, `/admin/runtime-config`, `/admin/feedback`,
  `/admin/invoices`, `/admin/payments/settle`, `/admin/sql-query`. The same
  file defines `routes.admin.sqlQuery = "/admin/sql-query"` and
  `endpoints.protected.adminSqlQuery = "/admin/sql-query"` — identical strings,
  different namespaces, and nothing in the text tells them apart.
- Of the 65 matches outside that file, **10 more are wrong**: eight
  `navigate("/dashboard")` / `window.location.href = '/dashboard'` navigation
  sites (incl. `apps/home/src/components/layout/Header.jsx:L42`) and two paths
  occurring only inside comments (`lib/http.ts:L91`,
  `services/gdpr/exports.service.ts:L91`).
- **Precision 168/188 = 89.4%.** Recall is also imperfect in the other
  direction: 17 of the 130 `api.endpoints` entries do not match, because the
  API mounts them with a param suffix or composes them at call time.
- On ctx-optimize the form cannot even tell a provider from a consumer: 16 of
  the 77 matches are the `mux.HandleFunc("/api/graph", …)` registration lines
  themselves, and 14 are inside the committed minified dashboard bundle.

So D1a-iii costs an **8× inflation of the boundary census** in `apps/ui`
(305 candidate ports against 37 today), most of them not boundary crossings at
all, to buy a label that is wrong 11% of the time — and it still needs the
cross-store join that fact 2 rules out. An 89%-precision label is a guess in a
smart hat.

### D1b — workspace host registry

With a declared registry of `*.reqsume.com`:

| store | consumes ports | flip to internal | precision |
|---|---|---|---|
| reqsume, all modules | 163 | **11 (6.7%)** | **11/11 = 100%** |
| ctx-optimize | 56 | 1 (`127.0.0.1`) | 1/1 |

The eleven: `api.reqsume.com` (api, extension), `devapi.reqsume.com`
(extension), `app.reqsume.com` (api, ui, home), `console.reqsume.com` (ui),
`devconsole.reqsume.com` (extension), `reqsume.com` (api, home). Every one is
genuinely a workspace-owned host, so this is the only option with clean
precision.

Three things sink it anyway:

1. **Recall on the motivating case is zero.** `apps/ui` never names the API
   host anywhere in its source — verified: every `reqsume.com` literal in the
   UI is the marketing site, a `mailto:`, or an SEO canonical. The base URL
   arrives from `VITE_BASE_API_URL` at build time, and that env read is not
   even extracted as a port (it is an `import.meta.env` member access that
   lands under the `env-js` rule's other shapes). The 137-call wrapper case
   that motivated the whole boundary lane is invisible to a host registry.
2. **It cannot be derived, only declared.** The only automatic derivation
   available is `.ctxoptimize/config.json`'s `name` → `*.<name>.com`, which is
   a guess: a repo named `reqsume` does not thereby own `reqsume.com`. Honest
   D1b is a hand-written list — new config surface on a project that has spent
   its whole design budget avoiding one.
3. **It answers a different question than the field documents.** ADR 1 says
   "matches a `provides` port in this workspace". A registry says "the host is
   on a list the user wrote". Both can be honest; only one is `scope`. Five
   `localhost` consumers across four modules make the difference concrete —
   nothing decides whether they are ours.

### D1c — module-to-module edges instead of a flag

The direct measurement: for every ordered pair of reqsume's 7 module stores,
count identifiers where A consumes X and B provides X.

**0 edges. Total, across all 42 ordered pairs.**

There is no second signal to fall back on. The `provides` side is 176
`network.http` route paths in `apps/api` and nothing anywhere else; no consumed
identifier in any module intersects them. Drawing `apps/extension → apps/api`
from the extension's `api.reqsume.com` would require a host→MODULE map, i.e.
D1b plus a per-module declaration — strictly more configuration than D1b, for a
relation that still misses `ui → api`. D1c is not a cheaper reshaping of the
answer; it is D1b with an extra column.

## Decision — the kill criterion fires, narrowed to the constant

No option produces `internal` without guessing. Per the kill criterion the
field must stop being a computation that isn't one. It does **not** follow that
the join should be deleted: the join is correct code with a proven-firing test,
and a repo can legitimately author a rule that feeds it. What must go is the
value that was never computed.

**`markScope` writes `scope: internal` on a hit and NOTHING on a miss.**

- There is no `external` value from the producer. A miss is undecidable, not
  false: for a consumed host there is no provided host to compare against, and
  a relative path we do not serve may still be served by a proxy.
- Absence therefore reads as exactly "not proven internal", which is always
  true. On the default rule set that means `scope` disappears from every
  consumed port on every real repo — which is the honest output, and is
  visibly different from a field that answers.
- `boundaries` drops the per-transport `N external` count (it was always N of
  N) and keeps `N internal`. `--external` is redefined as "everything not
  proven internal", which is what it was always read as.
- The `add --json` door still ACCEPTS `scope: external`, unchanged. An adapter
  that genuinely knows a port is third-party may say so; our producer may not,
  because it does not know.

D1b is **recorded as measured-and-rejected, not refuted.** If the owner later
wants host ownership, the honest shape is a declared list under a different
name (`host_owner: workspace`), never `scope` — it answers "is this host ours",
not "does this workspace provide this identifier".

Withdrawn with it: ADR 1's parenthetical "(that IS the monorepo ui→api link)"
and ADR 15's `ui → api internal` headline example. Both were written from the
spec rather than from output. ADR 3's §4 limit 1 stands as written and is now
resolved in the direction it feared: `scope` cannot draw an internal/external
distinction, so the world view gains no roads from this.

## D2 result — the gate that could not fail, made able to fail

`internal/golden/testdata/repos/boundary/` gained a genuinely internal
boundary: `web/client.ts` calls `fetch("/orders")`, and `/orders` is provided
by `api/main.go` — a different module of the same fixture workspace. The rule
that reads a same-origin fetch path is committed at the fixture's own
`.ctxoptimize/boundaries.json`, because no SHIPPED rule pair shares a namespace
(fact 1 above); that asymmetry is itself pinned, so adding a namespace-sharing
default rule has to come through this gate.

The `scope-join` class asserts four things:

1. `/orders` and `/status` carry `scope=internal`;
2. `/nowhere` — fetched, provided by nobody — carries **no scope key**;
3. across EVERY port in the fixture, no value other than `internal` appears;
4. `boundaries` prints "internal" and never prints "external".

Proven red, each restored to green after:

| break | result |
|---|---|
| restore `else { scope = "external" }` | 9 failures: `/nowhere` wants "", plus all 8 ports flagged by assertion 3 |
| make the join never match (`if false && provided[…]`) | `/orders` and `/status` lose `internal`; the CLI assertion fires; golden snapshot diverges |
| reprint `N external` in `groupCounts` | assertion 4 fires |
| delete `web/client.ts` | all three ports MISSING; the CLI assertion fires |

Three more gates in `internal/analyze`, also proven red: `--external` falling
back to `scope != "external"` (returns nothing), the `Internal` counter
mis-keyed (2 total / 0 internal), and the entry sort reverting to
external-first (`MMM_INT` sorts ahead of `ZZZ_PLAIN`).

## Gate results

- `task ci` exits **0**.
- `CTX_OPTIMIZE_GOLDEN_CORPORA=~/ctx-golden-corpora task golden` exits **0**,
  including the corpus tier (`linux-block`, `newtonsoft`) and the judged tier:
  **linux-block 16.5/20 (floor 16.5)**, **newtonsoft 13.0/20 (floor 13.0)** —
  unmoved.
- Byte-identity holds: two consecutive `add` runs of this repo into the same
  store produce a byte-identical `graph/`, and `TestGoldenGatherIsDeterministic`
  covers the boundary fixture.
- Effect on this repo, re-gathered with the new binary: 87 ports, **0 carrying
  a `scope` key**, where before every one of the consumed ports said
  `external`.

## Note — carried forward, NOT resolved here

The `tier` observation at the end of this ADR is untouched: `EXTRACTED` remains
absent from real repos (0 of 73 measured here) while the fixture shows it. That
belongs to the next re-measurement of the AST rules' verified blocks, and is
the same defect shape — a field whose value is nearly constant carries little
information, for the reason a scope that was entirely constant carried none.
