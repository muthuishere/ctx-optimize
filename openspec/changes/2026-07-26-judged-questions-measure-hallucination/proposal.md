# ADR — four judged questions were scoring hallucinations

Status: **DRAFT** — diagnosis complete and independently reproduced; the fix
needs the owner's call because it changes what the quality net measures.

## What was thought

`proof/golden/SCOREBOARD.md` recorded **newtonsoft 16.5/20 at commit 72900ca
(2026-07-16)**. The floor in `testdata/questions/newtonsoft.json` is **13.0**.
The floor note explains the gap by claiming 16.5 was *never measured* — copied
from linux-block in `7588bd8` and never met, leaving `golden.yml` RED from
2026-07-20 until it was "corrected" to the measured 12.5.

That claim is **false**. Running the judged tier at `72900ca` today scores
**newtonsoft 16.5/20**, with N01/N09/N13/N14 all at 1.0.

## What is actually true

Two bisects (`git bisect run` on "score ≥ threshold", with
`CTX_OPTIMIZE_GRAMMARS` pointed at an empty dir — verified not a variable, since
HEAD scores 13.0 with and without packs):

| commit | date | score | lost |
|---|---|---:|---|
| `72900ca` baseline | 07-16 | **16.5** | — |
| **`5c36287`** verify + ambiguity-aware resolution | 07-17 | **14.5** | N13, N14 |
| **`d041a2a`** definitions beat import stubs | 07-24 | **12.5** | N01, N09 |
| `96c6a87` question stopwords | 07-25 | 13.0 | +N16 |

So the score really did regress 3.5 points. **And restoring it would be wrong**,
because the lost points were being earned by the exact behaviours those two
commits removed on purpose.

### N13 was measuring a test method's blast radius

`affected "JsonConvert.DeserializeObject"`, `min_impacts: 5`.

The production symbol is labelled `Newtonsoft.JsonConvert.DeserializeObject`
(namespace-qualified), so the question's argument is **not an exact label**. It
falls to fuzzy, ties among five `Tests.JsonConvertTest.DeserializeObject*`
methods, and today refuses:

```
"JsonConvert.DeserializeObject" has no exact match and several near names score
alike — refusing to guess; pick one: Tests.JsonConvertTest.DeserializeObject …
```

Before `5c36287`, fuzzy silently took `matches[0]`. The question passed by
measuring **the impact of a test method, not the production API** — a wrong
answer scoring 1.0. That silent `matches[0]` is precisely what
`openspec/changes/2026-07-16-verify-verb` was written to kill (it names
graphify's version of the same behaviour as the anti-pattern).

Asked with the exact label it still fails: `affected` returns 2 impacts (both
`contains` ancestors), because `DeserializeObject` is declared many times across
the test suite, so its call edges are AMBIGUOUS and excluded from traversal by
design (ADR `2026-07-25-abstain-out-loud`).

### N01 was satisfied by an import stub

`query "deserialize json string object"`, `expect_any: ["DeserializeObject"]`,
k=5. `d041a2a` downranked `module://` stubs ×0.25 and test files ×0.5, after a
measured `card url_for` failure where a stub with no signature and no file:line
outranked the real definition. `expect_any` is a **substring** check, so a
stub-shaped hit satisfied it; demote the stub and it leaves the top 5.

## The defect

**A judged suite whose score falls when hallucination is removed is measuring the
wrong thing.** Three of these four questions only pass if the tool guesses among
fuzzy ties or ranks an import stub as an answer. The floor correction to 12.5 was
right in substance and wrong in its stated reason.

## Options

**A — fix the questions to ask correctly, re-derive the floor.**
N13/N14 get exact labels; N01's `expect_any` gets tightened so a stub cannot
satisfy it. Then whatever the suite scores is the honest number and the floor is
set from it. *Cost:* N13 will still fail on `min_impacts` (2 vs 5) because the
call edges are legitimately AMBIGUOUS — so this exposes a second, real gap rather
than closing one, which is arguably the point.

**B — keep the questions, accept 13.0, and document why 16.5 is unreachable.**
Cheapest. Leaves four questions permanently red for reasons unrelated to answer
quality, which erodes the suite's signal — a red mark nobody expects to fix gets
ignored.

**C — split the suite: "answers" vs "abstentions".**
Some questions should assert a REFUSAL (the grounding probes in `5c36287`
already do this). N13 as written is a good abstention test and a bad answer test.
*Cost:* a bigger change to the harness and the scoreboard format.

My reading: **A**, plus fixing `SCOREBOARD.md` and the floor note so neither
records a claim that is not true. C is the better long-term shape and should not
be attempted in the same change.

## What must be corrected regardless of option

1. `proof/golden/SCOREBOARD.md` recorded 16.5 as an **achieved** score for a run
   that achieved it only via silent fuzzy matches. It is not wrong about the
   number; it is wrong about what the number meant.
2. The floor note asserts 16.5 "was never measured". It was. The note should say
   what is true: the score was achievable, it was achieved by behaviour we
   removed deliberately, and it is not a target.

## Not claimed

- N09 and N14 were not traced to a specific mechanism, only to their commits
  (`d041a2a` and `5c36287` respectively). The N01 and N13 mechanisms were
  reproduced by hand; the other two are inferred from sharing a commit and a
  shape, which is weaker evidence and should be confirmed before acting.
- No claim that the current 13.0 is the *right* number. It is the honest number
  for the questions as they stand, and three of those questions are wrong.
- linux-block is unaffected throughout (16.5/20, floor 16.5), which is a useful
  control: whatever happened is specific to this corpus's questions, not to the
  engine generally.
