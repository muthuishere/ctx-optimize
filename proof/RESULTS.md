# Proof-matrix results — 2026-07-12

The pre-committed composite test from `docs/CRITIQUE.md`, executed for real.
Corpus: `linux/block` (98 C files / ~73k lines, hostile terrain per the S1d
terrain law). 12 questions (3 locate / 5 mechanism / 2 impact / 2 conceptual),
ground truth derived by reading source directly (`questions.json`). Arms and
token semantics: see `README.md`. Raw per-run payloads: `results/` +
`results/metrics.json`; every answer judged against the key before tokens
counted.

## The kill-bar verdict

> composite <25% on hostile terrain → stop, or pivot to impact-analysis-only

| Harness | Model (its default) | Pairs | Fresh-token change A→B | Wall change | Quality A / B | Clears 25%? |
|---|---|---|---|---|---|---|
| Claude Code 2.1.207 | claude (session default) | 12/12 | **−0.2%** (parity) | −14% | 12/12 vs 11.5/12 | **NO** |
| Codex CLI 0.142.4 | ChatGPT default | 12/12 | **+3.0% cost** (parity) | ~parity | 11.5/12 vs 11.5/12 | **NO** |
| Devin CLI 2026.8.18 | SWE-1.6 | 6/12¹ | **−42.5%** (saves) | ~parity | 5/6 vs 6/6 | **YES** (partial n) |

¹ Devin daily quota exhausted mid-matrix; 6 complete pairs only. High
variance (q4: −82%, q6: +103%).

**Composite verdict: the 25% bar is NOT cleared on frontier harnesses.**
On Claude Code and Codex — agents with excellent built-in search — the store
arm costs the same or more tokens than plain grep at equal answer quality.
The pre-committed consequence applies: **stop selling "saves tokens" as the
universal headline, pivot the claim to where the data says the value is.**

## Model-tier addendum (2026-07-12, same day)

Owner asked: does a small/medium model change the picture? Re-ran the full
12-question A/B on Claude Code with `--model haiku` and `--model sonnet`, on a
**40× bigger corpus** (3,987 C/H files: block+mm+kernel+fs+drivers/block+
drivers/nvme+lib; store: 274,812 nodes / 3,989 wiki pages, gathered in
**5.5s**). Raw runs: `results-tiers/`. (Codex's mini model returns 400 on
ChatGPT accounts — `gpt-5.1-codex-mini` is API-only; measured, not assumed.)

| Tier | Corpus | Tokens A→B | Turns A→B | Quality A / B |
|---|---|---|---|---|
| Claude (default/big) | 98 files | −0.2% | ~flat | 12/12 vs 11.5/12 |
| Claude Sonnet | 3,987 files | +5.2% cost | **−13%** | 11.5/12 vs **12/12** |
| Claude Haiku | 3,987 files | −1.1% | **−29%** | 11/12 vs 11/12 |
| Codex (default) | 98 files | +3.0% | ~flat | 11.5/12 vs 11.5/12 |
| Devin (SWE-1.6) | 98 files | **−42.5%** | −24% steps | 5/6 vs **6/6** |

**The mechanism is step-cost economics, not model IQ.** The store cuts
*steps* at every tier (haiku −29%, devin −24%, sonnet −13%). Whether fewer
steps become fewer tokens depends on what the harness pays per step: Devin
reports zero cached tokens — every step re-pays the whole context — so
−24% steps became −42.5% tokens. Claude Code's prompt caching makes extra
steps nearly free, so even haiku lands at token parity. Corollary: the
token-savings claim is real exactly where caching is absent or billed
(API pay-per-step scaffolds, custom agents, CI loops), and it is a
step/latency claim where caching is present.

**Quality tilts toward the store arm as models get smaller-or-equal:** sonnet
arm B beat arm A (A listed a comment-only file as a real caller of
`blk_mq_run_hw_queue`; B explicitly flagged it "comment, not an actual
call"), devin arm B beat arm A (wrong gatekeeper). Frontier arm A never lost
on correctness; sub-frontier arm A did, twice.

## Where the value actually is (measured)

1. **Weak/cheap harnesses save big.** Devin's model burned 2.3M tokens /
   67 steps flailing on the impact question; with the store: 412k / 23 steps
   (−82%). The store is a **competence equalizer** — its value is inversely
   proportional to the harness's own search skill. Frontier CLIs ~0%,
   mid-tier −42% (n=6).
2. **Conceptual/onboarding is the strong class on BOTH frontier harnesses.**
   q11 (trace a bio's lifecycle): Claude −34% tokens / −48% wall / 22→11
   turns; Codex −49% tokens / −20% wall. The wiki+cards carried it. Matches
   S1c's −39% wiki finding. (q12, a shallow enumeration, was flat-to-negative
   — the class that wins is deep architecture traces, not trivia.)
3. **Correctness beats economy — the q5 pattern.** On "which gatekeeper allows
   a back-merge", the grep arms of BOTH Codex and Devin named the wrong
   function (`blk_mq_sched_allow_merge` / `blk_rq_merge_ok`); the store arms
   got `ll_back_merge_fn` right via call edges. Grep answers that LOOK right
   and are wrong is exactly the S3 finding (graphify's 81% recall). Impact
   correctness is the moat, not tokens.
4. **Wall time favors the store even at token parity** (Claude −14%): fewer,
   fatter turns.

## Product defects the proof surfaced (fix list)

- **D1 — query noise:** C local-variable/parameter child decls
  (`func.localvar [struct]`) pollute top-10 hits — 50–80% junk on sampled
  kernel queries, burying the real symbol and burning arm-B tokens.
  Fix: exclude/downrank child decl nodes in query ranking.
- **D2 — card caller-list cap:** `card` truncates `called by` at 15 with
  "… 4 more", hiding entries; cost Codex arm B a complete impact answer
  (missed `kyber_domain_wake`). Violates our own no-silent-caps rule.
  Fix: name files in the overflow line, or list all callers for impact use.
- **D3 — lexical miss:** "writeback throttling" ranks `blk-throttle.c`
  (cgroup throttling) above `blk-wbt.c` (wbt). Doc-comment terms aren't
  weighted into the index.

## Honest positioning consequences (for site/README)

- Do NOT claim universal token savings for top-tier agents — measured parity.
- DO claim: onboarding/conceptual savings (−34% measured on Claude Code),
  correctness on impact questions (both frontier grep arms produced a wrong
  gatekeeper), large savings for non-frontier harnesses (−42%, partial n),
  and wall-time reduction.
- The 0.67s gather / 13× vs graphify build-speed claims are unaffected.

## Reproduce

`README.md` in this directory. Runner: `run.py`; extraction: `score.py`;
questions + keys: `questions.json`.
