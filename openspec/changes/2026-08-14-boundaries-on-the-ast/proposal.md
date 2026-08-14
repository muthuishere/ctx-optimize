# ADR 7 — boundaries on the AST: retire the regex lane

Status: DRAFT — owner review pending 2026-08-14. No product code until agreed.
Owner directive: *"we need to make the boundaries better and also incremental
better and never regex kind of."*
Supersedes ADR 6's D2 (regex alternation) — a faster regex is the wrong fix.
Scope: `internal/boundaries` rule engine + where it runs. The `port` model
(ADR 1 D1), the config ladder, and every emitted fact shape stay as they are.

## Why the regex lane must go — three failures, one cause

**1. It is slow, because it needs its own walk.** Measured (ADR 6): full
gathers +60% (kubernetes, spring, reqsume) to +120% (ts-typescript) against a
≤+5% budget. The lane re-opens every file and scans it once per rule.

**2. It is inaccurate, and we published the numbers ourselves.** The shipped
`verified` blocks, honestly measured, land where a regex must land:

| rule | recall | why it stops there |
|---|---|---|
| env-go | 0.81 | `os.Getenv(varName)` — the name is a variable |
| env-py | 0.82 | variable keys, bare `environ` |
| process-go | 0.65 | `exec.Command(bin, …)` — variable binary |
| process-py | **0.00** | argv is always a variable |
| routes-java | 0.73 | `path=` form; javadoc `{@code}` false positive |

Every miss has the same shape: **a regex sees text, so a variable is
invisible.** A parser sees a *call to `os.Getenv` whose argument is not a
literal* — which is not a miss at all, it is a known env read with a dynamic
identifier, a fact we already model (`resolved: dynamic`, AMBIGUOUS).

**3. It is a second source of truth.** Comments, strings and vendored samples
match text; the javadoc `{@code}` false positive is exactly that.

One cause: **the boundary lane reads bytes when the tree is right there.**
Every file is already read and already parsed into an AST by
`internal/extract/code`. The regex pass is a second, worse look at data we
already have in a better form.

## The precedent is already in this repo

`internal/extract/code/routepacks.go` is the declarative, AST-level
counterpart to hand-coded recognizers — and it is *data*, exactly as the owner
required of adapters:

```json
{"name": "myframework",
 "rules": [{"call": "registerRoute", "path_arg": 0, "handler_arg": 1,
            "method_arg": -1, "method": "GET"}]}
```

Its own doc states the properties we want: a rule matches any call expression
whose callee's **last identifier** equals `call` (so `Getenv` and `os.Getenv`
both match) **in ANY language whose grammar maps call nodes** — every embedded
language and every grammar pack. Arguments are positional over named children.
Literal-or-silent. Malformed packs fail LOUDLY.

That is the whole design, already proven and shipped. Boundaries should be the
same mechanism with a different emit.

## D1 — boundary rules become AST patterns, riding the existing walk

A rule matches a node shape, not a line of text:

```json
{"id": "env-go", "transport": "config.env", "direction": "consumes",
 "match": {"call": "Getenv", "identifier_arg": 0},
 "dynamic": "ambiguous"}

{"id": "process-exec", "transport": "process.exec", "direction": "consumes",
 "match": {"call": "Command", "identifier_arg": 0}, "dynamic": "ambiguous"}

{"id": "webstorage", "transport": "storage.local", "direction": "consumes",
 "match": {"call": "setItem", "receiver": "localStorage", "identifier_arg": 0}}
```

Semantics carried over from ADR 1 unchanged: `identifier_arg` must be a string
literal to earn an EXTRACTED port; a non-literal argument emits the port with
`resolved: dynamic` at AMBIGUOUS instead of vanishing — **the recall fix and
the honesty rule are the same mechanism.** Rule id, `metadata.rule`, `site`,
scope-by-join, normalization and the `sensitive` flag are untouched.

Not every boundary is call-shaped. `process.env.X` is a member expression and
needs a second pattern kind (`{"member": "env", "receiver": "process"}`).
Enumerate the shapes actually required by the shipped rules before designing
the schema — do not invent a general pattern language.

## Spike results (2026-08-14) — they changed the plan

**The cost is the regex, not the second read.** Measured directly, separating
the walk+read from the rule scanning:

| corpus | files / MB | walk + read | **regex** | regex share | ports found |
|---|---|---|---|---|---|
| go-kubernetes | 17,858 / 128 MB | 330 ms | **5.29 s** | **94%** | 349 |
| ts-typescript | 71,585 / 268 MB | 1.36 s | **12.43 s** | **90%** | 42 |
| java-spring | 10,033 / 52 MB | 192 ms | **2.48 s** | **93%** | 386 |

**This kills ADR 6 outright.** Its preferred fix (D1 walk fusion) removes only
the read — 6–10% — and would have missed the ≤5% budget while taking on
concurrency risk. Its D2 (regex alternation) optimises the right 90% but keeps
every accuracy defect. Both were aimed at the wrong problem.

The line to remember: **TypeScript spends 12.4 seconds of regex to produce 42
ports.** Each file is scanned 10–11 times (7 rule regexes + 4 SDK matchers).
Under AST-riding the marginal cost is a map probe per call node on a parse
already paid for, so essentially *all* of both columns disappears.

**Query API: available, but not needed.** The shim exports only
`co_alloc/co_free/co_parse/co_symbols` (`internal/extract/code/wasm.go:125`),
returning a flat preorder record stream — no `ts_query`. But the query engine
is already compiled into `treesitter.wasm` (`build.sh:61` pulls in
`lib/src/lib.c`, which includes `query.c`; the artifact carries 130 `ts_query*`
symbols). Exposing S-expression queries would cost ~40 lines of `shim.c` plus a
wasm rebuild — **and we should not**, because routepacks' technique already
works over the flat stream with no wasm change at all.

**Rule shapes: five, not one.** `routepacks`' `{"call": …}` is insufficient.
The shipped rules actually need: **call** (`os.Getenv`, `exec.Command`,
`localStorage.setItem`), **member** (`process.env.X`, `import.meta.env.X`),
**subscript** (`os.environ["X"]`), **decorator/annotation** (`@GetMapping("/x")`,
`@app.get`, `[HttpGet]`), **new-expression** (`new WebSocket(url)`), and **bare
string literal** (`http-url-literal`). Design the schema around exactly these;
do not invent a general pattern language.

**Precision bonus is real but small — do not headline it.** Measured on 5,776
k8s Go files, quoted-URL hits were 406 in code vs **5 in comments (1.2%)**.

## D2 — one walk, by construction

Rules evaluate inside `internal/extract/code`'s existing AST visit, at the
call/member nodes it already visits for `calls` edges. The second walk, the
second read, and the 14-scans-per-file all disappear — not optimized, deleted.
This satisfies ADR 2's D4 in the letter it was written in: *a rule that cannot
ride the engine's walk is declared, not smuggled in as a second pass.*

Producer identity stays `boundaries` (provenance, `store.Replace` scoping, and
the audit trail all key on it) even though evaluation rides the code walk.

## D3 — what regex keeps

Regex does not vanish from the product; it stops being the boundary lane.
It remains correct and RE2-linear for: the `search` verb's literal sweep,
`boundaries verify`'s ground truth (which **must** stay independent of the
capture mechanism — an AST rule verified by an AST rule is recall 1.00 by
construction, the trap ADR 2's skill already warns about), and any boundary in
a file with no grammar (plain `.env`, `.txt`).

That last one is the honest limit: **an AST lane only covers parseable files.**
Rules for unparsed formats must be declared as such, not silently dropped.

## D3b — SITE recall and VALUE recall are different numbers

The spike split the misses, and only one half is recoverable. Say so plainly
rather than promising a clean sweep.

**Fully recovered — regex shape brittleness.** `env-go`'s `[A-Z][A-Z0-9_]+`
excluded lowercase and 1-char names; `env-js` missed `npm_config_*`;
`routes-express` missed receiver-less chained `.get`; `routes-java` missed
`@RequestMapping(path=…)`; `routes-py` missed multi-line decorators. An AST
reads the literal with no shape assumption and does not care about lines. For
these, **the tier can honestly rise to EXTRACTED — and D3's cap on
`routes-go`/`routes-py` (measured 1.00 and 0.99 but pinned at INFERRED because
"regex without AST never claims EXTRACTED") loses its stated reason.**

**Site recovered, value NOT — the honest ceiling.** `os.Getenv(varName)` (31
k8s sites), `exec.Command(binVar)` (**73 k8s sites**), `subprocess.run(argsVar)`
(**every django site — the 0.00**), `new WebSocket(urlVar)`. The AST makes
these *visible but AMBIGUOUS* with `resolved: dynamic`. That is a real and
large gain — `process-py` stops reporting "this repo spawns nothing", which is
a lie, and starts reporting "spawns at N sites, arguments dynamic" — but
**no 1.00 is promised on values.** Resolving them needs constant propagation:
trivial for an in-file `const x = "FOO"`, impossible across modules or
reflection.

**Neither recovers:** `os.environ` copied wholesale, URLs built by
concatenation (partially foldable), aliasing (`getenv := os.Getenv`),
reflection.

**Precision can EXCEED routepacks.** Its matcher keys on the callee's *last*
identifier, so a rule for `os.Getenv` would also fire on a local `Getenv()`.
The AST holds the receiver, so boundary rules should match it — better, not
merely equal.

## D4 — incremental, made real

The owner asked for this directly, and the spike found the mechanism.

**Measured: the regression hits the developer's inner loop, not just the first
gather.** OLD (`0a2b192`) vs HEAD, *touch one file and re-gather*:

| corpus | scenario | old | new | delta |
|---|---|---|---|---|
| ts-typescript | one file touched | 8.15s | 22.46s | **+176%** |
| go-kubernetes | one file touched | 13.46s | 21.73s | **+61%** |
| ts-hono | one file touched | 0.36s | 0.59s | +64% |
| py-django | one file touched | 2.76s | 4.14s | +50% |
| py-django | **no change** | 0.20s | **0.17s** | −15% (faster) |
| reqsume (multi-module) | one file in `apps/api` | 0.71s | 0.92s | +30% |

The earlier audit's "incremental is unaffected" was **accurate but
incomplete** — it measured only the *no-change* re-gather, the one incremental
case that does no work at all. That case is genuinely fine and even slightly
faster. **Every case where you actually edited something pays the full
regression.** Editing one `.ts` file in the TypeScript compiler costs 22.5s
instead of 8.2s. That is the loop a developer sits in all day.

Multi-module containment is the only real mitigation: touching `apps/api` left
5 of reqsume's 6 modules short-circuited. It helps monorepos and does nothing
for single-module repos like kubernetes.

**Today incremental is all-or-nothing per module.** `treeSignature`
(`internal/app/multimodule.go:601`) hashes sorted `rel\0mtimeNano\0size` over
the module; lever 1 (`:732`) is binary — signature matches, skip everything;
one byte differs, re-extract everything. `.ctxoptimize/` is deliberately inside
the signature (`:626`) so rule changes correctly invalidate.

Consequence, stated bluntly: **touch one file in kubernetes and you pay the
full 5.3 s of regex re-scanning all 17,858 files.** The earlier audit's
"incremental is unaffected" is true only for the zero-change case.

**Do NOT build a boundaries-specific per-file cache.** It would layer
bookkeeping over a cost that ADR 7 deletes. The spike's three subtleties are
recorded for whoever does build one: node creation is first-writer-wins
(`boundaries.go:375`) so replay must be in sorted-`rel` order or output
changes; the cache key needs the ruleset AND services-registry hash, not just
content; and most entries are *negative* (17,858 files → 349 ports), so
"produced nothing" must be cached or every miss re-scans.

**The right incremental work is bigger and belongs in its own ADR:** cache the
whole `extractFile` result — code + routes + boundaries together — keyed on
content hash, making lever 1 per-file instead of per-module for *every*
producer. That is where a one-file change genuinely costs one file of work.

Two facts that ADR must inherit:

- **Correctness of today's incremental path is sound.** Incremental vs a
  `--force` full gather of the same state produced identical identity sets —
  44,911 nodes / 134,974 edges, zero difference either way. No stale or missing
  facts. The lever is crude, not wrong.
- **ADR 5 blocks the obvious verification method.** Two *full* gathers of the
  same tree with the same binary differ by 50–60 nodes (the node-id collision
  bug), e.g. django's `RelatedFieldWidgetWrapper.choices` alternating between
  the `@property` getter and its setter. So byte-identity cannot be used to
  verify an incremental path until ADR 5 lands; identity-set comparison is the
  only rigorous check available. **ADR 5 is therefore a prerequisite for
  trustworthy incremental work, not an independent nicety.**

Gotcha to record: `treeSignature` skips `build`, `dist`, `node_modules`,
`vendor`, `target`, `*-out` and `.git`. Edits under those names never trigger a
re-gather — consistent with the extractor, but it means a source tree that
legitimately lives in `build/` is invisible to both.

## Migration, gates, kill criterion

- **Byte-comparable output**: the AST lane must reproduce every port the regex
  lane produced *and the ones it missed*. Differences are reviewed one by one
  as recall gains, never accepted wholesale.
- **Tiers get re-derived, not assumed.** Every rule's `verified` block is
  re-measured under the tier law (≥0.95 EXTRACTED / 0.70–0.95 INFERRED /
  below reject). If an AST rule measures 0.98 it earns EXTRACTED honestly —
  and the D3 cap on regex routes stops applying, because the objection was
  "regex never claims EXTRACTED", not "rules never do".
- **Perf**: restore ≤+5% total gather vs `0a2b192`; add a gather wall-time
  ceiling to the golden net, whose absence let +60% ship green.
- **Determinism**: byte-identical output across two gathers (and note ADR 5 —
  node-location instability is a separate, pre-existing defect).
- **Kill criterion**: if the wazero host cannot express these patterns without
  a second parse, ADR 7 fails and we fall back to ADR 6's cheaper fix while
  keeping the quality argument on file. The feasibility spike answers this
  BEFORE any implementation.

## Open questions the spike must answer first

1. Does our wazero tree-sitter host expose the tree-sitter **query API**, or
   only parse + node walk? (`routepacks.go` implies a Go-side walk is enough —
   confirm.)
2. Which shipped rules are call-shaped, member-shaped, or neither?
3. How much of ADR 6's regression is the second *read* versus the 14 *scans*?
   If reads dominate, D2 alone recovers it.
4. What does an AST rule actually recover for `process-py` (measured 0.00)?
