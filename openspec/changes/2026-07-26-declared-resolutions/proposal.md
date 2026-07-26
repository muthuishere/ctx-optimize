# ADR — declared resolutions: let the repo settle what the extractor can't

Status: **IMPLEMENTED (first cut)** — 2026-07-26. Owner chose
`external_methods` only, hand-written. `receiver_types` / `scoped` / a
`resolve` verb are NOT built — see Deferred.

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

## Proposed shape (as drafted — only the first key shipped)

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

## The honest cost — and why it does not apply to what shipped

For the RESOLVING keys (`receiver_types`, `scoped`): this is user-supplied
ground truth, and a wrong declaration produces a confidently wrong edge —
precisely what the receiver gate was built to stop. The binary cannot
type-check a type claim. Guards those keys would need:

1. Config-resolved edges carry provenance (`metadata.resolved_by: "declared"`),
   so every one is auditable and `nodes/edges --where` can list them.
2. They must NOT be indistinguishable from parsed facts. Minimum:
   `INFERRED` confidence, never `EXTRACTED`.

**This cost is exactly why the owner scoped the first cut to
`external_methods`**, which resolves nothing and therefore needs neither guard.
Guard 3 below applies to every key and did ship:

3. A declaration that matches **nothing** is reported, not ignored — a rotted
   entry (renamed type) must be visible, or the file decays into lies.

## What shipped

`.ctxoptimize/resolutions.json`, one key:

```json
{ "external_methods": ["Error", "String", "Close"] }
```

Semantics, chosen so the safety claim is structural rather than a promise:

- Checked **only on the abstention path** — after `pick` has already declined.
  So it retires a shortlist and can never delete a resolved edge:
  `MyErr.Error()`, which names its own receiver, still resolves
  (`TestDeclarationNeverRemovesAResolvedEdge`).
- It **never creates an edge**. There is no code path from a declaration to an
  emitted edge at all (`TestExternalMethodRetiresTheShortlist` asserts the
  INFERRED absence, not just the AMBIGUOUS one).
- Applies only to **receiver-qualified** calls. An unqualified `Error()` is a
  plain function call and may well be yours
  (`TestDeclarationDoesNotTouchUnqualifiedCalls`).
- **Malformed is a hard error, never a warning** — bad JSON, an unknown key, a
  qualified name, parens, an empty entry. A silently ignored declaration is the
  worst outcome, because the author believes it is in force. The unknown-key
  error names the keys that ARE supported, so a future `receiver_types` line
  fails loudly today instead of doing nothing.
- A declared name matching **no** call site is reported on every gather. A file
  nobody prunes decays into confident-looking claims about code that moved on.

### Measured on this repo, same commit, one declared line (`"Error"`)

| | no declaration | declared |
|---|---:|---:|
| INFERRED `calls` | 2,455 | **2,455** |
| AMBIGUOUS `unresolved-receiver` | 239 | **141** |
| AMBIGUOUS `name-collision` | 1,202 | 1,202 |

98 maybes retired, nothing created, nothing else touched. `.ctxoptimize/resolutions.json`
is committed here — `Error` is genuinely external in this repo, since
`AmbiguousError.Error` is reached by interface dispatch (`%v`, `err.Error()`),
never by an explicit call on our type.

`init`/`up` scaffold an inert `resolutions.json.sample` (renamed to activate),
because nobody uses a declaration file they never learn exists.

## Deferred, deliberately

`receiver_types` and `scoped` — the keys that *resolve* rather than retire — are
where the value is and where the risk is: the binary cannot type-check a type
claim, so a wrong line becomes a confidently wrong edge, which is exactly what
`2026-07-25-method-call-resolution` was built to prevent. Shipping the harmless
key first means the file, the loader, the validation and the staleness report
all exist and are proven before that trade is taken. Reopen with its own ADR.

A `resolve` verb that proposes entries from the current shortlist was also
declined: approving a proposal is one keystroke, which makes it the fastest path
to a wrong declaration.

## Not claimed

- The numbers above are shortlist sizes and one line's effect. No claim that
  declarations are generally easy to get right — only that THIS key cannot be
  wrong in a way that corrupts the graph.
- No claim that a repo will maintain this file. The staleness report exists
  because it probably won't.
- Nothing here improves resolution. It removes noise the extractor was honest
  about; the 1,202 name-collisions are untouched.
