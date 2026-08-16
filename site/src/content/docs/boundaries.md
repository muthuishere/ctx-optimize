---
title: Boundaries — the external surface
description: "What this system calls out to, what it exposes, and which config values are credentials — one command, every line citable."
---

A repo's **external surface** is spread across env reads, spawned binaries, URL literals and
route decorators in five languages. Grep finds the strings. It cannot tell you the
*direction*, the *transport*, or that a name is a credential.

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

It is a C4-style system-context summary, not a node dump: CONSUMES and PROVIDES are split
because they are different questions, entries are grouped by transport with counts, and the
output is budgeted like `query` — it **always states how many entries were withheld**,
because silent truncation reads as "that is everything".

## Narrowing it

```bash
ctx-optimize boundaries --sensitive                    # credentials only
ctx-optimize boundaries --direction provides           # the routes we serve
ctx-optimize boundaries --transport process.exec       # what it shells out to
ctx-optimize boundaries --all                          # including dynamic identifiers
ctx-optimize boundaries --json                         # otel.* semconv keys pass through
```

`--json` passes metadata through under its **OpenTelemetry semantic-convention** names
(`otel.server.address`, `otel.http.route`), so a static boundary joins a runtime trace on the
same key — no invented vocabulary.

## Credentials by name, never by value

A value never enters the graph, is never printed, and is never stored. A rule flags
`sensitive` when the identifier *name* matches `KEY|TOKEN|SECRET|PASSWORD|_PW`, so
`STRIPE_SECRET_KEY` is marked without anything ever reading what it holds.

## Sixteen rules, each carrying its own measurement

Rules are **data, never code** — declarative AST shapes evaluated inside the code
extractor's existing walk, so a rule costs a map probe rather than another pass over the
bytes. Every shipped rule carries a `verified` block with its ground-truth command, corpora
and known misses, and **the confidence tier is derived from the measured recall, never
asserted**:

| recall | tier |
|---|---|
| ≥ 0.95 | EXTRACTED |
| 0.70 – 0.95 | INFERRED |
| < 0.70 | AMBIGUOUS, or reject the rule |

Most shipped rules were *demoted* by their own numbers. That is the point of measuring.

## Related verbs

- **`drift`** — where `provides`, `consumes` and *declared* disagree: dead contracts, env
  read but never declared. `--strict` is the CI gate.
- **`services`** — a 30-vendor registry for SDK-mediated egress, where the *dependency is
  the boundary*: `stripe`, `openai`, `firebase` produce a port from a manifest declaration
  even when no host literal exists in the source.
- **`boundaries verify`** — re-runs each rule's ground truth and reports drift. Numbers only
  move up.

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
