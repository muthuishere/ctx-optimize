# ADR 15 — a `boundaries` verb: the one answer nobody else can give

Status: DRAFT — owner review pending 2026-08-15. No product code until agreed.
Scope: a new read verb in `internal/app` over facts that already exist. No
producer change, no schema change, no new node kind, no ranking change.

## The problem is presentation, not capability

We extract 151 ports on reqsume, 404 on kubernetes, 11 gated in the fixture —
direction, transport, identifier, scope-by-join, sensitivity, tier, and a
`file:line` site for every one. **No competitor models any of it** (measured:
graphify, codegraph, gitnexus all `n/a`).

Here is how a user meets that today:

```
$ ctx-optimize nodes --kind port
${autosyncEnv}          [port]  port:config.env:>${autosyncEnv}
${key}                  [port]  port:config.env:>${key}
${name}                 [port]  port:config.env:>${name}
CTX_OPTIMIZE_BENCH      [port]  port:config.env:>CTX_OPTIMIZE_BENCH
...
```

Alphabetical, led by three DYNAMIC ports, no direction, no scope, no sites, no
grouping. And `query` cannot reach any of it at all (ADR 14: querying
`api.openai.com` does not return the node whose id is `api.openai.com`).

So the capability is real, gated, unique — and presented as a data dump.

## Why a verb, not a smarter `query`

**The house already answers this.** `deps` is a dedicated verb for dependency
facts; nobody is asked to `query "dependencies"`. `routes list` and
`manifests list` are the same shape. Boundary facts have MORE structure than
dependencies — direction, transport, scope, sensitivity, tier — so they earn a
shaped answer at least as much.

**It sidesteps the trap in ADR 14's second half.** Making `query` understand
that "shell out" means `process.exec` needs either a synonym map (maintenance
forever, still incomplete) or embeddings (which would destroy the determinism
the product rests on). A verb whose NAME is the answer needs neither: the skill
routes "what does this call / what does it spawn / which env vars" to
`boundaries`, and the mapping lives in the routing table where it is data.

**It cannot regress what already works.** No ranking change means the judged
floors (16.5 / 13.0) are untouched by construction. ADR 14's exact-match fix is
still needed — an agent querying a literal host should find it, that is a plain
correctness bug — but it stops being the only way in.

## D1 — the answer is a SUMMARY first, then drill-down

The output that makes this worth building is an architecture answer in one call:

```
$ ctx-optimize boundaries
reqsume — 7 modules, 151 ports

CONSUMES (what this system calls out to)
  network.http   4 external · 2 internal
    api.openai.com          EXTRACTED  apps/api/openai.go:42 (+2 sites)
    api.stripe.com          INFERRED   package.json (dep: stripe)
    ui → api                internal    apps/ui/src/lib/http.ts:18 (+136)
  config.env    87 keys · 11 SENSITIVE (names only, never values)
  process.exec   6 · 4 dynamic

PROVIDES (what this system exposes)
  network.http  12 routes   apps/api/routes.go, apps/api/admin.go

UNRESOLVED  9 ports carry a dynamic identifier (os.Getenv(varName) and
            friends) — the site is certain, the value is not.
```

Four properties make it useful rather than pretty:

- **Direction split.** "What we call" and "what we expose" are different
  questions and are currently interleaved.
- **Scope-by-join surfaced.** `ui → api` as an INTERNAL boundary is the
  monorepo architecture answer, and it is already computed — it is just
  invisible.
- **Dynamic ports summarised, never hidden.** They are ~30% of a real repo's
  list and would dominate an alphabetical dump; but deleting them would be the
  lie ADR 7 exists to prevent. Count them, name the transport, offer the sites.
- **Sites, so it is citable.** Every line traces to `file:line`, which is the
  product's whole contract.

## D2 — what it must NOT do

- **Never print a secret's value.** Env var NAMES only, `sensitive` flagged.
  This is standing doctrine and the verb is the most likely place to break it.
- **Never enumerate unbounded.** Budgeted like `query` (S1e: complete entries,
  hard cap, no pointer lists), with `--all` for the full list and an explicit
  count of what was withheld. Silent truncation reads as "that is everything".
- **Never merge tiers.** An INFERRED dep-tier port and an EXTRACTED call-site
  port are different claims and must be visibly different.
- **Not a new fact source.** It reads the graph. If an answer is wrong, the fix
  is a rule, not the verb.

## Issues, honestly

1. **Verb proliferation.** ~47 dispatch entries already. Mitigation: ONE rich
   verb with flags (`--direction`, `--transport`, `--external`, `--sensitive`,
   `--json`), not a family. And it replaces the `nodes --kind port` incantation
   rather than adding to it.
2. **Discovery.** An agent must know to call it. Partly solved: the skill's
   `activation-routing.xml` gained boundary routes with ADR 2's authoring work.
   This ADR must add the concept phrasings ("what does it shell out to", "what
   external services", "which env vars are secrets") as routes — that is where
   the synonym problem is cheap and reviewable.
3. **Multi-module federation.** reqsume is 7 modules; the summary must
   aggregate across them and still attribute each fact to its module, the way
   the federated `nodes` already does.
4. **It does not fix `query`.** ADR 14 D1 remains necessary and independent.
5. **Naming.** `boundaries` currently exists as a subcommand namespace
   (`boundaries verify`). Either this becomes `boundaries` with `verify` as its
   subcommand — tidy, and `boundaries` alone should already have meant this —
   or it takes a distinct name. Decide before implementing; a half-overloaded
   namespace is worse than either.

## Gates

- Output is deterministic and `--json` is machine-parseable.
- Secrets: a test asserting no env VALUE can appear in output, only names.
- The boundary golden fixture (ADR 13 D4) gains an assertion on this verb's
  output, proven red by breaking a rule.
- Judged floors untouched — by construction, but verified.
- No measurable gather cost: this is a read verb.

## Kill criterion

If the summary cannot be produced from the store as it stands — i.e. it needs a
new node kind, a new edge, or a producer change — stop and reconsider. The
entire premise is that the facts are already there and only the presentation is
missing. Needing new data would mean this ADR misdiagnosed the problem.
