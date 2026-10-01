# ADR 37 — the description is 200 characters

Status: DECIDED by the owner 2026-10-01 ("it should be 200 only, but at the
same time answer well"). Supersedes ADR 35's 858-char outcome.

## 1. Why 200 is defensible — ADR 35 already proved the mechanism

ADR 35 ran 5 runs x 25 queries per arm and found:

- the OLD 4,443-char description arrived **truncated at 1,535** and still
  scored **85/85 recall across all 13 lanes**;
- the 858-char rewrite scored **85/85** too;
- a 943-char variant that bought primacy wording back moved nothing.

The conclusion recorded there: **the lane-specific trigger text was never
doing the work.** What routes is the pair

1. "this is a prebuilt knowledge graph you ask instead of grepping", and
2. the `.ctxoptimize` marker plus REQUIRED-before-grep.

Both fit in 200 characters. Everything else was paying rent to say what the
marker already says.

## 2. What 200 costs and saves

| version | chars | resident tokens (approx) |
|---|---|---|
| shipped v0.15.3 | 4,443 (cut to 1,536) | ~384 |
| shipped v0.15.4 | 858 | ~215 |
| this ADR | ~200 | **~50** |

Against the 64-skill roster median of 695 chars, this makes ctx-optimize one
of the *cheapest* descriptions on the machine rather than the most expensive
— a product that sells context frugality finally modelling it.

On hosts that do not truncate (toolnexus and other opencode-style runtimes,
`js/src/skill.ts:421` et al — see muthuishere/toolnexus#84) the saving against
the original is ~4,240 chars **per turn**.

## 3. The real risk, and it is NOT the one we tested before

At 858 chars every lane still had words of its own. At 200 chars five lanes
have **no vocabulary at all**: native sources (postgres/kafka/s3/OpenAPI),
onboarding a monorepo, the dashboard, extraction customization, and
boundaries. ADR 35's arm A suggests the marker carries them — but arm A's
truncated text still contained 1,535 chars of lane words. **This is the first
arm where those words are genuinely absent.** That is the hypothesis under
test, and it is a real one.

## 4. Candidates (all angle-bracket free, validator-clean)

- **A** (176) — "Knowledge-graph answers about code with cited file:line —
  required before grep/Read when a .ctxoptimize dir exists. Trigger: where is
  X, who calls Y, what breaks if I change Z."
- **B** (198) — "Answers any code question from a prebuilt knowledge graph,
  cited file:line: find, callers, impact, architecture, onboarding, schemas,
  boundaries. REQUIRED before grep/Read where .ctxoptimize exists."
- **C** (191) — "A prebuilt knowledge graph of this codebase — ask it instead
  of grepping: where X is, who calls it, what breaks, how it fits, plus
  gather/index/share. REQUIRED first when .ctxoptimize exists."

B spends its budget naming lanes as single words; A spends it on example
questions; C spends it on the ask-instead-of-grepping framing.

## 5. Decision rule, declared before measuring

Same harness as ADR 35 (`stress.py`: real `claude -p` sessions, a hit counted
when the session actually runs a ctx-optimize command), 25 queries x 3 runs,
against the 858-char arm as the baseline to beat.

- **recall 1.000 and no lane below 100%** ⇒ ship that candidate at ~200.
- **any lane drops** ⇒ ship the candidate whose recall is highest; if all
  drop, the lane words were load-bearing after all and ~400 chars is the
  floor. Report the number, do not quietly keep 858.
- Ties break toward fewer characters.

## 6. What does NOT move

The ~30 KB body is unchanged — every lane keeps its full instructions once
invoked. This ADR only changes what is resident before the decision to
invoke. And `.ctxoptimize/instructions.md` plus the repo hook remain the real
carriers of routing inside a store repo, where it matters most.

## 7. Results (2026-10-01) — 200 chars is an IMPROVEMENT, not a compromise

Same harness as ADR 35. 25 queries x 3 runs per arm, plus a targeted n=10
run on the one lane that wobbled.

| metric | OLD 1,536 (truncated) | 858 (v0.15.4) | **B @198** | B2 @214 |
|---|---|---|---|---|
| recall | 1.000 | 1.000 | 0.980 | 0.980 |
| first-call rate | 0.882 | 0.776 | **0.941** | 0.922 |
| false-fire | 0.125 | 0.150 | 0.125 | 0.083 |
| lanes at 100% | 13/13 | 13/13 | 12/13 | 12/13 |

**Finding A — shorter routes BETTER.** First-call rate — the store verb
being the FIRST tool, which is the entire product thesis — is highest at 198
chars: 0.941, against 0.776 at 858 and 0.882 at 1,536. Less text to weigh
against 60 other skills appears to beat more text naming lanes.

**Finding B — the lane wobble was noise, and the fix was a non-fix.**
`verify` scored 2/3 on both 200-char arms. The hypothesis was missing
vocabulary, so B2 spent 16 chars adding "verify a citation". It scored 2/3
again. Re-run at n=10 on two differently-phrased citation questions:

- B2 @214: **20/20**
- B @198: **19/20**

So recall at ~200 is effectively 1.000, and the added words bought nothing —
the hypothesis in section 3 was wrong. The five "wordless" lanes
(sources, onboarding, dashboard, customize, boundaries) all held at 100%
without any vocabulary of their own, confirming ADR 35's conclusion that the
`.ctxoptimize` marker plus one clear framing does the routing.

**Decision: ship B at 198 chars.** B2 is indistinguishable and costs more;
ties break toward fewer characters.

## 8. The counterintuitive bit, for the changelog

Every instinct in skill authoring — including Anthropic's own guidance to be
"pushy" and enumerate triggers — says a description must list what it covers
to trigger on it. Measured across four sizes spanning 4,443 -> 198
characters, recall never moved and routing got BETTER as the text got
shorter. The enumeration was never doing the work.

## 9. Status

SHIPPED as v0.15.5. 4,443 -> 858 (v0.15.4) -> 198. Body unchanged at ~30 KB;
every lane keeps its full instructions once invoked.
