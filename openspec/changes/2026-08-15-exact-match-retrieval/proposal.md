# ADR 14 — the boundary graph is unreachable by query

Status: DRAFT — owner review pending 2026-08-15. No product code until agreed.
Scope: `internal/query` scoring only. No producer change, no schema change, no
new node kind.
Found while building ADR 13's boundary golden gate: the facts gated perfectly
and could not be retrieved.

## The defect, reproduced on our own store

`api.openai.com` is a `port` node in this repo's store, with that exact string
as its id, label and `metadata.identifier`. Querying for it verbatim:

```
$ ctx-optimize query "api.openai.com"
openAPIRoutes       internal/extract/markdown/yamlroutes.go L116-L167
buildOpenAPIBatch   internal/sources/connectors/openapi.go L154-L281
fetchOpenAPI        internal/sources/connectors/openapi.go L97-L148
api   [config_key]  internal/golden/testdata/repos/dockerstack/compose.yaml L2
api   [service]     internal/golden/testdata/repos/dockerstack/compose.yaml L2
```

**The node whose id IS the query string does not appear.** `Tokenize` splits it
to `api` / `openai` / `com`, and `openAPIRoutes` — which shares two of those
tokens by camelCase splitting — scores higher.

The boundary-fixture agent hit the same shape independently: querying the exact
host `api.weather.example` ranked its port **seventh**, behind four nodes from
`api/main.go`, and **all seven tied at score 1.51**. An exact identifier match
earns no boost whatsoever over a file that merely shares one token. Adding a
README to the fixture was enough to reorder the results, which is the signature
of a scoring function with nothing to separate candidates.

## Why this is worse than a ranking nit

We shipped a boundary lane, a services registry and a 30-vendor catalogue, then
gated all of it (ADR 13 D4, 11 ports, 7 proven-red gates). **All of that is
correct and none of it is reachable by the verb an agent actually calls.**

The capability exists via `nodes --kind port`, `card <host>` and `drift` — but
those require knowing the vocabulary first. The instructions we ship tell an
agent to `query "<terms>"` to FIND things; for boundaries, that path returns
noise.

It also means ADR 13's new question classes would score near zero on `query`
today. That is the honest number and we should publish it, but the cause is a
scoring gap, not a missing capability — so the fix belongs here, not in a
question set.

## Second, related failure: concept queries do not retrieve

Measured on this store:

| asked | returned |
|---|---|
| "what external services does this call" | `externalSet`, `externalSet.suppress`, `newExternalSet` |
| "what does it shell out to" | no hits |

Lexical IDF has no notion that "shell out" ≈ `process.exec` or that "external
services" ≈ a `consumes` port. This is a HARDER problem than exact match and
should NOT be solved by embeddings — that would break the determinism the whole
product rests on. Options worth measuring, in increasing ambition: a small
curated synonym map for the closed transport vocabulary (`process.exec` ←
"spawn", "shell out", "subprocess"); boosting `port` nodes when the query
contains boundary-shaped words; or accepting the limit and teaching the verb in
`instructions.md`. **D2 below is deliberately the smaller, certain fix; the
concept problem gets its own ADR once D1 is measured.**

## D1 — an exact match must win

When the query string (normalized) equals a node's id, label, or
`metadata.identifier`, that node must rank first. This is not a heuristic — it
is the one case where we have certainty, and it is currently worth nothing.

Design constraints:

- **Deterministic.** No fuzzy distance, no embeddings — a string equality test
  and a score floor that provably exceeds any accumulated IDF sum, or an
  explicit pre-ranking tier ahead of lexical scoring.
- **Must not disturb existing ranking.** The judged floors (16.5 / 13.0) are
  the gate; a change that fixes exact match and drops a judged mark is a loss.
- **Ties are the real bug.** Seven candidates at 1.51 means the scorer had no
  opinion. Whatever lands should also give exact/prefix matches a deterministic
  order rather than leaving equal scores to sort order — that is ADR 5's lesson
  applied to ranking.

## Gates

- The reproduction above must return the `port` node first, on this repo and on
  the boundary fixture.
- Judged scoreboard may not move DOWN (16.5 / 13.0).
- The boundary golden gate's retrieval assertions (currently name-based, e.g.
  "git process exec") must still pass, and the probes that were DROPPED for
  ranking noise — the exact-host query — should be re-added once they pass,
  because a gate that was removed to hide a defect must come back when the
  defect is fixed.
- Query latency must not regress (ADR 12 Ceiling 2 already has it at 3,516ms on
  linux; this must not make it worse).

## Note for whoever picks this up

The boundary-fixture agent deliberately did NOT gate the rank-7 result. That
was the right call — pinning a wrong answer makes it permanent — and it is why
this ADR exists instead of a quietly-passing test.
