# Answer quality — six arms, including CodeGraph, GitNexus and CodeGraphContext

Measured 2026-08-15. **Not committed to the site; for owner review.** Raw numbers:
`proof/agent/results-quality-6arm-2026-08-15.json`.

The published scoreboard (`RESULTS-QUALITY.md`, 2026-08-05) has three arms — shell,
ctx-optimize, graphify. This run adds the three remaining local graph competitors
in the arena manifest and runs **all six arms in one session**, so every cell in
the table was measured on the same box, the same day, under the same load. No arm
in this table is carried over from an earlier session.

## Method — identical to the published run, except where stated

- Harness: `proof/agent/run-quality.sh` → `agent.mjs` → `grade.mjs`. Same
  questions (`questions-graded-mux.json`, 12 hand-verified), same model
  (`openai/gpt-4o-mini`, temperature 0), same step cap (12), same deterministic
  grader (no LLM judge, grader never sees the arm).
- **3 runs**, aggregated: **n = 36 answers per arm**.
- Corpus: `gorilla/mux` @ HEAD, shallow clone, 56 files. **One clone per arm** —
  no arm's index sits in another arm's search space.
- **Every index was built before the graded run started.** Indexing time is not
  part of any per-question measurement; all six arms start from a built store.
- Entry points come from `~/ctx-bench-arena/versions.json` at run time, never
  from `PATH`, and that file is copied into each results dir.
- Prompts: each new arm's system prompt is the same shape as the existing ones —
  same instructions, same "call the tool FIRST, shell only to fill a gap", same
  closing sentence. Only the tool name and its verb list differ.
- Secret handling: `OPENROUTER_API_KEY` was read from the environment only. It is
  not in any file, result, or log produced by this run.

| run | started (UTC) | wall | load avg start → end |
|---|---|--:|---|
| 1 | 2026-08-15T16:45:38Z | 262 s | 4.49 2.88 2.73 → 1.66 2.32 2.52 |
| 2 | 2026-08-15T16:50:01Z | 248 s | 1.66 2.32 2.52 → 1.46 2.02 2.35 |
| 3 | 2026-08-15T16:54:10Z | 259 s | 1.46 2.02 2.35 → 3.19 2.49 2.44 |

Host: Darwin 25.4.0 arm64.

## Which build each arm ran — every arm is pin-verified or explicitly unpinned

| arm | tool | resolved SHA / version | status |
|---|---|---|---|
| a | shell (rg/grep/find/sed/cat) | — | n/a, system tools |
| b | ctx-optimize | `a8ce095eb06873ae13ceecfa6bec940f29d9e93f` — `v0.14.0-8-ga8ce095-dirty` | **pin-verified**, see below |
| c | graphify 0.9.25 | `2fa6cd3d5548577f8c5f591b713f0bf80c1af183` | pin-verified |
| d | codegraph | `572d22bfbe82602080e457bec655f72e3314f9ef` | pin-verified |
| e | gitnexus | `91b22676ceaa66ce7941fcb146ffc68ff9a144e6` | pin-verified |
| f | codegraphcontext | `810ea8a9f04fddb2b298b61d752a5e619e19245a` | **explicitly UNPINNED** (manifest `ref: null` = default branch) |

Competitor SHAs verified twice: the value recorded in `versions.json`, and
`git rev-parse HEAD` in each arena clone at run time. They agree.

**Arm b, in full.** The published 2026-08-05 number was produced by a binary
reporting `0.0.0-dev (none, unknown)` — no commit in the version string, so its
source was not provable. That is fixed. This run used `./bin/ctx-optimize`, built
with `task build` (ldflags-stamped; a bare `go build` is what yields
`0.0.0-dev`), and it prints:

```
ctx-optimize v0.14.0-8-ga8ce095-dirty (a8ce095, 2026-08-15T16:44:18Z)
```

The `-dirty` suffix is real but **provably cosmetic**. At build time the only
dirty paths were this session's own harness edits under `proof/agent/` — no `.go`
file, no `go.mod`, no embedded asset. Proof, not assertion:
`go build -trimpath -buildvcs=false` with identical ldflags produced a
**byte-identical** binary from this working tree and from a clean `git worktree`
of `a8ce095` (sha256 `f8c1ca6f2fc1a6cb3c1efbc97668a2f5b34fb130d6cc3cb1bea2b090ab0ad100`).
The only difference without `-buildvcs=false` is Go's own `vcs.modified=true`
stamp, which is metadata, not code.

**A concurrent session was editing this repo during the run** — it committed
`00ae46c` and later added an untracked `internal/store/contenthash.go`. Neither
reached the benchmarked binary: `00ae46c` touches openspec docs only
(`git diff a8ce095..00ae46c -- '*.go' go.mod go.sum` is empty, so the Go source is
identical at both commits), and `contenthash.go` was created at 22:28 IST while
the binary's mtime is 22:14 IST and it was never rebuilt. Confirming this from the
other direction: a rebuild attempted *after* 22:28 no longer reproduces
`f8c1ca6f`, exactly because that new file now enters the build. Arm b measured
`a8ce095`'s Go code.

## Totals — n = 36 answers per arm

| metric | shell | ctx-optimize | graphify | codegraph | gitnexus | codegraphcontext |
|---|--:|--:|--:|--:|--:|--:|
| **correctness** | **35%** | **64%** | **43%** | **55%** | **59%** | **42%** |
| · impact (8 q) | 27% | **75%** | 60% | 66% | 55% | 50% |
| · locate (4 q) | 50% | 42% | 8% | 33% | **67%** | 26% |
| false claims | 0 | 0 | 6 | 0 | 0 | 3 |
| empty answers | 4 | 0 | 0 | 0 | 0 | 0 |
| hit step cap (no answer) | 4 | 0 | 0 | 0 | 0 | 0 |
| tool errors | 53 | 0 | 0 | 0 | 0 | 0 |
| failed runs | 0 | 0 | 0 | 0 | 0 | 0 |
| wall s / run | 63.3 | **30.0** | 40.9 | 33.6 | 43.0 | 41.4 |
| tokens / run | 28,405 | 24,471 | 51,770 | **14,599** | 23,556 | 19,625 |
| cost / run | $0.0048 | $0.0036 | $0.0059 | **$0.0026** | $0.0038 | $0.0032 |
| tool calls / run | 43.0 | **12.0** | 22.3 | 17.7 | 15.7 | 17.3 |
| tool calls / question | 3.58 | **1.00** | 1.86 | 1.47 | 1.31 | 1.44 |

Every arm answered every question. No arm was a non-run; no index failed to build.
`tool errors` counts the model's own malformed shell commands — it is 0 for every
store arm because arms b–f never fell back to the shell at all (zero `run_shell`
calls across all three runs).

## Per question

| q | class | shell | ctx-optimize | graphify | codegraph | gitnexus | codegraphcontext |
|---|---|--:|--:|--:|--:|--:|--:|
| i1 | impact | 0% | 100% | 83% | 75% | 25% | 0% |
| i2 | impact | 50% | 100% | 100% | 100% | 100% | 100% |
| i3 | impact | 0% | 100% | 0% | 0% | 0% | 0% |
| i4 | impact | 0% | 100% | 100% | 100% | 100% | 100% |
| i5 | impact | 50% | 0% | 0% ⚠false | 50% | 0% | 0% ⚠false |
| i6 | impact | 50% | 100% | 100% | 100% | 100% | 100% |
| i7 | impact | 17% | 0% | 0% | 0% | 17% | 0% |
| i8 | impact | 50% | 100% | 100% | 100% | 100% | 100% |
| l1 | locate | 100% | 0% | 0% | 0% | 100% | 0% |
| l2 | locate | 67% ∅ | 100% | 0% | 100% | 100% | 50% |
| l3 | locate | 0% ∅ | 0% | 0% | 0% | 33% | 33% |
| l4 | locate | 33% | 67% | 33% | 33% | 33% | 22% |

⚠false = a forbidden false claim in at least one run · ∅ = at least one empty answer.

## Read

- **The a/b arms reproduce.** Published 2026-08-05: shell 35%, ctx-optimize 67%.
  Here: 35% and 64%. The 3-point move is inside the run-to-run swing the published
  file already documents (per-run totals moved by up to 20 points there).
- **The new competitors are not weak.** GitNexus (59%) and CodeGraph (55%) both
  land between graphify and ctx-optimize, and **GitNexus beats ctx-optimize on
  `locate` questions, 67% vs 42%** — its `query` verb returns execution flows with
  file:line, which is exactly where our lexical `query` misfires (defect D-B in
  `RESULTS-QUALITY.md`). Our lead is now specifically `impact` (75% vs 66% / 55%)
  and specifically cost: 1.00 tool calls per question against 1.31–1.86, and the
  lowest wall clock of any arm.
- **CodeGraph is the cheapest arm** — 14,599 tokens and $0.0026 per run, below
  ctx-optimize. If a token claim is ever made against this field, it must not be
  made against CodeGraph.
- **i3 and i7 are a shared blind spot, not ours alone.** Every graph tool scores
  0% on i3, and only shell and GitNexus score above 0 on i7 (17% each). These are
  the ambiguous-method-name questions (`Match` defined 8 times).
- **CodeGraphContext is the weakest graph arm (42%)** and emits false claims (3),
  including "no callers found" for a symbol with two real callers.

## Caveats — read before quoting any number

1. **graphify 42–43% here is NOT a repeat of the published 40%.** The published
   run used whatever was on `PATH` — graphify **0.9.12**. This run deliberately
   used the arena's pinned **0.9.25**. These are two different builds of the tool,
   not two samples of one measurement, and the difference must not be read as
   run-to-run noise.
2. **codegraphcontext is unpinned**, by manifest: `tools.json` carries
   `ref: null`, so `810ea8a` is whatever the default branch resolved to on the day
   the arena was built. Report it as unpinned until the manifest pins it.
3. **Two competitor clones were not clean checkouts.** `codegraph` is missing
   `CLAUDE.md`; `gitnexus` is missing `AGENTS.md` and `CLAUDE.md` — deleted by an
   earlier corpus-cleanup sweep, not by this run. Both are believed not to affect
   the CLI: the deleted files are agent-instruction markdown, they are not
   imported, executed, or read by either tool's indexer or query path, and both
   tools built and answered normally. It is stated here because "believed not to
   affect" is weaker than "clean checkout", and the reader should know which one
   they have.
4. **Two harness adaptations were needed for the new arms, and both cut in the
   competitors' favour** (documented in `agent.mjs`):
   - codegraphcontext prints its result table on **stderr**, and gitnexus exits 1
     with a JSON "symbol not found" body. `runExternal` therefore returns both
     streams and does not treat a non-zero exit with output as a tool error — the
     same reasoning as grep exit 1 in `runShell`. Without this, both arms would
     have been scored on empty output.
   - codegraphcontext's `rich` tables ellipsise the Location column — where its
     file:line lives — at terminal width, so it runs with `COLUMNS=400`. Otherwise
     the grader would be scoring the terminal, not the tool.
   - Only boilerplate is filtered: gitnexus's per-call pino warning about the
     optional VECTOR extension, and cgc's "Services initialized." preamble. No
     answer content is filtered.
5. **gitnexus needs an isolated `$HOME`.** It keeps a global registry and refuses
   to answer when several indexed repos share a name, so the arm gets its own
   HOME — nothing else on the box can be read as, or leak into, its index.
6. Everything caveated in `RESULTS-QUALITY.md` still holds: small well-named
   corpus, cheap model, n=3, and part of the shell gap is gpt-4o-mini's weakness
   rather than a ripgrep ceiling. **Re-run on a frontier model before publishing
   a headline number.**

## Reproduce

```sh
export OPENROUTER_API_KEY=...        # read from env only, never logged
task build                           # NOT `go build` — that yields 0.0.0-dev
PATH="$HOME/ctx-bench-arena/tools/graphify/.venv/bin:$PATH" \
  proof/agent/run-quality.sh --arms "a b c d e f" \
    --bin "$PWD/bin/ctx-optimize" --out /tmp/run1
# ... repeat for run2, run3, then:
node proof/agent/grade.mjs --results /tmp/run1,/tmp/run2,/tmp/run3 \
  --name gorilla-mux-graded --questions proof/agent/questions-graded-mux.json
```
