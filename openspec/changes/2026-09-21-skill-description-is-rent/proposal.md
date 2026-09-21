# ADR 35 — the skill description is rent, not documentation

Status: DRAFT — 2026-09-21. Opened from GitHub issue #18 (deemwario).
Numbers below re-measured this session on this machine, not taken from
the issue.

## 1. The measurement

`internal/skills/bundled/ctx-optimize/SKILL.md`:

| part | size | when it is paid |
|---|---|---|
| `description:` (YAML) | **4,563 B (~1,140 tok)** | **every session, every turn, whether or not the skill is invoked** |
| body | 30,525 B | only on invoke |

Across the 64 skills installed here (`~/.claude/skills/*/SKILL.md`,
description block only):

- median **695 B**
- ours **4,563 B** — **6.6× the median and the largest of all 64**
- next largest: window-ctl-skill 1,745 · recipe-builder 1,645 ·
  herdr-share 1,543 · explainer-video 1,318

The issue's "8× median / 66 skills" is the same finding on a slightly
different roster snapshot; the direction and the rank are confirmed
independently.

## 2. Why it is rent

A skill description is resident context: loaded at session start in every
session and re-sent on every turn. Cost is `tokens × turns`, not tokens.
So ~1,140 tokens is paid in full by every user of ctx-optimize in every
session where they never touch the store — which is most sessions, since
the roster is global and the marker check happens after loading.

This cuts directly against the product's own thesis. We sell "stop paying
for context you don't need," and we ship the single most expensive
always-on description on the machine.

## 3. What is in there that is body content

Auditing the current description, these are instructions for *after* the
model has decided to invoke, and all of them already exist in the body:

- the "SHELL COMMAND, not a callable tool / never call `ctx_optimize`"
  disambiguation;
- the full verb catalogue with inline examples — `query`, `card`,
  `change-plan`, `affected`, `path`, `explain`, `verify`, `wiki`,
  `boundaries`, `nodes`, `drift`, `services`;
- bootstrap guidance (`up`, `init --scan --yes`, `serve`, the 127.0.0.1:4747
  surface and its tab names);
- the native-sources contract (`adapters help <scheme>` → export → `add`);
- pointers to `references/sources.md`, `onboarding.md`, `dashboard.md`,
  `customize.md`, `boundaries-authoring.md`.

A description's only job is the invoke/don't-invoke decision. Everything
above is post-decision.

## 4. Proposed description (~870 B)

```
Pre-built knowledge graph of this codebase — answers "where is X", "who
calls Z", "what breaks if I change W", "is this citation still true", with
cited file:line, instead of a grep-and-read chain. REQUIRED before any
Grep/rg/Glob/Read when a `.ctxoptimize/` directory exists at the repo root
or any parent of your cwd — that marker means the graph is already built.
Also use to build, refresh, inspect or share that graph, to add a database /
bucket / queue / OpenAPI schema to it, to answer what the code calls, reads,
spawns or exposes, and to customize extraction (routes, manifests, language
packs). Trigger on: where is X, how does Y work, who calls Z, what breaks if
I change W, architecture, onboarding, verify this citation, gather/index this
repo, onboard this monorepo, push/pull the store, open the dashboard, add our
postgres schema, what external APIs do we call.
```

Saving: ~3,700 B / ~920 tokens resident, per session, per user, forever.

## 5. The honest risk

Some of the bloat may be load-bearing for *triggering*. The
"NEVER call a tool named ctx_optimize" line in particular was added
because models were hallucinating an MCP tool; the enumerated trigger
phrases may be doing real work for the sources/onboarding/boundaries lanes,
which have no obvious keyword overlap with "knowledge graph".

Mitigation, in order:

1. Every line removed from the description must be **verified present in
   the body** before removal — `docdrift_test.go` already asserts several
   substrings against the whole file, so a naive move keeps `task ci` green
   while silently losing nothing only if we check.
2. Run `anthropic-skills:skill-creator`'s eval harness on both versions
   with a question set covering all five lanes (code Q&A, sources,
   onboarding, boundaries, dashboard) plus negatives (sessions where the
   skill must NOT fire).
3. If triggering measurably regresses, the answer is a **middle size**
   (~1,500 B, still ≤ the next-largest skill on the machine), not a revert.

## 8. Research (2026-09-21) — it is worse AND cheaper than the issue says

Sources: the official `skill-creator` plugin shipped in
`claude-plugins-official` (`scripts/quick_validate.py`,
`scripts/improve_description.py`), and this session's own skill roster.

**8.1 There is a hard spec limit of 1,024 characters.**
`quick_validate.py:82` rejects any description over 1024. The improver's
own prompt (`improve_description.py:132`) states it outright: *"There is a
hard limit of 1024 characters — descriptions over that will be truncated,
so stay comfortably under it"*, and targets **100-200 words**. Our
description is **4,443 characters / ~700 words** — 4.3x the hard limit.

**8.2 Claude Code already truncates it at 1,535 characters.**
Measured against this session's injected roster: our entry is cut
mid-word at `Also builds/re…`, at offset **1,535**. `window-ctl-skill`
(1,691 B) is cut at the identical offset — 1,535 is the loader's limit,
not a coincidence.

Two consequences, both load-bearing:

- **The cost is real but ~3x smaller than the issue claims.** Only ~1,535
  chars (~384 tok) is resident, not 4,563 B / 1,140 tok. The ~130M-token
  figure should be restated as roughly 45M. Still the largest on the
  machine, still worth cutting — but we must not publish the 1,140 number.
- **Everything past 1,535 chars has never triggered anything.** That
  silently includes ALL of: native sources / `adapters`, onboarding and
  `scan`, `serve` / the dashboard, routes-manifests-grammar customization,
  and the entire BOUNDARIES lane. We wrote trigger text for five lanes and
  shipped it into a region the model never sees. This is a **capability
  bug**, not only a cost bug — and it means the "is the bloat load-bearing
  for triggering?" risk in section 5 is much smaller than feared: the
  bloat cannot be load-bearing, because it is not loaded.
  **CORRECTED 2026-09-21 by section 13** — those lanes do trigger, at 100%,
  on the SURVIVING prefix alone. The post-cut text was dead, but the lanes
  were never dark. This section's capability claim was wrong.

**8.3 The official validator currently FAILS our skill on a second count.**
`python3 scripts/quick_validate.py internal/skills/bundled/ctx-optimize`
returns: `Description cannot contain angle brackets (< or >)`. We use them
throughout (`card <symbol>`, `path <a> <b>`, `adapters help <scheme>`,
`verify "<label or file:L10-L20>"`). Any trim must also drop them.

**8.4 Counter-evidence to trimming, stated fairly.**
skill-creator's authoring guidance says *"All 'when to use' info goes here,
not in the body"* and that the known failure mode is **under**-triggering,
so descriptions should be deliberately "pushy". So: keep the pushy
REQUIRED-before-Grep line and keep intent coverage for every lane — we
just have to say it in 1,024 chars instead of 4,443. Compression, not
deletion.

## 9. Revised proposal (882 chars, 157 words, no angle brackets)

```
Use this skill for any question about a codebase you have not fully read:
where something is, how it works, who calls it, what breaks if you change
it, whether a cited file:line is still true, or how the project is laid
out. REQUIRED before Grep, rg, Glob or Read whenever a .ctxoptimize
directory exists at the repo root or any parent of your cwd — that marker
means a knowledge graph of this code is already built, and one call answers
what a grep-and-read chain would, with cited locations. Also use it to
build, refresh, inspect or share that graph, to onboard a repo or monorepo,
to pull a database, bucket, queue or OpenAPI schema into it, to ask what
the code calls, reads, spawns or exposes, and to customize extraction for
your framework's routes, manifests or language packs. ctx-optimize is a
shell command on PATH, not a callable tool: run every verb through your
shell.
```

- 882 chars: under the 1,024 hard limit, under the 1,535 loader cut,
  inside the 100-200 word target.
- Passes `quick_validate.py` (no angle brackets).
- **Covers all five previously-invisible lanes** — sources, onboarding,
  dashboard/share, customization, boundaries ("calls, reads, spawns or
  exposes") — which today trigger on nothing.
- Keeps the pushy REQUIRED-before-Grep clause and the not-a-tool line.
- Net resident saving: 1,535 -> 882 chars (~165 tok/session), plus the
  30,000-character body is unchanged.

## 10. Verification plan

1. Move the dropped specifics into the body first. Audited: `change-plan`,
   `adapters help`, all five `references/*.md`, `4747`, `init --scan`,
   `up`, `verify` are already in the body. **"SHELL COMMAND" appears zero
   times in the body** — it exists ONLY in the description and must be
   written in before it is cut.
2. `python3 scripts/quick_validate.py` must pass (it fails today).
3. `task ci` — `internal/skills/docdrift_test.go` asserts substrings
   against the whole file, so a move keeps it green; re-run to confirm.
4. skill-creator eval: the real risk is the five lanes, so the question
   set must cover sources / onboarding / dashboard / customize /
   boundaries plus negatives. Note the baseline to beat is the
   **truncated** description, not the file — and on four of those five
   lanes the baseline is "no trigger text at all", so a regression there
   is close to impossible.

## 6. Decision needed from the owner

- [ ] Trim to ~870 B and measure (proposed), or go straight to a middle
      ~1,500 B, or leave it.
- [ ] Whether a triggering eval is a gate for this change or a follow-up.

## 7. Scope

Touches `internal/skills/bundled/ctx-optimize/SKILL.md` only. No binary
behaviour changes. `task ci` + `task golden` unaffected except for the
docdrift substring assertions.

## 11. Wire evidence (2026-09-21) — measured, not inferred

Sections 8-10 reasoned from the roster this session was given. That is the
agent's *belief* about its context. This section reads the actual HTTPS
request body.

Method (now a reusable skill, `prompt-xray`): a local pass-through proxy
captures the full request; `ANTHROPIC_BASE_URL=http://127.0.0.1:PORT claude
-p "…"` routes a real subscription session through it. AgentProxy could not
be used for this — it truncates every logged body at 12,000 bytes
(`agent-proxy/apps/agentproxy-cli/internal/logger/traffic.go:164`) and a
Claude Code request is ~90,000.

One request, `claude-opus-5`, 61 skills installed:

| part of the request | chars |
|---|---|
| system blocks | 9,638 |
| messages (the skill roster lives here, not in `system`) | 78,228 |
| tool schemas (82 tools) | 62,651 |
| skill roster total | 68,039 |

**Finding 1 — the 1,535-char cut is real.** Exactly two descriptions arrived
truncated, each 1,535 chars plus a trailing `…` (1,536): `ctx-optimize` and
`window-ctl-skill`. Everything we wrote past that point was never sent.

**Finding 2 — the fix lands.** Re-capturing with the new description:

```
ctx-optimize chars on the wire: old 1536 (truncated) -> new 858 (intact)
```

The description is now delivered whole for the first time, and the five
lanes that lived past the cut (sources, onboarding, dashboard/share,
customization, boundaries) have trigger text in the model's context at all.

**Finding 3 — toolnexus does not truncate, so the bill is worse there.**
Every port interpolates the description verbatim into the skills prompt
(`js/src/skill.ts:421`, `python/src/toolnexus/skill.py:306`,
`java/.../SkillSource.java:166`, `csharp/.../SkillSource.cs:88`,
`golang/skill.go:378`). On an opencode-style runtime the full 4,443 chars
were paid every turn — no cut, no mercy. The trim helps that host ~5x more
than it helps Claude Code.

**Correction to section 8.2.** The resident cost on Claude Code is 1,536
chars (~384 tok), and the new cost is 858 (~215 tok) — a ~170-token saving
per session, not the ~950 the issue projected. The issue's headline economics
are overstated; the *capability* half of the finding is understated. Both go
in the reply.

## 12. Status

IMPLEMENTED 2026-09-21. `internal/skills/bundled/ctx-optimize/SKILL.md`
description: 4,443 -> 858 chars, no angle brackets, passes
`skill-creator/scripts/quick_validate.py` (it failed before, on the angle
brackets). Body unchanged; `task ci` green.


## 13. Stress test (2026-09-21) — 5 runs x 25 queries per arm

The official `skill-creator/run_eval.py` cannot score a CLI skill: it counts
a trigger only if the FIRST tool call is `Skill` or `Read` of a command
file, and a shell-command skill correctly routes through `Bash`. It returned
0/20 on BOTH arms. Discarded rather than reported.

Replacement harness (`scratchpad/skilleval/stress.py`): drive a real
`claude -p` session inside a sandbox repo that has a real store, and count a
hit when the session actually runs a `ctx-optimize` command. 25 queries —
17 positives across 13 lanes (in a store repo, plus 2 onboarding queries in
a store-less repo) and 8 negatives including two edit traps.

Pooled, n=85 positive runs and 40 negative runs per arm:

| metric | OLD (1,536, truncated) | NEW (858) | v3 (943) |
|---|---|---|---|
| recall | 85/85 = **1.000** | 85/85 = **1.000** | 51/51 = **1.000** |
| all 13 lanes | green | green | green |
| first-call rate | 75/85 = 0.882 | 66/85 = 0.776 | 40/51 = 0.784 |
| false-fire | 5/40 = 0.125 | 6/40 = 0.150 | 5/40 = 0.125 |

**Finding A — the trim costs no triggering.** Recall is perfect and
lane-for-lane identical on every arm. The rewrite is safe.

**Finding B — section 8.2 was wrong about consequences.** The OLD arm hit
all five "invisible" lanes at 100%, because the text that SURVIVES the cut
contains `INVOKE this skill before any Grep/rg/Read` and the
`.ctxoptimize/` marker. That clause alone routes nearly anything in a store
repo; the lane-specific trigger text past the cut was never doing the work.
The bytes past 1,535 were dead, but no capability was dark. The honest case
for this change is cost plus spec compliance, not recovered capability.

**Finding C — first-call is ~10 points lower, and it is not the wording.**
0.882 -> 0.776 is 9 events at n=85 (z~1.8, p~0.07) — under the 0.05 line
but on the metric that IS the thesis. v3 added the primacy language back
("REQUIRED as the FIRST tool you reach for", "fall back to grep only for
what it does not hold") for +85 chars and moved it 0.776 -> 0.784: inside
noise. So the emphasis was not the cause and buying it back is not worth
the bytes.

**Decision: ship the 858-char v2.** v3 is indistinguishable on every metric
and costs more. Recorded here so a future session does not re-litigate it:
if first-call is ever shown to matter at a larger n, the lever to try is NOT
more description — it is the repo-level `instructions.md` and the hook,
which are loaded only in repos that actually have a store.
