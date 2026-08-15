---
title: What we do not claim
description: "The claims we killed, and why they are not coming back. Each was retired because a measurement said so."
---

Each of these was on this site or in our own docs at some point. Each was retired because a
measurement said so — not because someone objected.

## ✗ "Saves you tokens"

Measured and killed. On frontier harnesses the store moved agent token usage by **−0.2% on
Claude Code and +3.0% on Codex** — parity at equal quality. Agent fixed costs (system
prompt, reasoning, the answer itself) do not shrink with a better tool. What *did* move in
the graded run is **tool calls per run — 15.0 against 42.7**.

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
