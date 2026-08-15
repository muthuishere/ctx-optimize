# ADR 17 — the authoring law is documentation, not a gate

Status: DRAFT — owner review pending 2026-08-15. No product code until agreed.
Scope: the boundary rule LOADER (`internal/boundaries`) and the empty-result
rendering of `nodes`/`edges` (`internal/app`). No schema change, no new node
kind, no producer change.
Found by executing the shipped authoring spec end-to-end against the shipped
binary on a purpose-built fixture, rather than reading it.

## How this was found

A fixture reproducing the flagship case in the spec — a TypeScript HTTP
wrapper (`apiGet`/`apiPost`) called from 5 sites, plus Go env reads and process
spawns — then every command and every claim in
`references/boundaries-authoring.md` run verbatim.

**The good news first, because it is the headline:** an agent following the
spec CAN author a working rule. The wrapper rule was written from the documented
schema alone and worked first try — 5 sites captured, the dynamic one
(`apiGet("/orders/" + id)`) correctly emitted as AMBIGUOUS with a `${…}`
identifier rather than dropped. The id-override ladder works
(narrowing shipped `env-go` removed its ports). `boundaries verify` is honest,
distinguishing "no sites here" from "passed". Four of seven malformed rules
were rejected loudly, with good messages.

The findings below are where the spec's PROMISES exceed the binary's
BEHAVIOUR. Every one is a case where an author is told a guard exists.

## D1 — the loader accepts an unmeasured rule and calls it EXTRACTED

The spec's first line is "measured, or it does not ship". Measured:

| rule shape | doc says | binary does |
|---|---|---|
| no `verified` block | "invalid" | loads, exit 0 |
| `tier` omitted | "defaults EXTRACTED" | yes — highest confidence, zero evidence |
| `transport: "carrier.pigeon"` | closed vocabulary in practice | accepted; renders as its own group in `boundaries` |
| `metadata: {"direction": "provides"}` on a `consumes` rule | `direction` is reserved | accepted, and **overwrites the real direction** — the port is listed under PROVIDES |
| `match[].identifier: 0` | "group 0 is invalid" | accepted, silently reads as group 1 |
| bare `metadata: {"owner": "me"}` | rejected fail-closed | **correct — rejected loudly, naming the key** |

**Correcting two claims made in the first draft of this ADR**, because they
were measured through a path that masked the result: the namespaced-metadata
door IS enforced (a bare key fails the batch and says so), and the earlier
"accepted" reading came from a `config.env` rule whose metadata was dropped by
port DEDUP before ever reaching the door — first rule to mint an identifier
owns its metadata (`boundaries.go:451`), so a shipped rule had already created
the node. The reserved-key hole is real and is the sharper finding: it is not
that a reserved key is tolerated, it is that it **wins**.

The defaulting is the sharp end: **the one tier that asserts certainty is what
a rule gets for providing no evidence at all.** Every other confidence decision
in this product fails toward doubt.

The reserved-key overwrite is the one I would fix regardless of the rest: it
lets a rule invert CONSUMES/PROVIDES, which is the headline split of the
`boundaries` verb and an input to the scope join. A three-line guard (skip
reserved keys when applying `r.Metadata`, or reject at load) closes it.

Options for the tier half: (a) `verified` required for any rule with
`tier: EXTRACTED`, other tiers warn; (b) default an unmeasured rule to
AMBIGUOUS instead of EXTRACTED —
smallest diff, matches the fail-toward-doubt doctrine everywhere else;
(c) leave the loader open and delete the word "invalid" from the docs (already
done as an interim, since the docs were actively lying).

Note the compatibility cost of (a)/(b): the 16 shipped rules all carry
`verified`, so defaults are unaffected, but any user rule authored against the
current permissiveness changes tier under (b). That is arguably the point.

## D2 — an empty result is indistinguishable from a wrong question

`nodes --kind route` prints `(0 nodes)` and exits 0. There is no `route` kind —
served routes are `port` nodes with `direction=provides`. The SHIPPED SPEC
taught this exact command (fixed in docs today), which is how it surfaced: an
agent following our own instructions concludes "this repo serves no routes".

`--kind` is deliberately an open vocabulary (adapters mint kinds), so
rejection is wrong. Disclosure is not: when a filter matches nothing AND its
value appears on no node in the store, say so —

```
(0 nodes)  — no node in this store has kind "route";
            kinds present: file, function, method, port, section, …
```

This is the same failure shape as the flag that was silently ignored
(fixed in `1c2e9bf`) and the `--where` that dropped a condition: **a filter
that cannot match returns a plausible empty answer instead of an error.** Third
instance of the pattern this week, which argues for a standing rule rather than
three point fixes.

Applies equally to `edges --relation`, and to `--where key=value` where the key
is never present.

## D3 — the ground-truth denominator has no guidance, and it mis-tiers rules

Measured on the fixture: the documented grounding pattern for a wrapper rule
counts the wrapper's own two `function apiGet(` DEFINITIONS alongside its 5
call sites. Ground truth 7, matched 5, recall **0.71** — a rule with perfect
recall lands at INFERRED instead of EXTRACTED, from lines that are not
boundaries.

The same class, in the opposite direction, is already shipped: `process-py`
records recall **0.00** while the rule demonstrably works (verified on the
fixture: `subprocess.run(["git","status"])` emits a port). Its denominator
counts `subprocess.` in comments and strings.

So the recorded numbers are wrong in BOTH directions, and one of them is
published in a file we hold up as the model of rigour. Docs now warn; the
durable fix is either a grounding convention that excludes definitions/comments
or a `verified` re-measurement pass. **This is the same decision already open
as the `boundaries verify` floor** — the two should be resolved together.

## Kill criterion

If the answer to D1 is "the loader stays open", then the word "invalid" and the
claim of a fail-closed metadata door must stay deleted from the spec
permanently — a documented guard that does not exist is worse than an
acknowledged gap, because an author stops checking.

## What is NOT proposed

No change to the six AST shapes, the ladder, `boundaries verify`, or the
emitted schema. Those were exercised and behaved as documented.
