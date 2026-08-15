---
title: What we do not claim
description: "The claims we killed, and why they are not coming back. Each was retired because a measurement said so."
---

Each of these was on this site or in our own docs at some point. Each was retired because a
measurement said so — not because someone objected.

## ✗ "Saves you tokens", universally

The **universal** claim is dead; a **harness-specific** one survives, and we publish both
because we measured both.

**Where it does not hold — frontier harnesses.** On Claude Code the store moved agent token
usage by **−0.2%**, and on Codex by **+3.0%**: parity at equal quality. Agent fixed costs
(system prompt, reasoning, the answer itself) dominate that bill and do not shrink with a
better retrieval tool. Record in
[CRITIQUE.md](https://github.com/muthuishere/ctx-optimize/blob/main/docs/CRITIQUE.md).

**Where it does hold — a thin small-model loop.** `gpt-4o-mini` via OpenRouter, tokens and
cost from OpenRouter's own accounting rather than estimated: **15,078 → 9,659 tokens
(−36%)**, **$0.0024 → $0.0016 (−31%)**, **11 → 4 steps**. Sample, stated inline because it
matters: **4 questions, one run, one corpus, one model** — much smaller than the correctness
study (12 questions, 3 runs, n = 36 per arm). Summary:
[SUMMARY-gorilla-mux.md](https://github.com/muthuishere/ctx-optimize/blob/main/proof/agent/results/SUMMARY-gorilla-mux.md).

The mechanism is why both are true: in a thin loop retrieval *is* most of the context, so
cutting steps cuts tokens with them; on a frontier harness the agent's own overhead is the
bill. So the sentence we will say is **"36% fewer tokens on a small-model harness, parity on
Claude Code and Codex"** — never "saves you tokens" with no harness named. What moves on
every harness we measured is **tool calls per run — 15.0 against 42.7**.

## ✗ "The fastest code graph"

Retracted. CodeGraph answers kernel queries in **0.79 s to our 3.70 s**, and ripgrep in
**1.59 s**. We are the slowest tool in our own headline table. Speed is their column;
answering is ours.

## ✗ Any un-run head-to-head

potpie and Serena have no measured row here because we have not run them. **A cell we did
not measure stays empty rather than getting a plausible number.**

## ✗ "Fastest adapters"

Never measured, so never claimed. What the native sources do is a *capability*: nine
connectors, an env-var name on argv, and a secret value that is never read into config,
stored, or printed.

## Known gaps in the current release

- **`scope` on a consumed port is always `external`.** The internal/external join compares
  hostnames against route paths and can never match. It is documented in the repo rather
  than quietly left to look like a computation.
- **Concept phrasing does not retrieve.** "What does it shell out to" returns nothing from
  lexical search; the routing table sends that question to `boundaries` instead. We will not
  fix it with embeddings — that would cost the determinism the product rests on.
- **A recorded recall can be wrong in either direction.** One shipped rule records 0.00
  recall while demonstrably working, because its ground-truth sweep counted matches inside
  comments. Numbers get re-measured, not quietly rounded.

## Why publish this at all

Because the one thing we are asking you to believe — that a graph answers questions grep and
a faster graph cannot — is only checkable if we also show the questions we failed and the
columns we lose. The standing counter-weight to every value claim in this project lives in
the repo:
[VISION.md](https://github.com/muthuishere/ctx-optimize/blob/main/docs/VISION.md) and
[CRITIQUE.md](https://github.com/muthuishere/ctx-optimize/blob/main/docs/CRITIQUE.md).
