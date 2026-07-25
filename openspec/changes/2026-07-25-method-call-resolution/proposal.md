# ADR — method calls resolve by bare name, so a unique name is a false witness

Status: **DRAFT** — not implemented. Surfaced by the `report` verb (ADR
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

My reading: **A**, with the abstention routed through the shortlist mechanism
that already exists, and D remains the real answer for repos that want precision.
But A trades recall for precision and that trade must be measured before it
ships, not argued.

## Measurement plan (before any code)

1. Instrument `calleeName` to record whether a call site was receiver-qualified.
   Report the split repo-wide — currently unknown and load-bearing for A's cost.
2. Implement A behind a flag; diff the edge set against today's.
3. Run the judged tiers. **This is the gate**: L20 ("which tests cover
   bio_split") and N14 ("which tests exercise SerializeObject") are derived from
   call edges, so a recall drop shows up there. If either falls, A is not free
   and the trade needs the owner's call.
4. Corpus tier for volume, golden snapshots for shape.

## Not claimed

- The 85/331 split is one repo's numbers. Go-heavy code with `err.Error()`
  everywhere is close to the worst case; a Python or C# corpus may differ
  substantially, and Newtonsoft/linux are available to check.
- "Almost all wrong" for `AmbiguousError.Error` is a judgement from reading the
  call sites, not an exhaustive audit.
- No option here has been implemented or benchmarked. This ADR records a measured
  defect and the shape of the fix, nothing more.
