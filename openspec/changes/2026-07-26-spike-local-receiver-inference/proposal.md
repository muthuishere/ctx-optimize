# SPIKE — local receiver-type inference is a dead end (and found a real bug)

Status: **SPIKE COMPLETE, negative result.** No product code written. Recommends
NOT building the obvious next step, and records why.

## The question

After the receiver gate (`2026-07-25-method-call-resolution`), a method call is
attributed only on four exact ties. Everything else is shortlisted as AMBIGUOUS.
On the C# corpus that is most calls:

| Newtonsoft.Json@13.0.3 | edges |
|---|---:|
| INFERRED `calls` | 3,469 |
| **AMBIGUOUS `calls`** | **6,702 (66%)** |
| …targeting a method | 6,688 |
| name-collision / unresolved-receiver | 4,947 / 1,755 |

The obvious middle path between today's narrow ties and the LSP/SCIP option
(`docs/VISION.md:290-293`) is **local type inference**: read the receiver's type
out of the same file — `var x = new Foo()`, `Foo x`, `x := &Foo{}`, a parameter,
a Go method receiver. No language server, no toolchain, just more of the AST we
already have.

**Does it work?** Measured, rather than assumed.

## Result: it resolves nothing, and where it differs it is wrong

400 randomly sampled AMBIGUOUS **call sites** (seed 7, grouped by caller+method
so each site is counted once, not once per candidate):

| | sites | |
|---|---:|---:|
| receiver type not findable locally | 198 | 49.5% |
| **type found, matches NO candidate** | **123** | **30.8%** |
| call site not locatable in the caller's body | 70 | 17.5% |
| `this`/`self`/`base` (already tied today) | 5 | 1.2% |
| no source / no location | 4 | 1.0% |
| **type found AND matches a candidate** | **0** | **0.0%** |

**Zero.** Not "a modest gain" — zero. And 30.8% found a type that matches no
candidate, meaning a naive implementation would have produced a wrong edge or
dropped a real one.

## Why — three distinct blockers, from the actual examples

```
.ToArray()            on `ms`      inferred MemoryStream ; candidates: LinqBridge.Enumerable
.FirstOrDefault()     on `…Defs`   inferred List         ; candidates: LinqBridge.Enumerable
.Peek()               on `_stack`  inferred Stack        ; candidates: JsonReader, JsonWriter
.ReadAsInt32Async()   on `reader`  inferred JsonReader   ; candidates: HAVE_ASYNC_DISPOSABLE, JsonTextReader
.WriteEndObject()     on `writer`  inferred BsonWriter   ; candidates: XmlJsonWriter, JsonWriter, TraceJsonWriter
```

1. **Most receivers are BCL / dependency types.** `MemoryStream.ToArray`,
   `Stack<T>.Peek`, LINQ's `FirstOrDefault` — the real target is not in the graph
   and never will be, because the graph holds only OUR declarations. The correct
   answer for these is "not ours, abstain", which is *exactly what the gate
   already does*. Local inference adds nothing except a more confident-looking
   way to reach the same abstention.
2. **Inheritance and interfaces make exact-owner matching wrong in the OTHER
   direction.** A receiver typed `JsonReader` legitimately dispatches to
   `JsonTextReader.ReadAsInt32Async`; a `JsonWriter` to `XmlJsonWriter`. Matching
   the inferred type against the candidate's owner would *reject real edges*. You
   need the type hierarchy — which is precisely what a language server has and
   tree-sitter does not.
3. **17.5% could not even be located**, because the call is in a nested lambda,
   a continuation, or a body the declaration's line range does not cover cleanly.

Blocker 2 is the fatal one. It is not a gap in the implementation; it is the
absence of a type system. Any amount of regex sophistication runs into it.

## Conclusions

- **Do not build local receiver-type inference.** It cannot beat the four exact
  ties already shipped, and its disagreements are errors in both directions.
- **This validates the narrow gate.** The four ties were chosen because each is
  exact; the spike shows the fifth "obvious" tie has no exact form.
- **LSP/SCIP is confirmed as the only path to method-call precision**, now by
  measurement rather than by assertion. It is the one source that carries the
  type hierarchy blocker 2 requires.
- **66% abstention on C# is the honest cost of not having it.** Worth stating in
  the docs: a Go repo loses far less to the gate than a C# one, because C# is
  method- and inheritance-heavy.

## Found while spiking: a real extraction bug (issue #16)

Every run kept surfacing `candidates owned by ['Newtonsoft.HAVE_ASYNC_DISPOSABLE',
…]`. `HAVE_ASYNC_DISPOSABLE` is a **preprocessor symbol**, not a class:

```csharp
public abstract partial class JsonReader
#if HAVE_ASYNC_DISPOSABLE
    : IAsyncDisposable
#endif
{
```

`declName` scans past the class's own identifier and picks the `#if` symbol, so
**48 of `JsonReader`'s async members** are hung off a class that does not exist —
on one of the two pinned judged corpora. Filed as #16 with a proposed structural
fix (skip `preproc_*` node types rather than guess).

That is the spike's most valuable output: a negative result costs nothing to act
on, but the bug was invisible to every existing test.

## Not claimed

- 400 sites of 4,342 on one corpus, one seed. The direction is unambiguous (zero
  correct resolutions) but the exact percentages are a sample.
- The binding regexes are crude. A better implementation would find *more* types
  — which makes blockers 1 and 2 worse, not better, since both failure modes
  scale with how often a type IS found.
- Go was not measured separately. It has fewer AMBIGUOUS method calls to begin
  with, so the ceiling there is lower, not higher.
