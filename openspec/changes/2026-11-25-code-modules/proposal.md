# ADR 37 — ship JS modules for Code Mode (clicodemode v0.8.0)

Status: ACCEPTED by the owner 2026-11-25 (relayed from the clicodemode owner session: "adopt v0.8.0 and ship your own JS modules, then release"); module choice left to this repo.

## Problem
Agents writing generic JS over six raw operations pick wrong ops, wrong args, wrong order, and retry.
The recurring tasks are known: plan an edit, find-and-inspect, blast radius over several symbols, orient.

## Decision
`internal/app/js/*.js`, embedded (`go:embed`) and registered as scheme `ctx` (`js/edit.js` = `ctx:edit`).
Operations stay primitives; modules hold fan-out, defaults, deduplication and data shaping.
- `ctx:edit` planEdit, planEdits · `ctx:find` findCards · `ctx:impact` blastRadius · `ctx:orient` orient, connect
- `ctx:safe` settle (shared, misses reported not thrown). Rejected: 1:1 wrappers of an op (no value).
Each module has a leading comment and a doc comment on every export. A module can call only registered
operations, so there is no new privilege and the six operations stay read-only.

## Verification
`engine.CheckModules()` returns no diagnostics; one run test per module on a gathered fixture store; every export
documented; the drift test requires "ctx:edit" on all three agent surfaces. `task ci` green.

# ADR 38 — program-first default (2026-11-25, owner: "by default its going for query")

Evidence (60 sessions, 2026-09/11 transcripts): first verb `query` 48 vs `up` 16, `card` 14, `change-plan` 14;
same-verb runs dominate (query,query 52; search,search 44; card,card 40); after a store call the next call is bash/grep
in ~85% of cases. Decision: lead every agent surface with a program, add `ops.ctx.search` and `ops.ctx.verify` (both read-only),
and ship `ctx:investigate`, `ctx:sweep`, `ctx:cite`. Declined: arbitrary shell execution from a program (breaks the
read-only guarantee and the deterministic-binary doctrine); the search op covers the grep a program needs.
