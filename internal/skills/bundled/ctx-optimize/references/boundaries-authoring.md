# Authoring boundary rules — measured, or it does not ship

**You are the generator.** The binary is deterministic and model-free; the
model runs ONCE, here, at authoring time, and what it emits is reviewed JSON
committed beside the code. Your output is `boundaries.json` — **data, never
code**.

A boundary is code binding to an externally-addressable name that no compiler
checks: an env var, a spawned binary, an HTTP host, a route it serves, a queue
topic, a storage key. The graph calls each one a **`port`** node.

## The law: a rule without its measurement is invalid

This exists because of two measured failures on real repos, both caught by
accident:

- **recall:** a rule matched 16 of **137** real ui→api call sites (12%). Every
  call went through one wrapper (`apps/ui/src/lib/http.ts`), and a
  literal-argument regex cannot see through a wrapper. **The tool reported
  nothing wrong.**
- **precision:** a vendored corpus (`benchmarks/corpus-flask/docs/conf.py`) was
  reported as production egress.

A boundary graph that is silently 12% complete is worse than none — agents and
humans will trust it. So every rule carries a `verified` block, and **the tier
is derived from the measurement, never asserted.**

```
recall ≥ 0.95            → EXTRACTED
recall 0.70 – 0.95       → INFERRED
recall < 0.70            → AMBIGUOUS, or reject the rule
```

`known_misses` is **mandatory** whenever recall < 1.0. Silence that looks like
completeness is the one failure that makes a graph lie.

**Hold this standard against yourself.** The rules that ship in this binary
were measured and most were *downgraded* by their own numbers: the `env-*`
rules landed at **INFERRED** (recall 0.81–0.95 — `os.Getenv(varName)` with a
variable name is unmatchable), and the `process-*` rules at **AMBIGUOUS**
(recall 0.65, and 0.00 for python — argv is nearly always a variable). Honest
numbers that demote your own rule are the point, not a failure.

## The loop — eight steps, all required

```
1 SURVEY    from the STORE, not the filesystem
2 PROPOSE   one rule, smallest useful scope
3 GROUND    an independent count
4 RUN       apply the rule
5 MEASURE   recall AND precision
6 ITERATE   fix what the measurement exposes
7 TIER      derive it from the numbers
8 WRITE     rule + verified block; no evidence, no rule
```

### 1 SURVEY — the gap between "kinds present" and "kinds connected"

Ask the store what exists before guessing what to match:

```sh
ctx-optimize deps                      # what this repo talks to by dependency
ctx-optimize nodes --kind port         # boundaries already captured
ctx-optimize nodes --kind route        # provider side from the AST recognizers
ctx-optimize edges --relation consumes
ctx-optimize query "http client wrapper" --json
```

A kind that is *present* but never *connected* is the work list. So is a
dependency in `deps` with no matching `port`.

### 2 PROPOSE — one rule, smallest useful scope

One transport, one direction, one file-extension set. A rule that tries to
cover Go and TypeScript and Python at once cannot be measured, because its
recall is an average over three different failure modes.

### 3 GROUND — the count must be independent of your regex

Ranked, best first:

| rank | source | independent because |
|---|---|---|
| 1 | framework registry — swagger, `go list`, router table, migrations | authoritative by construction |
| 2 | our own verbs — `nodes`, `edges`, `card`, `affected`, `verify` | tree-sitter-derived, not your regex; always present; fastest |
| 3 | `ctx-optimize search '<regex>' --ext .go --count` | raw count vs capture logic; cross-OS incl. Windows; **same file set as the extractor** |

```sh
ctx-optimize search 'exec\.Command\(' --ext .go --count
ctx-optimize search 'os\.Getenv\(' --ext .go,.ts --path internal/ --count
```

Use `search`, not `rg`/`grep`: it is cross-OS (a teammate on Windows
reproduces your number) and it walks **the same files the extractor walks**, so
the denominator is honest. External `rg`/`grep` are optional cross-checks only
— nothing may depend on them.

**Ground truth must be broader than your rule.** Count `os\.Getenv\(` — every
call site — not `os\.Getenv\("[A-Z_]+"\)`, which is your own capture logic
wearing a disguise and will always report recall 1.00.

### 4 RUN

Write the rule into `.ctxoptimize/boundaries.json`, then:

```sh
ctx-optimize add . --force        # rules are file-set-wide; --force re-runs every module
ctx-optimize nodes --kind port --where transport=config.env
```

### 5 MEASURE — both numbers, every time

- **recall** = matched ÷ ground truth. Catches the 16-vs-137 class.
- **precision** = confirmed ÷ sampled. Open **at least 10** hits at their
  `file:line` and confirm each is a real boundary. Catches the
  vendored-corpus class. `ctx-optimize verify "<file:L10-L20>"` is exactly
  this check.

### 6 ITERATE — this is where the 137 case was solved

Low recall is information about the *codebase*, not a prompt for a cleverer
regex. The ui→api rule did not reach 137 by adding alternations; it got there
by discovering the wrapper in step 5 and matching **the wrapper's exported
function names** instead of raw `fetch(`. Read the misses before you edit the
pattern.

Recall that will not move is often a real ceiling — a variable argument cannot
be resolved by any regex. Record it in `known_misses` and let the tier fall.

### 7 TIER — derive, never assert

Apply the table. If the sample is tiny, say so and cap the tier: the shipped
`websocket-js` rule measured recall 0.5 on n=2 and is capped at INFERRED with
`"n=2 is too small to derive a tier mechanically"` written into its
`known_misses`.

### 8 WRITE

Rule plus evidence, in one committed diff a reviewer can read.

## The schema — real field names, from `internal/boundaries/defaults.json`

```json
{
  "version": 1,
  "boundaries": [
    {
      "id": "env-go",
      "transport": "config.env",
      "direction": "consumes",
      "when":    { "ext": [".go"] },
      "exclude": { "path": ["testdata/", "benchmarks/"] },
      "match": [
        { "re": "os\\.Getenv\\(\\s*\"([A-Z][A-Z0-9_]+)\"", "identifier": 1 }
      ],
      "flag": {
        "when_identifier_matches": "KEY|TOKEN|SECRET|PASSWORD|_PW",
        "set": { "sensitive": "true" }
      },
      "metadata": { "otel.server.address": "$identifier" },
      "tier": "INFERRED",
      "verified": {
        "at": "2026-08-14",
        "ground_truth": {
          "tool": "ctx-optimize search",
          "cmd": "search 'os\\.Getenv\\(' --ext .go --count  # corpora: go-kubernetes, go-gin, ctx-optimize"
        },
        "expected": 179, "matched": 145, "recall": 0.81,
        "sampled": 10, "confirmed": 10, "precision": 1.00,
        "known_misses": [
          "os.Getenv(varName) — name is a variable (k8s: 31 sites)",
          "lowercase or 1-char names excluded by the [A-Z][A-Z0-9_]+ shape"
        ]
      }
    }
  ]
}
```

Field notes that are easy to get wrong:

- `exclude` is **top-level**, not inside `when`. `when` holds only `ext`.
- `match[].identifier` is the **1-based capture group** holding the external
  name. Group 0 is invalid.
- `direction` is `provides` (this repo serves it) or `consumes` (this repo
  calls it). **`scope` is NOT yours to set** — the engine computes
  internal/external by JOIN: a consumed identifier is `internal` iff some
  `provides` port in the workspace matches it.
- `metadata` values may interpolate `$identifier`. Open metadata keys must be
  **namespaced** (`otel.*`, `pack.*`, `org.*`) — the schema door rejects a bare
  unknown key fail-closed. Reserved keys: `direction`, `transport`,
  `identifier`, `scope`, `sensitive`, `resolved`, `raw`, `producer`.
- `tier` omitted defaults to EXTRACTED. **Always set it explicitly** from your
  measurement.
- Identifiers are normalized at emit for `network.*` transports (case, trailing
  slash, `{id}`≡`:id`≡`<id>` → `*`); the source spelling survives in metadata
  `raw`. Other transports are verbatim — env keys are case-sensitive facts.
- An identifier containing `${`, `%s` or `%d` is auto-downgraded to AMBIGUOUS
  with `resolved: dynamic`. That is the engine refusing to assert a template.

**The ladder** (merged by rule `id`, later wins — so a repo rule can narrow or
replace a shipped default by reusing its id):

```
.ctxoptimize/boundaries.json        repo    (committed, reviewed)  ← author here
~/ctxoptimize/boundaries/*.json     machine (CTX_OPTIMIZE_BOUNDARIES overrides)
embedded defaults                   the 14 shipped rules
```

A malformed file **fails loudly** — a silently dropped rule would make every
later count a lie.

## Hard rules

- **Data, never code.** The adapter door exists for what rules cannot express,
  and taking it needs written justification. A rule that cannot ride the
  engine's single walk is **declared**, not smuggled in as a second pass.
- **Secrets by NAME only.** Never read, print, log or commit a value. The
  `flag.when_identifier_matches` mechanism above is how `KEY|TOKEN|SECRET|
  PASSWORD|_PW` gets `sensitive: true` — set it on every rule that can capture
  a credential name.
- **No network during authoring.** Ground truth is the repo in front of you.
- **Vendored trees excluded by default** — `exclude.path` with `testdata/`,
  `benchmarks/`, `vendor/`, `node_modules/`. This is the precision failure that
  shipped once; do not re-ship it.
- **Never edit a `verified` block to make a check pass.** Numbers move up by
  fixing the rule. Lowering one is a reviewed diff with a reason.

## The standing check

```sh
ctx-optimize boundaries verify      # re-runs each rule's ground truth, reports drift
```

```
process-exec   recall 0.96 → 0.71   ⚠ 14 new exec sites unmatched
http-egress    recall 0.98 → 0.98   ok
```

CI-runnable, and governed like the golden net: **numbers only move up.** A ⚠
means the codebase grew a shape your rule does not see — fix the rule, or
record the new miss deliberately.

## Definition of done

- every rule carries a `verified` block with real numbers
- `ctx-optimize boundaries verify` passes
- `ctx-optimize nodes --kind port` returns what the SURVEY predicted
- the diff is JSON a reviewer can read

## When a rule proves out

A repo rule whose `verified` blocks keep passing across repos is promoted
**verbatim** — evidence and all — into the shipped defaults. Adapters are the
frontier, defaults are the settled core, and the promotion is a reviewed diff,
never a rewrite.
