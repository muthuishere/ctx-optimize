# ADR 33 — market recon: the seat is taken, and we are pitching the wrong pain

Status: DRAFT — 2026-09-11. Live recon: HN Algolia (stories + comments),
Reddit via the chrome-agent logged-in lane, GitHub API. Every number and
quote below was fetched this session. Supersedes ADR 31's core premise.

## 1. ADR 31's premise is dead

ADR 31 (2026-08-22) said the lane was "validated but execution-starved,"
with twelve-plus Show HNs "almost all stalled at 1-3 points," and that "the
measured+fast+deterministic version is the open seat."

Verified on the GitHub API today:

| repo | stars | forks | created |
|---|---|---|---|
| **CodeGraph** | **70,426** | 4,505 | 2026-01-18 |
| **GitNexus** | **47,210** | 5,155 | 2025-08-02 |
| Serena | 29,149 | — | — |
| Repomix | 28,287 | — | — |
| potpie | 5,717 | — | — |
| **ctx-optimize** | **0** | — | — |

Checked twice because they looked implausible: 4,505 forks and 485 open
issues is real adoption, not star-farming. **CodeGraph went 0 → 70k in eight
months**, and it is precisely the tool that beats us on query (0.98 s vs
4.11 s) and warm re-gather (0.2 s vs 4.9 s).

The seat is taken. Bet 4's "launch on HN and convert downloads to community"
is now a plan to arrive third.

**Where it did NOT come from: HN.** Every code-graph Show HN pulled scored
1-4 points with zero comments — Rig, Arbor, Sonde, RemembrallMCP,
CodeGraphContext, Vexp. Meanwhile the same topic on Reddit draws 280, 338,
555, 570 and 794 upvotes. **We have been planning a launch on the channel
where this category reliably dies.** (Memory also records HN/Reddit as
shadowbanned for our posting — that constraint now matters far more.)

## 2. We are pitching the wrong pain

Our homepage says: faster to find, cheaper model, fewer tokens. What people
actually complain about, in their words:

**Session amnesia** — r/ClaudeCode, 2026-02-24, 280 up:
> "Session amnesia. Every new session, Claude re-discovers the same
> architecture, re-reads the same files, asks the same questions. All the
> understanding from yesterday's session? Gone."

**Cross-agent rediscovery** — r/cursor, 2026-05-06:
> "Open a new session on a project I worked on yesterday → first 2-4 minutes
> the agent is grepping around rediscovering what files exist... Switch from
> Claude Code to Codex mid-task and the whole rebuild happens again."

**Memory that rots** — r/cursor, 2026-05-06:
> "a memory server saving 'the auth lives in services/auth' is a fact that
> goes stale silently the day someone moves it."

**Add instead of change** — HN, 2026-07:
> "The most painful part is the 'add instead of change/delete' habit. The real
> test... is whether they can understand the existing system, reuse the right
> abstraction, remove bad code, and own the whole call chain after the change."

**Our product, described as an open question by someone who has not heard of
us** — HN, 2026-08:
> "what scheme can we use to organize our code base such that an agent working
> on one part really can make changes and not break the other parts on
> accident"

Note what is NOT on this list: "finding is too slow." The felt pain is
*re-discovery*, *staleness*, and *damage* — not latency.

## 3. Token-savings claims are now radioactive — and that is our asset

r/ClaudeCode, 2026-08-08, 46 up, a rival tool author benchmarking 5
token-saving tools over 261 runs:
> "Caveman claimed 65% and measured 8.5%. RTK claimed 60-90% and ended up
> slightly more expensive than using nothing."

And Graphify's own 794-up thread was dismantled in comments: "not that hard
for agents to reproduce" (237 up), "I don't quite get why it's any better
than an LSP" (44 up).

**We killed our own token claim when S16 measured -0.2%/+3.0%.** In a market
that has just watched two tools get caught, being the one that retracted its
own headline is worth more than any number we could publish. But it only
counts if we SAY it, and the site currently does not.

Corollary: any launch must answer "why not just an LSP?" in the first
paragraph, unprompted. It is the top objection in every thread.

## 4. Four unserved gaps — we already built three of them

The recon asked what no named tool solves. Three of the four answers are
features we shipped and do not talk about.

| gap nobody serves | what we already have |
|---|---|
| Cross-agent memory — Claude Code, Codex and Cursor each re-learn the same repo independently | `install` fans one store to claude + codex + copilot + devin. **One gather, every agent.** |
| Memory that rots — static memory files go stale silently | The store is DERIVED from code; `fresh` reports staleness vs git HEAD, `sync` fixes it. A fact cannot outlive its file. |
| No trusted comparison of graph tools — every thread asks "how does this compare to X" and gets no answer | We own a built arena, 12 corpora, a committed field manifest, and a habit of publishing our losses. |
| Human comprehension of AI-written codebases ("the visualization tools you seek don't exist yet", 361 up) | `serve` + Flow + `boundaries`. |

## 5. Proposed position

Retire "built fastest, answered cheapest." It fights CodeGraph where CodeGraph
wins and it makes a token-adjacent claim in a market that has stopped
believing them.

> **One store. Every agent. Never stale.**
>
> Your agent re-learns your repo every session, and so does the next one you
> switch to. ctx-optimize gathers once — Claude Code, Codex, Copilot and
> Devin all read the same graph, it tells you when it is stale, and it knows
> what breaks before your agent changes it.

Why this and not the speed pitch:
- It names the pain in the words people use (amnesia, rediscovery, staleness).
- **Cross-agent is genuinely unserved.** Not "we are faster at X" — nobody
  else does it at all, and it is the one gap the recon found with no tool
  attached to it.
- It is unfalsifiable by a benchmark war, which we would lose on query.
- `affected`/`change-plan` become the "won't break other parts" answer, which
  is the HN quote verbatim.

## 6. Actions

1. **Re-site around the new position.** Homepage, compare, and the skill's
   intent table lead with cross-agent + non-stale + blast radius, not speed.
2. **Add the retraction as a visible credential.** A short "what we stopped
   claiming, and why" block. In this market it converts.
3. **Answer the LSP question on the site**, in a heading, unprompted.
4. **Stop planning an HN launch as the primary channel.** The category dies
   there. Reddit has the audience and we are shadowbanned, so the realistic
   channel is a written artifact others cite — which points at (5).
5. **Publish the neutral comparison nobody has.** Every thread asks it; no
   one answers it. We have the arena and the disposition. This is the single
   highest-leverage thing we own, and it doubles as distribution.
6. **Re-run the field first** (ADR 31 Bet 1, still unmet). A comparison
   published on unpinned, wiki-inflated numbers would end the credential in
   one thread.

## 7. Kill criteria

- If the neutral comparison is published and cited by no one in 60 days, the
  content-as-distribution thesis is wrong; fall back to being personal infra
  plus a skill, and stop spending on positioning.
- If CodeGraph ships boundary/outer-surface answering, our last uncontested
  axis is gone — at that point merge the differentiator into the agent-skill
  layer rather than defending a standalone product.

## Open questions

1. Does "never stale" survive scrutiny? `sync` is fast but warm re-gather
   loses 6/6 to CodeGraph — staleness is a claim about correctness, not
   speed, but a reviewer will conflate them.
2. Is the neutral comparison credible from a participant? Options: publish
   method + harness and invite re-runs, or recruit a third party to run it.
3. Cross-agent is the wedge — is it actually true in practice, or does each
   harness need per-agent tuning we have not measured?
