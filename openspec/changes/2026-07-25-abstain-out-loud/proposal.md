# ADR — abstain OUT LOUD: report what we could not resolve, never guess it

Status: **IMPLEMENTED** — 2026-07-25. Refines the AMBIGUOUS-emission line in
`docs/VISION.md` (see Reversal).

## Owner's motto (governing)

> **"We can say no instead of being wrong — that's the motto."**

Stated 2026-07-25, consistent with the earlier rule in the same session
("try only whatever possible; if there is a 1% chance wrong, no need").

## The problem

Call resolution has three outcomes (`internal/extract/code/code.go:330-345`):

| candidates for the callee name | today |
|---|---|
| exactly 1 (same file, else module-wide) | `calls` edge, INFERRED |
| **more than 1 — ambiguous** | **silently discarded** |
| 0 — external/stdlib | silently discarded |

`pick` returns `nil` for the last two and the caller just `continue`s. Nothing —
not `status`, not the gather summary, not `card` — ever says a call site went
unattributed. Measured surface of the loss, from `docs/VISION.md:284`: a 352-file
/ ~1,900-symbol spike found **2,487 reliable vs 7,718 guessed edges and 1,405
ambiguous sites**.

**Silence is not abstention.** An agent that asks "who calls X" and gets three
edges concludes there are three callers. It has no way to learn that four more
call sites named `X` existed and were dropped because the name was ambiguous. We
are not wrong in what we emit; we are misleading in what we omit — and the agent
cannot tell the difference, which is the part that matters.

## Owner's decision: emit AMBIGUOUS, and make the consumers honor it

I first proposed rejecting emission outright: an AMBIGUOUS edge is a
possibly-wrong fact wearing a label, and the label does not travel — `hubs`,
`affected`, `path` and `change-plan` would traverse it, and VISION.md:284 already
measured that failure (god-nodes polluted by `get`/`append`/`new`).

**The owner's answer resolved that objection rather than accepting it:** emit the
edge labelled AMBIGUOUS, exclude it from `change-plan` and the other traversal
verbs, and *check the top by grep*. That makes an AMBIGUOUS edge a **shortlist to
grep**, not a claim — and the label travels precisely because the consumers
filter it. Adopted.

The three objections and what answers each:

| objection | resolution |
|---|---|
| the label does not travel into blast radii | `analyze.WithoutAmbiguous` applied INSIDE `Affected`/`Hubs`/`ShortestPath`/`Explain`/`Card`, not at call sites — a new verb cannot forget it |
| a wrong edge costs more than a missing one | nothing enters any answer path; opt-in only via `edges --confidence AMBIGUOUS` |
| `verify` cannot check a maybe | it is not offered as a citation. `card` prints a COUNT plus the grep that settles it |

`internal/schema/schema.go:23` had declared the tier since the beginning and
nothing had ever produced one — `edges --confidence AMBIGUOUS` returned 0. So
this implements a contract the schema already promised.

## Decision (what shipped)

Emit the shortlist, filter it everywhere that answers, and say the count out loud.

1. **`shortlist()` in `internal/extract/code/code.go`** distinguishes the two
   outcomes `pick` used to conflate:
   - **>1 candidate** → the name IS defined in this repo, we cannot say which.
     Emit one `calls` edge per candidate, `Confidence: AMBIGUOUS`.
   - **0 candidates** → stdlib or a dependency. Nothing here to point at, so
     nothing is emitted. External is NOT ambiguity and must never inflate it
     (`TestUnknownCalleeEmitsNothing`).
   - **> `ambiguousCap` (4) candidates** → shortlist NOTHING. A name with 40
     definitions is better served by grep than by 40 maybes; this is the
     god-node pollution `VISION.md:284` measured.
   Ambiguity inside a single file never widens to the module.
2. **`analyze.WithoutAmbiguous` applied INSIDE** `Affected`, `Hubs`,
   `ShortestPath`, `Explain` and `Card` — not at their call sites, so a verb
   added later cannot forget it. `change-plan` inherits it through `Card` +
   `Affected`, and its own confidence tally no longer buckets an AMBIGUOUS edge
   as INFERRED (it would be reporting confidence for an edge the plan never used).
3. **`card` says no out loud** — `AmbiguousCallers` is counted BEFORE the filter
   drops the edges, and rendered as an abstention with the two commands that
   settle it: the candidate query and the grep. `called_by` itself stays exact.

Reachable on purpose via `edges --relation calls --confidence AMBIGUOUS [--to ID]`,
where the caller has asked for maybes and knows it.

Explicitly NOT in scope: gather-time / `status` counters, and per-symbol
persisted ambiguity. The card covers the case an agent actually hits; a
repo-wide counter needs a store-shape change and gets its own ADR rather than
being smuggled in.

## Refinement — `docs/VISION.md:287-289`

That passage read:

> emit EXTRACTED / INFERRED / AMBIGUOUS and let the agent weigh them
> (graphify-parity behaviour).

"Let the agent weigh them" was too loose: nothing stopped a maybe entering a
blast radius, and an agent cannot weigh what it cannot see is a guess. VISION now
carries the refinement — AMBIGUOUS is a shortlist to grep, filtered from every
traversal verb by default — and points at this ADR. The schema's `AMBIGUOUS`
constant was already public contract on the `--json` door and is unchanged.

## Doc drift is part of the fix, not an afterthought

"Ambiguous" is described in **7 hand-maintained surfaces** today (`README.md`,
`CLAUDE.md`, `docs/VISION.md`, `docs/adapters.md`, and three skill references),
across ~2,900 lines of behaviour docs in 13 files plus 6 site pages. Two stale
claims were found in this same session — `extending.md`'s `packConfig` field list
and the skill's silence on redaction — so restating the rule in seven places is
how it rots.

Therefore: **one canonical statement + a drift guard.** A test asserts that no
doc surface contains a contradicting claim (e.g. any file still saying ambiguous
names are dropped *without* saying they are reported). Same shape as cljgo's
`gen-editors --check`: cheap, and it fails on the next edit that drifts.

The generated store wiki (`~/ctxoptimize/<repo>/wiki/`) needs nothing — it is
derived from nodes+edges on every `add`, so it cannot go stale by construction.

## How we will know it worked

- AMBIGUOUS edges emitted on a real repo, with EXTRACTED/INFERRED untouched.
- **Zero new nodes**, and no confident edge converted to a maybe — asserted.
- Judged tiers must not move (16.5 / 13.0) and every golden snapshot must pass.
- No traversal verb exposes a maybe; `card` reports the count with a grep.
- The drift guard fails when a doc reverts to the old claim.

## Not claimed

- This does not make the call graph more complete. It makes its incompleteness
  legible. The completeness fix is a precise producer (LSP/SCIP), which
  `VISION.md:290-293` already names as the real differentiator.
- The 1,405-ambiguous-site figure is from a 352-file Rust spike on citenexus, not
  this codebase. Our own number is 1,096 edges at cap 4 (measured below).
- A shortlist is not an answer. `Run` with 65 unattributed callers still needs a
  grep — what changed is that the agent now KNOWS to run it.


---

## Measured (2026-07-25)

On this repo, sweeping `ambiguousCap` 0 → 100:

| cap | AMBIGUOUS | INFERRED | EXTRACTED |
|---|---|---|---|
| 0 (pre-change) | 0 | 2561 | 2956 |
| 2 | 496 | 2561 | 2956 |
| 3 | 664 | 2561 | 2956 |
| **4 (shipped)** | **1096** | 2561 | 2956 |
| 6 | 1480 | 2561 | 2956 |
| 10 | 1585 | 2561 | 2956 |
| 100 | 2102 | 2561 | 2956 |

**INFERRED and EXTRACTED are identical at every cap.** The change is purely
additive — no confident edge became a maybe and no node was created. Pinned by
`TestAmbiguousShortlistIsPurelyAdditive`.

`cap = 4` because a shortlist is only worth having if you would actually grep it;
uncapped makes 2,102 of 7,619 edges (28%) maybes, and a name with 40 definitions
is better served by grep alone than by 40 candidates.

Verified unchanged, i.e. the filter holds: `affected LangForFile` reports the
same 8 impacts as before; judged tiers stayed at **linux-block 16.5 / newtonsoft
13.0**; every golden snapshot passes; `task ci` green.

Real example — `Run` is defined in three packages, so 65 call sites cannot be
attributed to any one of them:

```
called by (3):
  …
unattributed callers: 65 — the name is defined more than once, so these call sites were NOT guessed.
  candidates: ctx-optimize edges --relation calls --confidence AMBIGUOUS --to internal/adapterscli/adapterscli.go::Run
  confirm:    grep -rn '\bRun\b' .
```

## Doc drift guard shipped

`internal/skills/docdrift_test.go` pins the claims that would mislead an agent if
they went stale: no surface may say ambiguous names are simply "dropped"; the
skill must explain `unattributed callers`; `activation-routing.xml` must carry the
routing note; and `docs/VISION.md` must not revert to green-lighting unfiltered
emission. It also pins the 0.9.2 redaction guidance.

It earned its keep immediately — it caught a stale
`internal/extract/code/code.go:333` comment ("Ambiguous and unknown names are
dropped, never guessed") that this very change had made false and that I had
missed.
