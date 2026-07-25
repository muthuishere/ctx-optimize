# Spike — P4 (proto grammar pack) / P5 (`reads_config`) / P5b (spec-route ↔ code-route link)

Effectiveness spike for `proposal.md`. Every number below comes from a command
run on this machine on 2026-07-25 with the installed `ctx-optimize` (built from
HEAD). No product code was touched; all work happened in a scratch dir with
`CTX_OPTIMIZE_STORE` pointed at scratch. Anything constructed rather than
found in the wild is flagged **CONSTRUCTED**.

---

## P4 — proto via tree-sitter grammar pack

### VERDICT: **P4c for now — pack-only (P4a) is UNSAFE, and the mitigation written into the proposal does not work as written.**

Three findings, in order of how badly they hurt:

1. A pack **cannot** emit a package-qualified label. Definitive NO (§P4.3).
2. Worse: **even a qualified label would not prevent the collision** — call
   resolution keys on the *bare* declared name, not on the node label. The
   proposal's stated mitigation ("the resolver keys on `d.label`, so proto decls
   carrying fully-qualified labels never collide") is incorrect (§P4.3).
3. The damage is real but bounded: **126 correct `calls` edges destroyed
   (1.10% of 11 501) and 1 wrong edge invented**, measured with a REAL proto
   grammar pack on a real Go repo (§P4.2).

Proto is therefore a **binary change or nothing**: the Go side must either
qualify what goes into the resolution key, or keep non-`calls`-participating
lanes out of the resolution map entirely. Both are ADR-sized decisions.

### P4.1 Prevalence and the collision rate

```
find ~/muthu/gitworkspace -name '*.proto' -not -path '*/node_modules/*'
```

3 094 `.proto` files, but the distribution is lopsided: 3 063 in `chromium`,
29 in `testrepos/beam`, 2 elsewhere (`hackathonprepaer/starters/go-service`,
a ctx-optimize benchmark fixture). Only two repos hold **both** `.proto` and
committed generated code:

| repo | .proto | *.pb.go | *_pb2.py | *.pb.cc |
|---|---|---|---|---|
| chromium | 3 063 | 2 | 960 | 991 |
| testrepos/beam | 29 | 23 | 4 | 0 |

Chromium's generated Python is a **non-collider**: modern builder-style
`_pb2.py` declares no classes at all (`grep -c '^class ' ` = 0 across the first
20 sampled files), so message names never become Python decls. Collisions come
from Go / Java / C++ generated code, which does declare named types.

Collision rate on beam (`collide.py`: proto `message|enum|service|rpc` names vs
every code decl bare-name in a real gathered store):

```
CTX_OPTIMIZE_STORE=<scratch>/store ctx-optimize add .    # in a copy of beam
python3 collide.py <scratch>/beam <scratch>/store/beam
```

- 560 distinct proto decl names; 70 087 distinct code decl bare-names.
- **373 proto names collide → COLLISION RATE 66.6% repo-wide.**
- 325 of those 373 already resolve to >1 file, i.e. the name is *already*
  ambiguous — adding proto there changes nothing; the dangerous cases are the
  48 that are currently unique.

Call resolution is *module-wide*, not repo-wide, so the module-scoped rate is
the operative one (`collide_mod.py`, per gathered module):

```
module sdks/go                        names=18   collide=15   83.3%
module playground                     names=71   collide=23   32.4%
module sdks/python                    names=6    collide=1    16.7%
module model/pipeline                 names=148  collide=0     0.0%
module model/fn-execution             names=87   collide=0     0.0%
… (7 more modules at 0.0%)
TOTAL (module-scoped): 576 names, 39 collide = 6.8%
```

The reading: where proto and its generated code sit in the **same** module —
the normal shape of a Go/gRPC service repo — collision is **83%**. Where the
generated code is produced at build time and not committed (beam's Java
modules), it is 0%. Beam's low aggregate (6.8%) is an artifact of beam
generating Java at build time; it is not the number a typical gRPC repo sees.

### P4.2 Bounded edge-loss regression, measured with a real pack

Target: `beam/sdks/go` copied to scratch as a single-module repo (real code,
17 684 code nodes, 11 501 `calls` edges baseline).

```
CTX_OPTIMIZE_STORE=<scratch>/store_go ctx-optimize add .   # baseline
grep -c '"relation":"calls"' <store>/gosdk/graph/edges.ndjson  → 11501
```

Three runs, each diffing the full `calls` edge set (`comm -23/-13`), not just
the count — a net count hides simultaneous loss and invention:

| # | what was added | lost | gained |
|---|---|---|---|
| A | the module's **own** 18 real proto names, simulated as python class decls | **0** | 1 (spurious) |
| B | 332 proto names (beam `model/**/*.proto` co-located) as python class decls — **CONSTRUCTED co-location** | **126** | 1 (spurious) |
| C | the **real proto grammar pack** parsing the real `model/**/*.proto` files copied into the module — **CONSTRUCTED co-location, real extraction** | **126** | 1 (spurious) |

C reproduces B exactly (126 / 1), which is the point: the python simulation and
the real pack are the same defect. C also added 359 real proto decl nodes.

**126 / 11 501 = 1.10% of all `calls` edges destroyed.** The loss does not land
on generated code — it lands on **hand-written API**:

```
60 edges → pkg/beam/core/graph/window/trigger/trigger.go   (Always, Repeat, AfterProcessingTime, AfterEndOfWindow…)
41 edges → pkg/beam/testing/teststream/teststream.go       (Config.AddElements, Config.AdvanceWatermark…)
22 edges → pkg/beam/model/pipeline_v1/*.pb.go              (Field, Schema, ApiServiceDescriptor…)
 3 edges → coder.go, handlerunner.go
```

Proto messages are named after domain concepts, and so are the hand-written
functions that build them — so the collateral damage is in the code a user
actually asks about, not in the generated noise.

The 1 gained edge is a **wrong** edge, and it appears in run A too — i.e. with
zero restructuring, purely from the module's own real `v1.proto`:

```
pkg/beam/core/runtime/graphx/serialize.go::encodeType  --calls-->  pkg/beam/core/runtime/graphx/v1/v1.proto::Type.ChanDir
```

A Go function is now recorded as *calling a proto enum*. Adding proto decls
does not only delete true edges, it manufactures false ones.

### P4.3 Can a pack emit a qualified label? **NO — definitively.**

Label construction, exact citations (`internal/extract/code/`):

- `code.go:562` — `qual := name` (the bare identifier from the `names` mapping).
- `code.go:563-568` — the only qualifier available to a pack: the **enclosing
  decl's** qual (`parentID[idx+2:] + "." + name`). A proto `package acme.orders.v1;`
  clause is a *sibling statement*, not a container, so nothing nests under it.
- `code.go:569-580` — the other qualifier, `ReceiverQualify`, is a Go-method
  special case.
- `code.go:591` — `Label: qual` is emitted.
- `langs.go:224-231` — `packConfig` has exactly six fields:
  `name, exts, decls, names, calls, imports`. There is **no** field for a
  package prefix, and `NameStrategy` / `ReceiverQualify` are Go-only, not
  exposed to packs (`langs.go:275-277` builds the pack `Lang` from those six
  fields only).

Empirically confirmed with a real proto pack on a real proto file
(`package acme.orders.v1;`):

```
type    | Order                     | orders.proto::Order              L5-L8
type    | Order.Item                | orders.proto::Order.Item         L7-L7
service | OrderService              | orders.proto::OrderService        L10-L12
method  | OrderService.PlaceOrder   | orders.proto::OrderService.PlaceOrder L11-L11
enum    | Status                    | orders.proto::Status             L9-L9
```

Labels are bare / nesting-qualified. `acme.orders.v1.Order` is not producible
by a pack. **P4a (pack-only) is UNSAFE.**

**And the proposed mitigation is broken independently of that.** Resolution
does not use the node label:

- `code.go:599` — `res.decls = append(res.decls, declRef{id: id, label: name, …})`
  stores the **bare** `name`, not `qual`.
- `code.go:78` — `label string // unqualified name` (the type's own comment).
- `code.go:328` — `byName[d.label]` builds the resolution map from that bare name.

So even if a pack *could* emit `acme.orders.v1.Order` as a node label, the
resolver would still index it as `Order` and still collide. The requirement
stated in the proposal ("proto decls carrying fully-qualified labels never
collide") is **false against HEAD** and must be rewritten: the fix has to change
`declRef.label` (or exclude proto-lane decls from `byName` entirely).

### P4.4 `grammar build` — attempted, worked, two side notes

```
ctx-optimize grammar build https://github.com/treesitter-grammars/tree-sitter-proto   # 404 Not Found
ctx-optimize grammar build https://github.com/mitchellh/tree-sitter-proto            # OK
```

- Built in well under the 200s timebox; zig was already on PATH (no download).
- **wasm size: 1.7 MB** — cheap; the pack lane costs nothing in the main binary.
- The auto-suggested mapping is **not usable as generated**:
  `{"decls": {"enum": "enum"}, "names": ["constant","identifier"], "calls": [], "imports": ["import"]}`
  — no `message`, no `service`, no `rpc`. Extraction only worked after hand-editing
  `decls` to `{message,enum,service,rpc}` and adding name node types. A shipped
  proto pack would need a curated, tested mapping (the `_review` marker is doing
  its job).
- Note (unrelated to P4, worth a separate look): `grammar build` writes to
  `~/ctxoptimize/grammars` and **ignores `CTX_OPTIMIZE_GRAMMARS`**, which the
  *loader* does honor (`langs.go:236`). The pack it wrote into the user's real
  grammars dir was moved out to scratch afterwards; `~/ctxoptimize/grammars`
  contains only `cljgo.*` again.

### P4 requirement if it is ever revisited

The golden fixture the proposal asks for must pin **both** directions:
edge count must not drop, and no `code --calls--> proto-decl` edge may exist.
Run C produced exactly such an edge, so a count-only golden would pass it.

---

## P5 — `reads_config`

### VERDICT: **P5a is viable but ONLY on P3's dotted paths — and only for literal, dotted keys. With today's flat labels it must not ship.**

Measured precision, string-literal matching only, two real Spring repos:

| label shape | literal hits | genuine config reads | false | precision |
|---|---|---|---|---|
| flat top-level (`app`, `spring`, `pmjay`) | 6 | 0 | 6 | **0.0%** |
| bare leaf names (`name`, `port`, `enabled`, `model`) | 215 | 4 | 211 | **1.9%** |
| dotted full paths (what P3 emits) | 43 | 43 | 0 | **100.0%** |

Recall ceiling (keys with ≥1 genuine reference in code): **28.6%** (yaml repo),
**46.7%** (properties repo).

### P5.1 Does the code extractor index string literals? **No — not as graph data.**

- `internal/extract/code/code.go` collects exactly three things besides
  nodes/edges: `declRef` (`code.go:75-79`), `callSite` (`code.go:70-74`),
  `routeSite` (`routes.go:35-38`). There is no string-literal collection and no
  string-literal node kind anywhere in the code lane.
- String literals *are* read, but only inside the route recognizers, as local
  unquoting helpers: `pyStringLit` (`routes.go:124`), `jsStringLit`
  (`routes.go:133`), the JSX/object-literal path readers
  (`frontend_routes.go:155, 226`).

So P5a needs new collection work in the code lane (a `configRef` sites list,
gathered on the same preorder visit the way `routeSite` is) — but the
**precedent already exists**: framework recognizers that pull a literal out of a
bounded local scan and defer resolution. P5b-style recognizers are a smaller
delta than a general literal index.

### P5.2 What `config_key` looks like today

`internal/extract/markdown/markdown.go:147-192` (`extractConfig`) emits
**top-level keys only** — `markdown.go:157-159` explicitly `continue`s on any
indented line. Consequence, measured:

- `application.properties` (dotted keys live at column 0): 30 keys → **30
  `config_key` nodes with dotted labels** — already matchable today.
- `application.yml` (nesting): 63 real dotted keys → **47 flat labels**, and
  every nested key (`app.providers.ollama.enabled`) has **no node at all**.
  P5 over yaml is not low-precision — it is *absent*.

### P5.3 Recall and precision on real repos

Repos: `enterprisewebagent` (Spring Boot + yaml, 353 code files) and
`satish-projects/.../pjay-workflow` (Spring Boot + properties, 237 code files).
Both gathered into a scratch store; keys parsed from the real config files; the
literal sweep done with grep/python (doctrine: exhaustive literal-string sweeps
stay grep's job). A hit counted as *genuine* only when the line carries a real
config-read form (`@Value("${`, `@ConfigurationProperties(prefix`,
`@ConditionalOnProperty`, `getProperty(`, `registry.add(`, `getenv`,
`process.env`, `viper.Get`, `os.environ`, `config.get(`).

**enterprisewebagent (yaml, 63 dotted keys, 3 top-level):**
- keys with a genuine reference: 18 → **recall ceiling 28.6%**
- flat top-level labels as string literals: 0 genuine / 0 total (they simply
  aren't written as literals). Substring matching instead of literal matching
  collapses to **2.4% precision** (455 hits, 11 genuine) — `app` inside
  `'./app/App'`, `spring` inside `org.springframework…`, `management` inside
  `"Session management"`.
- bare leaf names: 196 literal hits, **0** genuine → 0.0%. Worst offenders:
  `openai` 43, `name` 27, `anthropic` 27, `copilot` 19, `ollama` 18, `model` 12.
- dotted paths: 8 literal hits, 8 genuine → **100.0%**

**pjay-workflow (properties, 30 dotted keys):**
- keys with a genuine reference: 14 → **recall ceiling 46.7%**
- flat top-level (`pmjay`, `server`): 6 hits, 0 genuine → 0.0%
- bare leaves: 19 hits, 4 genuine → 21.1% (`name` 10, `bucket` 2, `type`, `secret`, `path`)
- dotted paths: 35 hits, 35 genuine → **100.0%**; hand-verified — every one is
  a `@Value("${jwt.admin-expiration-minutes}")`, a
  `@Value("${pmjay.pipeline.batch-size:500}")`, or a testcontainers
  `registry.add("s3.bucket", …)`. Zero accidental matches.

Why recall stalls near 30–47%: `@ConfigurationProperties(prefix = "app.auth")`
binds a whole subtree with **one** literal — the prefix. The individual leaf
keys are never written in code, so no literal-match scheme can ever reach them.
Prefix binding is a recognizer job, not a matcher job.

### P5 recommendation

- **Do not** emit `reads_config` against flat / leaf labels: 0–2% precision.
- **P5a, gated**: literal match, but only for keys containing at least one dot,
  matched as a whole quoted literal or `${…}` placeholder. Measured precision
  100% (43/43) on real repos, recall ~30–47%. Hard prerequisite: **P3** (yaml
  dotted paths), otherwise yaml — the majority format — has no matchable nodes.
- **P5b on top** to reach the rest: a `@ConfigurationProperties(prefix=)` /
  `@Value` / `viper.Get*` / `os.environ[...]` recognizer set, riding the same
  visit as `routeSite`, is what covers prefix-bound subtrees. Higher confidence,
  and cheaper than a general string-literal index.
- Sequencing: **P3 → P5a(dotted-only) → P5b**. P5 before P3 is not shippable.

---

## P5b — spec route ↔ code route link

### VERDICT: **DEFER the raw-label join. It scores 0% on every real repo measured. A normalized + mount-aware join reaches 69.8% on one repo and 12.8% on another, with 25–28% ambiguous — not enough to ship as a naive linker.**

The synthetic Flask/openapi.yml case joins on the raw label because Flask
decorators carry the *absolute* path. Real Express repos — the only real
spec+code pairings found on this machine — carry **mount-relative** paths, and
the raw join is 0/63 and 0/47.

### P5b.1 Prevalence

```
find ~/muthu/gitworkspace \( -iname 'openapi.y?ml' -o -iname 'swagger.y?ml' -o -iname '*openapi*.json' \) …
grep -rl --include='*.y*ml' -E '^(openapi|swagger):' ~/muthu/gitworkspace
```

Across ~50 repos: **exactly one** repo has a spec **and** code routes that both
lanes recognize today.

| repo | spec | format | code routes | recognized by ctx-optimize? |
|---|---|---|---|---|
| `bossbroprojects/review-workspace/review-app` | `docs/openapi.yaml` (84 KB, generated) | YAML | Express 5, `apps/api` | **both lanes: yes** |
| `dilproject/iepapp` | `apps/ui/docs/openapispec.json` | JSON | Express, `apps/api` | code yes, **spec no** |
| `cli-agents-workspace/enterpriseclaw/reference/react` | `docs/openapispec.json` | JSON | (vendored copy of the same app) | spec no |
| `opencode/packages/{docs,sdk}` | `openapi.json` | JSON | TS SDK, not framework routes | spec no |
| `raman-workspace/volentis` (librechat) | `.well-known/openapi/*.yaml` | YAML | — | spec yes, but these describe **third-party** APIs (askyourpdf, scholarai) with no local implementation |

Two structural facts fall out before any join math:

1. `extractYAMLRoutes` (`internal/extract/markdown/yamlroutes.go`) is
   **YAML-only**. 4 of the 5 specs found are JSON. JSON spec support is a
   prerequisite for the feature to have subjects.
2. Specs in a repo frequently describe *someone else's* API (librechat). A
   linker must treat zero-match as normal, never as a defect.

Route-lane coverage for the code side: FastAPI, Flask, Express, NestJS
(`routes.go:13-30`) plus angular/react/vue-router (`frontend_routes.go:13-23`);
spec side: OpenAPI/Swagger YAML, Drupal `*.routing.yml`, k8s Ingress.

### P5b.2 Join rate on the real repos

```
CTX_OPTIMIZE_STORE=<scratch>/store_routes ctx-optimize add .    # in copies of each repo
# then partition kind=="route" nodes by source extension, dedup by id
```

`review-app` — **63 spec routes, 79 code routes** (all spec routes from one file):

| join | exactly one match | zero | ambiguous (≥2) |
|---|---|---|---|
| raw label | **0 (0.0%)** | 63 (100%) | 0 |
| param-normalized (`{id}`/`:id`/`<id>` → `{}`, trailing slash stripped) | **0 (0.0%)** | 63 (100%) | 0 |
| normalized **+ mount-suffix** (spec path *ends with* code path) | **44 (69.8%)** | 3 (4.8%) | 16 (25.4%) |

`iepapp` — 47 spec operations (parsed from the JSON spec by hand, since the lane
can't read it), **257 code route nodes**:

| join | exactly one | zero | ambiguous |
|---|---|---|---|
| raw label | **0 (0.0%)** | 47 (100%) | 0 |
| param-normalized | **0 (0.0%)** | 47 (100%) | 0 |
| normalized + mount-suffix | **6 (12.8%)** | 28 (59.6%) | 13 (27.7%) |

### P5b.3 The normalization question

Normalization of param placeholders and trailing slashes buys **exactly zero**
on both repos. The gap is not spelling, it is **scope**:

```
spec:  DELETE /api/v1/organizations/untag/{profileOrgId}     docs/openapi.yaml
code:  DELETE /untag/:profileOrgId                           modules/organization/organization.routes.ts
spec:  GET    /api/v1/auth/admin/role-requests
code:  GET    /admin/role-requests                           modules/auth/auth.routes.ts
```

Express routers are mounted with `app.use("/api/v1/organizations", router)`, and
the code lane emits the router-relative path (correctly — it is what the literal
says). Recovering the absolute path means resolving the `app.use` mount prefix
to the imported router module: real cross-file work, not a normalization. The
suffix heuristic is a *stand-in* for that resolution, and it is what produces the
69.8% — along with the 25.4% ambiguity, because a suffix match is not a mount
resolution.

Where a framework does emit absolute paths (FastAPI/Flask decorators, NestJS
`@Controller` composition — `routes.go:26-30`), a raw-label join should work.
That case was **not measured on a real repo** — no such repo with a spec exists
on this machine; the only evidence is the coordinator's synthetic scratch repo.

### P5b.4 False-join risk — real, and large

Duplicate code-route labels within a single repo, measured:

- `review-app`: **8 labels shared by 2–5 route nodes.** `GET /me` in
  auth / profile / review / subscription; `GET /profile/:profileId` in
  organization / quality / recruiter / reference / review; `GET /`, `POST /`,
  `GET /:id` in several modules.
- `iepapp`: **21 shared labels.** `GET /` appears in **20** different route
  files; `POST /` in 16; `DELETE /:id` in 13.

Under mount-relative labels this is not an edge case, it is the norm: every
CRUD router declares `GET /`, `POST /`, `GET /:id`. Any join that is not
mount-resolved will hit these, and the calls discipline then forces a drop —
which is exactly the 25.4% / 27.7% ambiguous buckets. Dropping is the *correct*
behaviour; the point is that a quarter of the candidate links are unrecoverable
without mount resolution.

No case was found of the same *absolute* path being served by two modules, and
no repo had multiple spec files (`review-app`: 1 spec file, 0 duplicated spec
labels). So the false-join risk is entirely on the code side, entirely caused by
mount-relative labels.

### P5b recommendation

1. **Defer** the linker as specified (raw-label join): 0% on real repos.
2. The prerequisite is not a linker, it is **absolute code-route paths** —
   Express/NestJS mount resolution (`app.use("<prefix>", router)` → the imported
   router's routes), which is the same class of cross-file work as
   `internal/extract/deplink` and should follow deplink's shape exactly: its own
   producer, its own `Replace` lifecycle, `INFERRED` + `synthesized_by`,
   ambiguous candidates dropped (`deplink.go:1-11`).
3. Also required before the feature has subjects: **JSON OpenAPI specs**
   (4 of 5 real specs are JSON; `yamlroutes.go` is YAML-only).
4. If a cheap win is wanted now, the honest scope is: link only where the code
   route path is already absolute (FastAPI/Flask/NestJS) and the normalized
   labels match exactly, drop everything else. Value unmeasured on real repos —
   no real corpus for it exists on this machine.

---

## Reproduction

Scratch dir (all artifacts, scripts and stores):
`/private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-ctx-optimize/3cb25356-4d1c-416f-be28-ea86609d3c63/scratchpad/spike-p45/`

- `collide.py`, `collide_mod.py` — P4 collision rates
- `edges_base.txt` / `edges_a.txt` / `edges_b.txt` / `edges_pack.txt` — P4 edge diffs
- `grammars/proto.{wasm,json}` — the built proto pack + the hand-corrected mapping
- `p5.py`, `p5b_prec.py` — P5 recall / precision
- repo copies: `beam`, `gosdk`, `ewa`, `pjay`, `reviewapp`, `iepapp`,
  `protorepo`; stores: `store`, `store_go`, `store_p5`, `store_routes`, `store_proto`
