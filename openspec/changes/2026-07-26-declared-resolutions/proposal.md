# ADR — declared resolutions: let the repo settle what the extractor can't

Status: **DRAFT** — not implemented. Needs the owner's call on the declaration
shape (see Open questions) before any code.

## First, a correction

This was raised as "isn't that what community config means?" — it isn't.
**Community detection** (`internal/analyze/communities.go:58`) is the Louvain
clustering that finds *subsystems*; it is a read-side algorithm over the graph
and has nothing to do with resolving call sites. It deliberately runs on facts
only (`communities.go:67`) because including AMBIGUOUS edges produced fake
subsystems whose hub lists repeated one over-used name.

The idea underneath the question is real and separate: **let the repo declare
the resolutions it knows, in committed config, and have the binary honor them.**
That is the Tier-3 pattern `docs/CRITIQUE.md:66` already describes — LLM (or
human) proposes, binary disposes.

## The gap this closes

After `2026-07-25-abstain-out-loud` and `2026-07-25-method-call-resolution`, the
store holds 1,406 shortlisted call sites on this repo that it refuses to
attribute. `--include-ambiguous` (ADR `2026-07-26-include-ambiguous`) lets you
*look* at them; nothing lets you **settle** one permanently. Today the answer is
"grep it", every time, by every agent, forever. A resolution established once is
knowledge, and knowledge belongs in the repo.

## Measured — the declaration would be small

| reason | edges | distinct targets |
|---|---:|---:|
| `name-collision` | 1,174 | **87** |
| `unresolved-receiver` | 232 | **24** |

Top unresolved-receiver targets: `AmbiguousError.Error` (95), `Batch.Validate`
(22), `Store.Nodes` (21), `Store.Merge` (13), `Store.Edges` (11). The long tail
is short. A file of a few dozen declarations covers essentially all of it, which
is what makes this worth doing by hand at all — the same reason grammar and
route packs are hand-written JSON.

## Proposed shape (my recommendation)

One committed file, `.ctxoptimize/resolutions.json`, discovered like the other
packs, validated at gather time, failing **loudly** on a malformed entry:

```json
{
  "external_methods": ["Error", "String", "Close"],
  "receiver_types": { "st": "Store", "b": "Batch" },
  "scoped": { "internal/store/": { "s": "Store" } }
}
```

- **`external_methods`** — "this method name belongs to types we do NOT own;
  never attribute it." Kills the 95 `err.Error()` maybes outright.
- **`receiver_types`** — repo-wide "a receiver named `st` is a `Store`". Turns
  maybes into resolved edges. This is where the leverage is.
- **`scoped`** — the same, narrowed by path prefix, for names that mean
  different things in different packages (`s` is the common case).

### Why JSON, not markdown

The question mentioned "custom markdown or config". Markdown is the wrong
container for this: a mis-typed heading in markdown silently yields *nothing*,
and silent partial success is the failure mode this repo is most careful about.
Every existing extension point — grammar packs, route packs, manifest packs — is
drop-in JSON that is schema-validated and fails loudly, and this must behave the
same way. Markdown stays what it is here: the generated wiki, for humans to read.

### Why this is not the denylist I already rejected

ADR `2026-07-25-method-call-resolution` rejected option **C**, a built-in
denylist of "universal" method names (`Error`, `String`, `Close`), as "a guess
dressed as a rule". `external_methods` is the same list — but shipped by *us* it
is a guess about strangers' code, whereas declared by the repo owner it is an
assertion about their own. That distinction is the whole ADR: we do not guess;
we accept declarations, and we record who declared them.

## The honest cost, stated up front

**This is user-supplied ground truth, and a wrong declaration produces a
confidently wrong edge** — precisely what the receiver gate was built to stop.
The binary cannot type-check a type claim. So:

1. Config-resolved edges carry provenance (`metadata.resolved_by: "declared"`),
   so every one is auditable and `nodes/edges --where` can list them.
2. They must NOT be indistinguishable from parsed facts. Minimum:
   `INFERRED` confidence, never `EXTRACTED`.
3. A declaration that matches **nothing** is reported, not ignored — a rotted
   entry (renamed type) must be visible, or the file decays into lies.

## Open questions for the owner

1. **Which of the three keys ship first?** `external_methods` is trivial and
   only ever *removes* maybes, so it cannot introduce a wrong edge — the safe
   first cut. `receiver_types` is where the value is and where the risk is.
2. **New confidence tier, or INFERRED with provenance?** A `DECLARED` tier is
   more honest but the schema is a public door and widening it is permanent.
   I lean to INFERRED + `resolved_by`.
3. **Should `verify` check declarations?** It could confirm the declared type
   exists as a node; it can never confirm the receiver really has that type.
4. **Who writes the file?** Hand-written, or a `ctx-optimize resolve` verb that
   proposes entries from the current shortlist for a human to approve? The
   second is friendlier and is also how a wrong declaration gets in fastest.

## Not claimed

- No spike. The numbers above are shortlist sizes, not evidence that
  declarations are easy to get right.
- No claim that a repo will maintain this file. An unmaintained resolutions file
  is worse than none, and question 3 exists because of that.
