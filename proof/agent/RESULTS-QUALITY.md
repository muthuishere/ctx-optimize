# Answer quality — is the store's answer RIGHT, not just cheap?

Measured 2026-08-05. **Not committed; for review.** Reproduce with
`proof/agent/run-quality.sh` (see "How to reproduce" at the bottom).

## Why this measurement exists

The speed benchmarks are settled and ripgrep is *faster* than ctx-optimize at raw
lookup (rg 11ms vs 12ms on a small repo; rg 3,022ms vs 4,039ms on the Linux
kernel). The positioning rests on a different claim, which until now was an
assertion:

> ripgrep returns matching LINES; ctx-optimize returns a RESOLVED SYMBOL with
> callers, callees and file:line. rg cannot answer "who calls this function".

`docs/CRITIQUE.md` already records that the universal token-savings claim was
measured and **killed** (S16: Claude Code −0.2%, Codex +3.0%). It also records
what survived: *impact-answer correctness*. This run measures exactly that.

## Method

- Corpus: `gorilla/mux` @ HEAD (56 files) — small, modern, well-named Go. This is
  the **hardest terrain for us**: the case where plain grep is already strong.
- Arms (`proof/agent/agent.mjs`, unchanged behaviour): **a** shell only
  (grep/rg/find/sed/cat) · **b** ctx-optimize store first · **c** graphify graph
  first. Same model, temperature 0, same prompts.
- **One clone per arm** (new) so no arm's artifacts sit in another arm's search
  space — arm a's grep must not be able to read `graphify-out/` or `.ctxoptimize/`.
- Model: `openai/gpt-4o-mini` via OpenRouter. Tokens/cost are OpenRouter's own
  accounting (`usage.include=true`), not estimates.
- Questions: `proof/agent/questions-graded-mux.json`. 8 `impact` ("who calls X")
  + 4 `locate`. Every expected fact was verified by hand with ripgrep against the
  checked-out tree and carries its `truth` file:line in the JSON.
- Grading: `proof/agent/grade.mjs` — **deterministic, no LLM judge**. It scores
  the agent's FINAL ANSWER for hand-verified facts, plus `forbid` patterns that
  catch false claims ("no callers", or naming a non-caller). Identical rule for
  every arm; the grader never sees which arm produced the answer.
- **3 full runs** aggregated (n = 36 answers per arm). The agent is not
  deterministic even at temperature 0 — tool-call order varies — and per-run
  totals swung by up to 20 points, so a single run is noise.

# Answer-quality benchmark — gorilla-mux-graded

Deterministic grading: each question's answer is checked for hand-verified facts (function names / files, sourced by ripgrep against the checked-out tree). No LLM judge. Same rule for every arm. **3 run(s)** aggregated; per-question cells show the mean, with each run's score in parentheses when they disagree.

## Per question

| q | class | shell | ctx-optimize | graphify |
|---|---|--:|--:|--:|
| i1 | impact | 0% | 100% | 17% (0/0/50) |
| i2 | impact | 50% | 100% | 50% |
| i3 | impact | 0% | 100% | 0% |
| i4 | impact | 0% | 100% | 50% |
| i5 | impact | 50% | 0% | 0% ⚠false |
| i6 | impact | 50% | 100% | 100% |
| i7 | impact | 33% (50/50/0) | 33% (0/100/0) | 17% (50/0/0) |
| i8 | impact | 50% | 100% | 100% |
| l1 | locate | 67% (100/100/0) | 0% | 0% |
| l2 | locate | 67% (100/100/0) ∅ | 100% | 100% |
| l3 | locate | 0% ∅ | 0% | 11% (0/33/0) |
| l4 | locate | 56% (67/33/67) | 67% | 33% |

## Totals

| metric | shell | ctx-optimize | graphify |
|---|--:|--:|--:|
| **correctness** | **35%** | **67%** | **40%** |
|   · impact (8 q) | 29% | 79% | 42% |
|   · locate (4 q) | 47% | 42% | 36% |
| false claims | 0 | 0 | 1 |
| empty answers | 4 | 0 | 0 |
| hit step cap (no answer) | 3 | 0 | 0 |
| tool errors | 44 | 0 | 0 |
| failed runs | 0 | 0 | 0 |
| wall s / run | 65.5 | 40.1 | 43.1 |
| tokens / run | 29361 | 27822 | 56229 |
| cost / run | $0.0051 | $0.0040 | $0.0070 |
| steps / run | 42.7 | 15.0 | 26.0 |

## Misses (what each arm failed to find)

**shell**

- `i1` missed: Route.Host, Route.Path, Route.PathPrefix, Route.Queries
- `i2` missed: addRegexpMatcher is the sole caller
- `i3` missed: methodMatcher.Match, schemeMatcher.Match
- `i4` missed: Router.ServeHTTP, SetURLVars
- `i5` missed: getAllMethodsForRoute
- `i6` missed: Router.ServeHTTP
- `i7` missed: Route.Match
- `i7` missed: Route.Match, cites route.go
- `i8` missed: CORSMethodMiddleware
- `l1` missed: newRouteRegexp, cites regexp.go
- `l2` missed: Router.ServeHTTP, cites mux.go
- `l3` missed: setMatch extracts vars, Vars accessor, request context helper
- `l4` missed: Router.Match
- `l4` missed: Router.Match, NotFoundHandler
- `l4` missed: NotFoundHandler

**ctx-optimize**

- `i5` missed: Router.Match, getAllMethodsForRoute
- `i7` missed: Route.Match, cites route.go
- `l1` missed: newRouteRegexp, cites regexp.go
- `l3` missed: setMatch extracts vars, Vars accessor, request context helper
- `l4` missed: NotFoundHandler

**graphify**

- `i1` missed: Route.Host, Route.Path, Route.PathPrefix, Route.Queries
- `i1` missed: Route.Host, Route.Path
- `i2` missed: cites route.go
- `i3` missed: methodMatcher.Match, schemeMatcher.Match
- `i4` missed: SetURLVars
- `i5` missed: Router.Match, getAllMethodsForRoute
- `i5` missed: Router.Match, getAllMethodsForRoute · **false claim: names an unrelated helper as a caller**
- `i7` missed: Route.Match
- `i7` missed: Route.Match, cites route.go
- `l1` missed: newRouteRegexp, cites regexp.go
- `l3` missed: setMatch extracts vars, Vars accessor, request context helper
- `l3` missed: setMatch extracts vars, request context helper
- `l4` missed: Router.Match, NotFoundHandler


## The mechanism, in one pair of transcripts

This is the whole claim, from the recorded `trace` of q `i4` ("which functions
call `requestWithVars`?"), run 3:

**arm a — shell.** One call, `grep -r 'requestWithVars' .`, 189 bytes back:

```
The functions that call `requestWithVars` are:
 1. In `mux.go` at line 4: `req = requestWithVars(req, match.Vars)`
 2. In `test_helpers.go` at line 3: `return requestWithVars(r, val)`
```

Grep gave it the matching **lines**. It found the right files and then could not
name a single caller — and invented the line numbers (the real sites are
mux.go:209 and test_helpers.go:18; "line 4" and "line 3" are the ordinal
positions in grep's output). Scored 0/2.

**arm b — ctx-optimize.** One call, `affected requestWithVars`, 3552 bytes back:

```
The following functions call `requestWithVars`:
 1. **Router.ServeHTTP** — mux.go, lines 188-229
 2. **SetURLVars**       — test_helpers.go, lines 17-19
 3. **ExampleSetURLVars** — mux_test.go, lines 2549-2556
```

Correct, with the enclosing symbol and a real line range. Scored 2/2.

The same shape repeats on i1 (shell: "Route (method not specified) — route.go:7,
8, 9, 10"; store: Host / Path / PathPrefix / Queries with signatures) and on i3.

## Honest caveats — read these before quoting any number

1. **Part of the shell gap is model weakness, not a tool ceiling.** `grep -n`
   would have given real line numbers, and a stronger agent would then read the
   file and walk up to the enclosing `func`. gpt-4o-mini mostly did not: it ran
   `grep -r` without `-n` and answered from the lines it got. What is fairly
   measured here is *the answer a cheap agent actually produces*, not the best
   answer grep could support. **Re-run on a frontier model before publishing a
   headline number.**
2. **One shell failure is entirely the model's fault.** On `l3` arm a burned 15
   steps on malformed commands (`grep -rnw 'func' -e 'path'` — treats `func` as
   a filename), hit the step cap and returned an empty answer. That is a
   gpt-4o-mini failure, not a ripgrep failure, and it is why arm a's `locate`
   score is depressed. It is recorded, not laundered into a win.
3. **`tool errors` counts the model's own broken commands**, and only exit>1 —
   grep exit 1 ("no matches") is now reported as `[no matches]`, not an error
   (fixed in `agent.mjs` during this session; earlier numbers overcounted).
4. **Small, well-named corpus, cheap model, n=3.** No claim about large or
   legacy repos is supported by this file.

## Where ctx-optimize LOSES — the real defects this surfaced

- **D-A: ambiguous method names destroy the call graph (q i5, i7 — 0%).**
  `Match` is defined 8 times in mux (Route, Router, routeRegexp, headerMatcher,
  headerRegexMatcher, MatcherFunc, methodMatcher, schemeMatcher). Per the
  AMBIGUOUS-shortlist rule those call edges are filtered out, so
  `affected Route.Match` returns **only the containing file**:

  ```
  changing Route.Match impacts 1 nodes (depth 2):
    d1 route.go  [file]  via contains on route.go::Route.Match
  ```

  The model then answered "This is the only caller identified" — a confidently
  useless answer. Ground truth: `Router.Match` (mux.go:153) and
  `getAllMethodsForRoute` (middleware.go:79). Same failure on
  `routeRegexpGroup.setMatch` (real caller `Route.Match`, route.go:112), where
  the model concluded "No external calls to setMatch were found in the
  knowledge store." **This is the single highest-value fix the benchmark found:
  the store is silent exactly where the receiver type would disambiguate.**
  Note this is the exact question already shipped as q4 in
  `proof/agent/questions.json`, and it has been scoring 0 unnoticed because
  nothing graded the answer.
- **D-B: `query` ranking misfires on conceptual phrasing (q l1, l3 — 0%).**
  `query "/users/{id} parsed into a regular expression"` ranked `Route.Use`
  (middleware.go) first; the true answer `newRouteRegexp` (regexp.go:41) was
  not what the model took. Plain grep got l1 right in 2 of 3 runs. On l3
  (`query "path variables URL handler"`) the top hits were the URL *building*
  path (`replaceURLPath`, `Route.URL`, `Route.URLPath`) rather than the
  extraction path (`setMatch`, `Vars`, `requestWithVars`) — a URL-build vs
  URL-match confusion. **The store's advantage is graph verbs (`affected`,
  `card`), not lexical `query`; on `locate` questions it is at or below grep.**

## Read

The narrow claim is **supported on this corpus**: on "who calls this function",
ctx-optimize's graph verbs produced 79% of the verified facts vs 29% for shell
and 42% for graphify, at 1.25 tool calls per question vs 3.6 (shell), and the
mechanism in the transcripts is exactly the asserted one — grep hands over lines,
the store hands over resolved symbols. The broad claim "ctx-optimize gives better
answers" is **not** supported: on `locate` questions it is level with or worse
than grep (42% vs 47%), and two of its four losses are its own defects, not
terrain.

Honest headline: *ctx-optimize answers impact questions; grep answers location
questions.* Do not widen it until D-A is fixed and this is re-run on a frontier
model and a legacy corpus.

## How to reproduce

```sh
export OPENROUTER_API_KEY=...        # read from env only, never logged
proof/agent/run-quality.sh           # 12 questions x 3 arms, ~$0.016 per run
# aggregate several runs under one grading rule:
node proof/agent/grade.mjs --results run1,run2,run3 \
  --name gorilla-mux-graded --questions proof/agent/questions-graded-mux.json
```
