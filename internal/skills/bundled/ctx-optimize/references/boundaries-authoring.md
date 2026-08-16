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

**This law is enforced by REVIEW, not by the loader — know that before you
lean on it.** Measured against the shipped binary: a rule with no `verified`
block at all loads without complaint, and an omitted `tier` defaults to
**EXTRACTED** — so an entirely unmeasured rule silently claims the *highest*
confidence tier. Never omit `tier`. Set it from your numbers, and treat a
missing `verified` block in a diff as a blocking review comment, because
nothing downstream will catch it for you.

**Hold this standard against yourself.** The rules that ship in this binary
were measured and most were *downgraded* by their own numbers: the `env-*`
rules landed at **INFERRED** (recall 0.81–0.95 — `os.Getenv(varName)` with a
variable name is unmatchable), and the `process-*` rules at **AMBIGUOUS**
(recall 0.65). Honest numbers that demote your own rule are the point, not a
failure.

**And a recorded number can itself be wrong — check before you copy the
pattern.** `process-py` ships recording recall **0.00**, which reads as "the
Python spawn rule matches nothing". Run it and it emits correctly:
`subprocess.run(["git", "status"])` yields a `process.exec` port. The 0.00 is a
ground-truth artifact — the raw sweep counted `subprocess.` occurrences inside
comments and strings that are not call sites at all. A wrong denominator does
not merely mis-tier a rule; published, it advertises a working capability as
broken. This is the same class of error as the inflated denominator above, and
it is why step 3 says to sanity-check the count.

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
ctx-optimize boundaries                          # the summary — start here
ctx-optimize deps                                # what this repo talks to by dependency
ctx-optimize nodes --kind port                   # every boundary captured
ctx-optimize nodes --kind port --where direction=provides   # the routes we serve
ctx-optimize edges --relation consumes
ctx-optimize query "http client wrapper" --json
```

There is **no `route` kind** — a served route is a `port` with
`direction=provides`. `--kind` is an OPEN vocabulary (adapters mint their own),
so `nodes --kind route` does not error and still exits 0 — but it no longer
answers in silence: an empty result whose value exists nowhere in the store now
DISCLOSES that, and names the kinds that do exist.

```
$ ctx-optimize nodes --kind route
(0 nodes)  — no node in this store has kind "route";
            kinds present: … port …
```

Read that note. When it is ABSENT, the value is real and the empty result is a
genuine "nothing matched". When it is present, you asked an impossible question
— cross-check with `nodes --kind port` before concluding a repo serves nothing.

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
ctx-optimize search 'os\.Getenv\(' --ext .go,.ts --count
```

`search` takes `--ext`, `--count`, `--files` and `--ndjson`. **It has no path
filter** — `--path` is the global repo-root flag, so `search … --path internal/`
silently re-roots the sweep and prints `0`. A zero denominator is the most
dangerous output in this whole loop: it makes recall undefined, and an
inattentive author records 1.00. **Sanity-check every ground-truth count
against a number you expect before you divide by it.**

Use `search`, not `rg`/`grep`: it is cross-OS (a teammate on Windows
reproduces your number) and it walks **the same files the extractor walks**, so
the denominator is honest. External `rg`/`grep` are optional cross-checks only
— nothing may depend on them.

**Ground truth must be broader than your rule.** Count `os\.Getenv\(` — every
call site — not `os\.Getenv\("[A-Z_]+"\)`, which is your own capture logic
wearing a disguise and will always report recall 1.00.

**But broader is not the same as sloppy, and the wrapper case proves it.** A
count of `api(Get|Post)\(` over a repo whose wrapper exports those names
returns the CALL SITES *plus the two function DEFINITIONS*. Measured on a
5-call-site fixture: ground truth 7, matched 5, recall 0.71 — a rule with
perfect recall demoted from EXTRACTED to INFERRED by two lines that are not
boundaries at all. Definitions, imports, re-exports and test doubles all
inflate the denominator. Subtract them explicitly and say so in
`known_misses`, or ground on a pattern that cannot match a definition — one
that requires a quote after the paren catches a literal argument but not
`apiGet(p: string)`.

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

This is the shipped `env-go` rule, copied. Note it declares **no** `metadata`:
only the `network.*` rules carry `otel.server.address` / `otel.http.route`,
because those keys mean something a tracing backend can join on. An env-var
name is not a server address — putting one there would poison that join.

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
      "ast": [
        { "shape": "call", "name": "Getenv", "receiver": "os", "arg": 0 }
      ],
      "flag": {
        "when_identifier_matches": "KEY|TOKEN|SECRET|PASSWORD|_PW",
        "set": { "sensitive": "true" }
      },
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

### Rules match the AST, not the bytes (ADR 2026-08-14)

`ast` is the matcher. A rule names a node **shape**, and it is evaluated inside
the code extractor's existing walk — the file is already parsed, so a rule
costs a map probe rather than another pass over the bytes. Six shapes, and
they were chosen by enumerating what the shipped rules actually need:

```json
{ "shape": "call",       "name": "Getenv",  "receiver": "os", "arg": 0 }
{ "shape": "member",     "path": ["process", "env"] }
{ "shape": "subscript",  "path": ["os", "environ"] }
{ "shape": "annotation", "name": "GetMapping", "arg": 0, "named_arg": ["value", "path"] }
{ "shape": "new",        "name": "WebSocket", "arg": 0 }
{ "shape": "literal",    "url_scheme": ["http", "https"], "take": "host" }
```

**The rule that makes this worth doing:** an argument that is not a string
literal is NOT a miss. `os.Getenv(name)` is a certain env read with an
uncertain value, so it emits with `resolved: dynamic` at AMBIGUOUS instead of
vanishing. A regex saw only text, so it saw nothing there, and a repo that
spawned processes reported that it spawned none — a lie the AST lane fixed.
(The `process-py` recall still recorded in `defaults.json` predates that fix
and is measured against a denominator full of comments; see the warning at the
top of this file before you trust any single recorded number.)

Other `ast` fields: `receiver_any_of` / `receiver_suffix` (constrain the
receiver — better precision than route packs, which key on the callee's last
identifier only, so `os.Getenv` would also fire on a local `Getenv()`),
`arg_in_list` (read the first element of a list argument, for
`subprocess.run(["git"])`), `in_decorator` (a route is `@app.get("/x")`; the
same call at runtime is not), and `identifier_prefix` (routes require `/`).

### `scan: "raw"` — the declared escape hatch

`match` (regex over raw bytes) still exists, and a rule that uses it MUST also
declare `"scan": "raw"`. It is for boundaries in files **no grammar parses** —
`.env`, `.txt`, and languages that ship as a grammar pack rather than embedded
(a machine without the Kotlin pack cannot parse `.kt` at all, so
`routes-kotlin` is a raw rule). This is the honest limit of an AST lane, and
the ADR requires it be *declared* rather than silently empty.

A raw rule in full — note `scan` and `match` together, and that the
`known_misses` say plainly why it is not an `ast` rule:

```json
{
  "id": "routes-kotlin",
  "transport": "network.http",
  "direction": "provides",
  "when":    { "ext": [".kt"] },
  "exclude": { "path": ["testdata/", "benchmarks/"] },
  "scan": "raw",
  "match": [
    { "re": "@(?:Get|Post|Put|Delete|Patch|Request)Mapping\\(\\s*(?:value\\s*=\\s*)?\"(/[^\"]*)\"", "identifier": 1 }
  ],
  "metadata": { "otel.http.route": "$identifier" },
  "tier": "INFERRED"
}
```

Regex also still owns two jobs it is right for: the `search` verb, and
`verified.ground_truth` — which **must** stay independent of the capture
mechanism. An AST rule verified by an AST rule scores 1.00 by construction.

Field notes that are easy to get wrong:

- `exclude` is **top-level**, not inside `when`. `when` holds only `ext`.
- Declare `ast` OR `match`+`scan:"raw"` — never both; the loader rejects it.
- `ast[].arg` is a **0-based argument position**. (`match[].identifier`, the
  raw lane's capture group, is 1-based. `identifier: 0` is NOT rejected — it is
  the zero value and reads as group 1, so a rule meaning "the whole match"
  quietly gets the first group instead. Always write the group you mean.)
- `direction` is `provides` (this repo serves it) or `consumes` (this repo
  calls it). **`scope` is NOT yours to set** — the engine computes it, by JOIN:
  a consumed identifier that matches a `provides` port in the same gather gets
  `scope: internal`, and **a miss gets no `scope` key at all**. There is no
  `external` value. It used to write `external` on every miss, which measured
  as 56/56 on this repo and 163/163 across a seven-module monorepo — a constant
  dressed as a computation (ADR 2026-08-15-scope-join-broken). Absence means
  "not proven internal"; never report it as evidence that a call is
  third-party.

  The join only has an input when both sides share a namespace. Every shipped
  `consumes` rule on `network.http` takes a **host**, and every shipped
  `provides` rule takes a **route path**, so the default set produces no
  internals at all. If you want them, author a rule whose consumed identifier
  IS a path — a same-origin `fetch("/orders")` matcher is the worked example,
  and it lives in `internal/golden/testdata/repos/boundary/.ctxoptimize/`.
- `metadata` values may interpolate `$identifier`. Open keys must be
  **namespaced** (`otel.*`, `pack.*`, `org.*`) and this one IS enforced
  fail-closed — a bare `"owner": "me"` fails the batch loudly at the schema
  door, naming the key.
- **The reserved keys are NOT enforced, and writing one silently overrules the
  rule.** `direction`, `transport`, `identifier`, `scope`, `sensitive`,
  `resolved`, `raw`, `producer` are applied from `metadata` *after* the engine
  sets them. Measured: a rule declaring `"direction": "consumes"` with
  `"metadata": {"direction": "provides"}` emits a port that `boundaries` lists
  under **PROVIDES** — the summary reports the exact opposite of the truth.
  Never put a reserved key in `metadata`.
- **First rule to mint an identifier owns its metadata.** Ports dedup by
  `transport` + `identifier`; a later rule matching the same one contributes
  its sites but its `metadata` is dropped. If your metadata does not appear,
  check whether a shipped rule created that port first.
- `transport` is **not a closed vocabulary**: a typo'd `carrier.pigeon` is
  accepted and renders as its own group in the `boundaries` summary,
  indistinguishable from a real transport. Copy the string from an existing
  rule rather than typing it.
- `tier` omitted falls back to your EVIDENCE: a rule that ships a `verified`
  block defaults to EXTRACTED, and an **unmeasured** rule (no `verified`)
  defaults to **AMBIGUOUS** — the highest confidence is never what a rule gets
  for providing no evidence at all (ADR 2026-08-15-authoring-loop-unenforced
  D1). **Always set it explicitly** from your measurement.
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
embedded defaults                   the 16 shipped rules
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
