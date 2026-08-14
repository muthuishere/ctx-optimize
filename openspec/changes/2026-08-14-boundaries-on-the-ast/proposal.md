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

## D4 — incremental, made real

Separate from the walk fix, and the owner asked for it directly. Pending the
mechanical findings of the in-flight audit, the target is: a one-file change
should cost one file of work, not a whole-tree re-extract. Today lever 1 is a
whole-tree stat signature — it answers "did anything change", not "what
changed". A per-file cache keyed on **content hash** (the store already keeps a
content-hash manifest) would let an unchanged file's nodes/edges be reused.
This is drafted as a direction, not a decision; the numbers land first.

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
