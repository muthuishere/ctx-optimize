# ADR — answer quality: definitions must win over import stubs & tests

Status: **ACCEPTED** — 2026-07-24 (owner: "will work on it first"). Decisions:
F1+F2 build now; **F3 already SHIPPED in v0.9.0** as `--include-content` on
query/card (on-demand hydration, nothing stored — resolves Q1); **F4 verified
complete** on the flask corpus (`src/flask/helpers.py::url_for [function]
L200-L251` is a first-class decl node — no extraction bug, purely resolve/rank);
Q2 = demote tests for symbol queries unless the query itself mentions tests;
Q3 = import stubs never the primary answer, kept as secondary context.
Both failures re-reproduced on v0.9.0 before coding (card → module://url_for
stub; query ranks test + 2 stubs above the app.py:1102 definition).

## Evidence (the small-model quality judge, 2026-07-24)

`ctx-optimize query` scored **0.79 correctness at ~6KB context** — within 3–7% of
CodeGraph (0.86) at **⅓ the tokens** (2.8× better correctness-per-byte). But
`ctx-optimize card` scored **0.66** — the lowest arm. Root cause is visible in the
raw capture for `card url_for` on Flask:

```
url_for  [module]  module://url_for
  imports ← (3): auth.py, blog.py, test_converters.py
```

It returned the IMPORT STUB, not the definition. CodeGraph went straight to
`helpers.py:200`. Three distinct causes, two are real bugs, one is a tradeoff.

## Root causes

**R1 — import-stub nodes hijack bare-symbol lookup (BUG, biggest hit).**
An imported name is modeled as a synthetic `module://<name>` node whose label is
the bare string (`url_for`). `analyze.Resolve` (id > label > fuzzy) matches that
stub on an exact-label tie and returns it — no signature, no body, no file:line —
over the real decl. This drags `card` to 0.66.

**R2 — the lexical ranker floats tests above the definition (BUG).**
For `query "where is url_for defined"`, top hits were `test_url_for_...`,
`test_path_is_url`; the real `Flask.url_for [function] app.py:1102` sat *below* the
import stubs. "url_for" occurs more in test names, so IDF/lexical ranking rewards
tests over the canonical definition.

**R3 — terse output loses "coverage" points (TRADEOFF, not a bug — KEEP IT).**
The judge's coverage metric rewards full body + docstring; CodeGraph/ast-grep dump
18–21KB, ctx shows signature + doc snippet + callers in 1–6KB. We lose coverage
*because* we're token-efficient — the same reason we win efficiency. **Default stays
terse.** Owner decision: keep the tradeoff, add an opt-in way to get the full body.

## Fixes

**F1 — decls beat import stubs (R1).** In `analyze.Resolve` and query ranking, a
real declaration node outranks a `module://` import-target on any label tie. `card`
returns the definition as the primary result; "also imported in N files" becomes a
secondary line, not THE answer. Consider suppressing `module://` stubs from top
hits entirely unless the query is explicitly about imports.

**F2 — definition-boost / test-demote (R2).** For a symbol-intent query, boost the
canonical definition of the queried name; demote test-only nodes (heuristic:
`tests/` dir, `test_*`, `*_test.go`, `*Test`, `*.spec.ts`) UNLESS the query is about
tests. The definition should be the top hit for "where is X".

**F3 — DEFERRED (owner: discuss later; NOT in this change) — but it is the
STRATEGIC move, per measured evidence below.** Keep terse as the default (protects
the efficiency win). Direction: don't STORE bodies and don't add a separate verb —
let `query`/`card` **hydrate the body on demand from the source file** using the
already-stored `file:line` range (the `verify.go` slice reader already does exactly
this read). Store stays tiny; the body is read from disk only when asked
(`--full`/`--body`), or only for the top hit. **Open blocker:** freshness — a
changed file makes the stored range stale, so hydration must verify the slice
(reuse `verify` / the freshness gate) or depend on the autosync ADR.

**Why F3 matters more than it looks — MEASURED 2026-07-24 (multilang bench):**
the footprint win and the coverage loss are the SAME design choice. Rival stores
hold **verbatim source + docstrings** (proven: CodeGraph's SQLite contains
`return i.proceed();` + Javadoc; GitNexus's DB contains file text incl. README) —
that is why their stores are big (postgres 183MB–1GB vs our 44MB) AND why they
scored **coverage 1.0** in the quality judge (they have the body to return). Our
store holds **0 verbatim source lines** — pointers only — hence tiny store AND
coverage 0.79. One decision drives both. F3 (file-hydration) closes the coverage
gap WITHOUT surrendering the footprint win — the result no rival has: **smallest
store AND full-body answers**, because we point at source instead of duplicating
it. They pay store bytes for coverage; we pay one cheap file read. That is the
positioning F3 unlocks — which is exactly why it deserves its own careful ADR, not
a bolt-on.

**F4 — extraction completeness (verify per-language via the multi-lang bench).**
Confirm module-level defs (python `def url_for` in `helpers.py`) are first-class
decl nodes, not shadowed by the import stub. If a language drops standalone defs,
that's an extraction bug, not just ranking. The 6-language corpus surfaces this.

## Validation — the multi-language benchmark IS the test harness

The `bench/multilang` corpus set (c/python/go/java/c#/ts) re-runs the small-model
quality judge **before/after each fix**. Gate: mean correctness + coverage must go
UP per language, and **perf/footprint must NOT regress** (the owner's "if it stops
performance, do differently" rule). Add a per-language quality floor to the golden
scoreboard so a future change can't silently regress it.

## Guardrail (perf must hold)

- F1/F2 are ranking/resolve changes — O(nodes), cheap, no gather-time cost.
- F3 is opt-in — zero default-path bytes, so the efficiency win and footprint are
  untouched.
- If ANY fix measurably regresses cold-gather or query latency on the multi-lang
  corpora, we do it differently (don't ship a quality win that costs the speed
  story). Benchmarked, not assumed.

## Open questions

1. F3 surface: `card --full` flag, or a dedicated `def`/`source` verb, or both?
2. Test-demote: always demote tests for symbol queries, or only when intent is
   clearly "definition"? (Lean: demote for symbol-name queries; keep for
   free-text that mentions "test".)
3. Should `module://` stubs be dropped from `card` output entirely, or kept as a
   secondary "imported in" line? (Lean: secondary line, never the primary.)

## Success check

- `card url_for` on Flask returns the DEFINITION (file:line + signature) as the
  primary result, not `module://url_for`.
- "where is X defined" ranks the definition above tests and import stubs.
- Multi-language judge: correctness/coverage up across all 6 languages; perf floor
  unchanged. `card --full`/`source` gives full body when asked, default stays terse.
