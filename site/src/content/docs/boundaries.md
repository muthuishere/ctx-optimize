---
title: Boundaries — the external surface
description: "What this system calls out to, what it exposes, and which config values are credentials — one command, every line citable."
---

Grep finds the string. This command adds direction, transport, and whether the **name** is a credential.

```bash
ctx-optimize boundaries
```

```text
boundaries: 73 ports

CONSUMES (what this system calls out to)
  config.env     28 · 28 external · 1 SENSITIVE · 3 dynamic
      OPENROUTER_API_KEY                 INFERRED   SECRET  proof/agent/agent.mjs:L28 (+1 sites)
      CTX_OPTIMIZE_STORE                 INFERRED    internal/app/app_test.go:L49 (+3 sites)
  network.http   16 · 16 external
      api.github.com                     INFERRED    internal/app/app.go:L3149
      codeload.github.com                INFERRED    internal/app/manifests.go:L157 (+2 sites)

UNRESOLVED  3 ports carry a dynamic identifier — the SITE is certain,
            the value is not
```

CONSUMES vs PROVIDES. Grouped by transport. Truncation is printed.

### config.env
Env **names** the code reads. `KEY|TOKEN|SECRET|PASSWORD|_PW` marked SECRET. Never the value.

### network.http
Hosts and routes: `api.openai.com`, `/orders`. Direction: consumes vs provides.

### process.exec
Binaries the process spawns. A variable argv is AMBIGUOUS, not missing.

## Narrowing it

```bash
ctx-optimize boundaries --sensitive                    # credentials only
ctx-optimize boundaries --direction provides           # the routes we serve
ctx-optimize boundaries --transport process.exec       # what it shells out to
ctx-optimize boundaries --all                          # including dynamic identifiers
ctx-optimize boundaries --json                         # otel.* semconv keys pass through
```

`--json` keeps OpenTelemetry names (`otel.server.address`, `otel.http.route`).

A value never enters the graph. `sensitive` is the **name** matching `KEY|TOKEN|SECRET|PASSWORD|_PW`.

Rules are JSON on the same AST walk. Tier comes from measured recall:

| recall | tier |
|---|---|
| ≥ 0.95 | EXTRACTED |
| 0.70 – 0.95 | INFERRED |
| < 0.70 | AMBIGUOUS, or reject the rule |

Most shipped rules were demoted by their own numbers.

## Related verbs

### drift
Where provides, consumes, and declared disagree. `--strict` is the CI gate.

### services
SDK egress from the manifest: `stripe`, `openai`, `firebase` become ports even with no host literal.

### boundaries-verify
`boundaries verify` re-runs each rule's ground truth. Numbers only move up.

## Authoring your own

Point your agent at the bundled `boundaries-authoring` reference and it will follow an
eight-step measured loop — survey, propose, ground, run, measure, iterate, tier, write —
emitting reviewed JSON into `.ctxoptimize/boundaries.json`. Repo rules merge over machine
rules over the shipped defaults, **by rule id**, so you can narrow or replace a default
without forking anything.

:::caution
Two honest limits. **`scope` says `internal` or says nothing.** It is written only when the
join actually fires — when a consumed identifier matches a `provides` port in the same
gather. Absence means *not proven internal*, which is usually because the two sides are
different namespaces: every shipped `consumes` rule on HTTP yields a **host**
(`api.openai.com`) and every shipped `provides` rule yields a **route path** (`/orders`).
There is no `external` value, because "we did not find it here" is not evidence that a
call leaves the building. On the default rule set you will therefore see no `scope` at
all; a repo that authors a rule sharing the provides namespace — a same-origin
`fetch("/orders")` rule, say — gets a real `internal`.

And a **port list is a floor, not a census**: a host assembled by string concatenation is
not a literal, so it is reported as AMBIGUOUS or not at all. The rules say what they miss.
:::
