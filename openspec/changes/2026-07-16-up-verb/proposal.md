# ADR — `up`: the omakase get-me-a-store verb; `init` becomes author-only

Status: APPROVED (maintainer "cool / do that", 2026-07-16). Follows
2026-07-16-scripted-remote-transports.

## Context

On a fresh clone with a committed `.ctxoptimize/config.json`, `init` adds
nothing — the scaffold and pointer blocks are already committed; its only
clone-side value is the auto-pull courtesy. Meanwhile the newcomer must
CHOOSE between `remote pull` (did the team publish a store?) and `add .`
(build it locally) — a decision that requires knowledge the config already
encodes. Maintainer: "if they have config.json already do we need init or
just pull… or some better verb". The tool should decide.

## Decision 1 — `ctx-optimize up`

One idempotent verb, safe to run any time, zero decisions
(docker-compose/vagrant `up` semantics — make it exist by whatever means):

| State | Action |
|---|---|
| no local store, `remote.pull` declared | run the declared pull |
| pull fails (creds/tooling not ready) | FALL BACK to gathering locally (`add .` lane), say so |
| no local store, no remote | gather locally (`add .` lane; config drives modules/fan-out) |
| store present, stale vs git HEAD | fast re-gather (`sync` lane — no adapter scripts) |
| store present, fresh | "up to date", exit 0, touch nothing |

- Scope-aware like every verb (module dir → that module; root → tree).
- No config anywhere → behaves like today's bare flow: scaffold via init
  lane first? NO — `up` never scaffolds; without a config it gathers into
  the basename-keyed store exactly as `add .` would. Authoring stays with
  `init`.
- `--force` passes through to the gather lanes (shrink guard unchanged).

## Decision 2 — verb boundaries after `up`

- `init` = AUTHOR-side only: create config/samples/pointers the first
  time. Its auto-pull-on-clone courtesy MOVES to `up`; on a clone with a
  pull-declaring config and an empty store, `init` now just prints
  "run: ctx-optimize up". (Kept working, redirects — never silently
  rebuilds.)
- `pull` keeps meaning exactly "run the declared pull command" — it never
  gathers. `sync`/`add` unchanged. No verb is removed.
- Torvalds-style overload of `pull` (pull-or-build) REJECTED: quietly
  gathering 4k files when there is no remote is a surprise, not a
  convention.

## Decision 3 — every onboarding surface says `up`

The whole story collapses to "fresh clone? `ctx-optimize up`":

- Pointer blocks (`project.go` pointerBlock `<no-local-store>`, both
  single and multi-module wording) and the global always-on block's
  no-store bullet.
- SKILL.md: frontmatter no-store line, routing rows (clone case + "told
  code changed" row gains nothing — sync stays), fast-path note.
- activation-routing.xml: `onboard-clone` route → `up`; new `up` route in
  the build group; `onboard-new` keeps init for authors.
- onboarding.md + push-pull.md teammate flows; help text; CHANGELOG.

## Consequences

- CI gate becomes `ctx-optimize up && ctx-optimize fresh`.
- The 0.3-era "init auto-pulls on clone" test moves to `up`
  (init's version asserts the redirect message instead).
- Additive verb — no breaking change; ships in the next release with
  0.4.0's scripted transports.
