# ADR 36 — `code` verb: embed CLI Code Mode over the read-only verbs

Status: ACCEPTED by the owner 2026-11-22 ("whats stopping you"). Local commits only; push is the owner's call.

## Problem
An agent answering one question runs ~7 processes (query, card x5, change-plan), each paying
process start, a model round-trip and unfiltered output. One program can do it in one process
and return only the filtered result.

## Decision
Add `ctx-optimize code <operations|describe|check|run|eval|skill|...>` using
`github.com/muthuishere/clicodemode` (pinned to a commit, never `@main`; MIT, pure Go).
Each READ-ONLY verb is wrapped with `cmdtree.FromRunner(..., WithJSONResult("--json"),
WithSideEffects(SideEffectNone))` over the existing `cmdX` functions: no handler is duplicated.
Operations: `ctx.query`, `ctx.card`, `ctx.change-plan`, `ctx.affected`, `ctx.path`, `ctx.hubs`.

## Doctrine check
- No LLM, no embeddings, no MCP, no DB driver in the main binary. clicodemode is a deterministic JS-subset interpreter.
- Mutating verbs (add/sync/up/install/remote/config/store) are NOT exposed. Programs cannot gather or write.
- Cost: one dependency (`gopkg.in/yaml.v3`) + binary size delta, measured in the commit.
- Existing verbs and tests unchanged; `code` is purely additive.

## Skill
The embedded agent skill gets a Code Mode section: when to send one program instead of many verb calls,
results are JSON objects, `--input`/`--json`, and `ctx-optimize code run - --input ... --json` once per task.

## Acceptance
`task ci` green; `code operations|describe|check|run` work; a chained query -> parallel card -> change-plan
program runs in one process; `--input` works; exit codes 0 ok / 1 failed / 2 nothing ran.
