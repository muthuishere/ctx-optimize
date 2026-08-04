# ADR — method calls resolve by bare name, so a unique name is a false witness

Status: **IMPLEMENTED** — 2026-07-26. Surfaced by the `report` verb (ADR
`2026-07-25-report-verb`).

## The defect

`calleeName` (`internal/extract/code/code.go:571`) returns the **last** name node
of the callee expression and discards everything before it:

```go
// `s.Merge(a)` is a call to Merge, not to s; `self.bar()` is bar, not self.
```

That is right for identifying *which name* is called and wrong for identifying
*whose*. Resolution then keys on the unqualified label
(`byName[d.label]`, `code.go:337`), so `err.Error()` and `Error()` are
indistinguishable — and if exactly one declaration in the repo has the bare label
`Error`, every `err.Error()` in the codebase resolves to it with **INFERRED**
confidence.

**This is not ambiguity.** ADR `2026-07-25-abstain-out-loud` handles the case
where a name has several candidates. Here there is exactly one candidate, so the
shortlist never fires and we emit a confident edge. A unique name is being
treated as evidence that the call targets it, and for methods that inference does
not hold: the receiver's type is what decides, and we never looked at it.

The root cause is a blind spot, not a bug in the matcher. **The graph contains
only OUR declarations.** It cannot see `error`, `strings.Builder`, `io.Closer` or
any dependency type. So a bare-name method match silently assumes the receiver is
ours — which for `Error`, `String`, `Close`, `Read`, `Write`, `Len` is usually
false.

## Measured (this repo, 2026-07-25)

| | |
|---|---:|
| INFERRED `calls` edges | 2,596 |
| …targeting a **method** (only reachable by bare-name match) | **331 (12%)** |
| …of those, **cross-package** | 215 (65%) |

Top method targets:

| target | edges | assessment |
|---|---:|---|
| `AmbiguousError.Error` | 85 | **almost all wrong** — `err.Error()` on stdlib errors |
| `Batch.Validate` | 31 | mostly right — `Batch` is the only type with `Validate` |
| `Store.Nodes` | 26 | mostly right |
| `Store.Edges` | 15 | mostly right |
| `Store.Merge` | 14 | mostly right |
| `Engine.Add` | 12 | plausible |

So **331 is the suspect population, not the error count**. Where the name is
genuinely unique to one of our types (`Batch.Validate`), the edge is correct and
useful. Where the name is a universal method that stdlib and dependency types
also implement (`Error`), it is systematically wrong. `AmbiguousError.Error`
alone is 85 edges — 3% of all INFERRED calls and 26% of the method-targeted ones.

An honest lower bound on the damage: **≈85 known-false edges**. An honest upper
bound: 331. The gap is unmeasured and measuring it needs a ground truth we do not
have.

## Why it matters more than the raw number

These edges are INFERRED, so they flow into `affected`, `change-plan`, `hubs` and
`Communities` — everything AMBIGUOUS edges are excluded from. A blast radius for
`AmbiguousError.Error` currently claims 85 callers that mostly do not call it.
That is the precise failure mode the abstain-out-loud ADR exists to prevent,
arriving through a path that ADR does not cover, because the matcher was
*confident*.

## Options

**A — capture the receiver, resolve what we can, abstain otherwise.**
The receiver IS in the AST; `calleeName` just drops it. With `x` in hand:
package-qualified calls (`store.Open`) resolve exactly; a receiver whose type is
declared in the same file resolves; anything else is unresolvable and we
**abstain** (drop, or shortlist under the existing AMBIGUOUS mechanism).
*Cost:* removes edges — including correct ones like `Batch.Validate` when the
receiver's type cannot be tied locally. Precision up, recall down.

**B — same-file / same-type preference, keep the rest.**
Cheaper, no abstention. Does not fix `err.Error()` at all when there is no local
competitor.

**C — universal-method denylist** (`Error`, `String`, `Close`, `Read`, `Write`,
`Len`, `Next`, `Size`). Kills the worst offenders for ~10 lines.
*But it is a guess dressed as a rule* — a repo whose own `Error` method is
genuinely called loses real edges, silently. Rejected on the motto unless
measured to beat A.

**D — a precise producer (LSP/SCIP).** `docs/VISION.md:290-293` already names
this as the real differentiator: exact references and call hierarchy, same emit
schema behind it. This defect is a concrete argument for that roadmap item.
*Cost:* per-language toolchains and a build environment — the thing we currently
do not require.

## Decision — A, with an explicit list of the ties we accept

`calleeName` now returns `(receiver, callee)` instead of throwing the receiver
away, and a **method** candidate (`d.owner != ""`) is attributed only when the
receiver is actually tied. `receiverTies` (`internal/extract/code/code.go`)
accepts exactly four, and nothing else:

| tie | example | why it is not a guess |
|---|---|---|
| callee is a free function | `Open()`, `store.Open()` | no receiver to check; gate does not apply |
| receiver == owner | `Batch.Validate()`, a Python classmethod | the receiver names the type |
| unqualified / `self` / `this`, inside the owner | `self.helper()` in `Engine` | the enclosing declaration IS the receiver |
| owner type named in the SAME declaration | `e := &src.Engine{}; e.Charge()` | the type is written in the calling scope, and no other declaration repo-wide bears the method name |

Everything else — `err.Error()` in a function that never names an error type of
ours — is **abstained on**, routed through the AMBIGUOUS shortlist that already
existed. Nothing is dropped: the edge survives as a maybe, filtered out of every
traversal by `WithoutAmbiguous`, visible via `edges --confidence AMBIGUOUS`.

**B** was rejected: it does not fix `err.Error()` at all. **C** was rejected on
the motto — a denylist of "universal" method names is a guess about other
people's repos. **D** (LSP/SCIP) remains the real answer and is unaffected by
this; `docs/VISION.md:290-293` still holds.

### The fourth tie is a convention, and that is stated on purpose

"Owner type named in the same declaration" uses `typeShaped` (CamelCase: not
lowercase, not SHOUTING) to decide which tokens to remember as evidence. That
is a naming convention, and conventions are normally rejected here. It is
admissible in this one position because of an asymmetry: the filter can only
**withhold** evidence, never manufacture it. A type it misses (a lowercase Go
type like `engine`) causes an abstention, never a wrong edge. Pinned by
`TestTypeShapedAdmitsOnlyCamelCase`.

The fourth tie is also load-bearing, not a nicety: without it the hermetic
golden net went red on exactly the case the judged tiers score —
`EngineTests.AddWorks -calls-> Engine.Add` fell to AMBIGUOUS. That is the
"which tests cover X" answer, and it is the reason A is shipped with this tie
rather than without it.

### Two abstentions, two greps

The card used to explain every abstention as *"the name is defined more than
once"*. For this defect that sentence is **false** — the name is defined
exactly once. So the reason is stamped on the edge
(`Metadata["ambiguous_reason"]`: `name-collision` | `unresolved-receiver`,
`internal/schema/schema.go`) and `card` prints the matching line and grep:

```
unattributed callers: 89 — call sites write `.Error(...)` on a receiver whose
type this store never established, so they were NOT attributed here.
  candidates: ctx-optimize edges --relation calls --confidence AMBIGUOUS --to …
  confirm:    grep -rn '\.Error(' .   # then check each receiver's type
```

SKILL.md and `references/activation-routing.xml` carry the same split, plus the
consequence an agent must not miss: **a blast radius for a method is a FLOOR,
not the full set.** Pinned by `TestUnresolvedReceiverExplainedToAgents`.

## Measured (this repo, 2026-07-26)

`calls` edges, gate off → gate on:

| | before | after |
|---|---:|---:|
| INFERRED | 2,626 | 2,401 |
| AMBIGUOUS | 1,138 | 1,364 |
| …of which `unresolved-receiver` | — | 226 |

**225 attributions reclassified as maybes; none lost.** Before the fourth tie
landed it was 272, so scope evidence recovers ~46 real edges. `AmbiguousError.Error`
went from 89 confident callers to 89 declared abstentions, and no method target
remains in the top INFERRED list.

Gate results:

- **Judged tiers unchanged: linux-block 16.5/20, newtonsoft 13.0/20** — byte-identical
  before and after (verified by re-running the tier against stashed changes).
  L20 and N14 were already 0.0; this change did not move them either way.
- Hermetic golden green; corpus tier green; perf ceilings green (gather of this
  repo 0.77s → 0.63s, i.e. within noise — `scopeNames` collection is free at
  this scale because `typeShaped` rejects most tokens).
- `receiverGate` is a package var, so the trade stays sweepable
  (`TestGateOffRestoresBareNameMatch` pins the old behaviour).

## Not claimed

- The 85/331 numbers above are one repo's. Go-heavy code with `err.Error()`
  everywhere is close to the worst case; the C# and C corpora exercise the gate
  differently and were checked only for regression, not for precision gain.
- **Precision is not measured, only plausibility.** We know 225 edges are no
  longer asserted; we do not have a ground truth saying how many of them were
  wrong. The claim is that we stopped asserting things we could not justify,
  not that we removed exactly the false ones.
- The fourth tie can still be wrong: a scope may name `Engine` for an unrelated
  reason while `e` is some other type with a same-named method — though that
  requires the method name to be repo-unique, or the name-collision path takes
  over first.
- Nothing here helps a method call whose receiver type is genuinely
  unknowable without type resolution. That is D's job, still unbuilt.
