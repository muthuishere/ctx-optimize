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
