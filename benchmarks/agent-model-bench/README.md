# agent-model-bench — how far does the store carry each model class?

One question, measured: **given the same prebuilt ctx-optimize store, the
same 8 questions, and an agent loop, how good is each model's answer —
and how much of a frontier agent can a small model replace?**
Run 2026-07-17. Judge: a Fable-5 session grading against withheld golden
keys (`questions-linux.json`; keys come from the golden linux-block
scoreboard the product already pins in CI).

## Arena

- **Repo**: linux kernel clone, full-tree store (~274k nodes / ~480k
  edges), read-only. Questions span the block layer (elevator hashing,
  bio splitting, disk+partition add, SED-OPAL, flush machinery,
  timeouts, user-page mapping, blk-mq submission).
- **Two harnesses, same store, same questions**:
  - **Claude Code sessions** (one fresh Ghostty session per model tier,
    `claude --model <tier>`, mission prompt = questions + evidence rules,
    self-timed per question).
  - **toolnexus** (the maintainer's agent runtime): `run --once` per
    question over OpenRouter, bundled skill loaded, plus the
    mandatory-protocol system prompt from the skill's
    `references/small-models.md`.
- Scoring: 10 per question — exact golden-key hit with correct
  citations = 10; right mechanism/file but canonical symbol missed =
  5–7; wrong subsystem or fabrication = 0–2.

## Results — Claude Code sessions (the model ladder)

| model | score /80 | avg s/question | tool calls (8 q) | notes |
|---|---|---|---|---|
| **fable 5** | **80** | 24.6 | 23 | every key + full mechanism chains |
| **sonnet 5** | **80** | 17.5 | 25 | every key; fastest frontier-quality run |
| **opus 4.8** | **79** | 19.0 | 22 | complete mechanisms; L02 described the split machinery without naming `bio_split` itself |
| **haiku 4.5** | **72** | 13.6 | 18 | 6 exact keys; L02 → `bvec_split_segs` (internal, not the canonical splitter), L08 → blk-mq machinery, missed `blk-timeout.c` |

## Results — toolnexus + OpenRouter (small non-Claude models)

| model | score /80 | avg s/question | notes |
|---|---|---|---|
| gpt-4o-mini + forced protocol | **54** | 9.4 | 4 exact keys; fails on query re-crafting when the first hit is subsystem noise (L06 → xen driver, L09 → ublk/gup) |
| gpt-4o-mini, skill only (no forced protocol) | 23* | 4.5 | *on the companion 8-question ctx-optimize-repo set: freelances into priors — 4 of 8 answers used zero tools |
| gemini-2.5-flash-lite | n/a | — | below floor: invents tool names / returns empty turns regardless of prompt |

## What it means

1. **The store is model-portable.** Sonnet matches Fable exactly (80/80)
   at 1.4x the speed; haiku — the cheapest Claude tier — lands 90% of
   frontier quality at half the wall time and 18 tool calls total.
   Claude Code discipline + the store beats raw model size: haiku
   in-session (72) outscored gpt-4o-mini in toolnexus (54) on identical
   questions.
2. **Small models need the protocol pinned in the system prompt** —
   without it they answer from priors (23/80). The measured-good prompt
   ships in the skill: `references/small-models.md`. With it, a
   $0.15/M-token model reaches ~70% of frontier quality at ~1/100th cost.
3. **What frontier still buys**: canonical-symbol precision (haiku's two
   near-misses), query re-crafting when the first hit is noise, and
   honest abstention under near-matches (in the companion trick-question
   set, gpt-4o-mini fabricated a nonexistent module even under protocol;
   every Claude tier declined to).

## Reproduce

1. Store: `ctx-optimize up` in a linux clone (or any pinned corpus), or
   reuse `~/ctxoptimize/linux`.
2. Claude lane: one fresh session per model, cwd = the repo, prompt =
   the 8 questions + "store-first, cite file:line, not-found after two
   empty queries, self-time each question".
3. Small-model lane: toolnexus `run --once` per question with the skill
   dir + the `references/small-models.md` system prompt verbatim (pass
   the API key via env, never argv).
4. Judge blind against `questions-linux.json` keys before reading any
   contestant's answer.
