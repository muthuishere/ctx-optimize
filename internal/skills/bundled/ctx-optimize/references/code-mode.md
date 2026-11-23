# Code Mode — one program instead of many verb calls

`ctx-optimize code` runs a small JavaScript-subset program in ONE process. The six
READ-ONLY verbs are operations; nothing can gather, sync or write the store.
Measured on this repo (query, card x5, change-plan; best of 5): seven processes
63 ms and 22,538 bytes back; one `code run` 29 ms and 825 bytes back — the win is
**27x less output**, because the program filters before anything reaches you.

| operation | wraps | args (all strings) |
|---|---|---|
| `ops.ctx.query` | `query` | `["<2-4 terms>", "--budget", "N"]` |
| `ops.ctx.card` | `card` | `["<exact label>"]` |
| `ops.ctx.change_plan` | `change-plan` | `["<symbol>"]` |
| `ops.ctx.affected` | `affected` | `["<symbol>", "--depth", "2"]` |
| `ops.ctx.path` | `path` | `["<a>", "<b>"]` |
| `ops.ctx.hubs` | `hubs` | `["--top", "10"]` |

Every call is `await ops.ctx.<op>({args: [...]})` and **resolves to the verb's parsed JSON
object**. Do not pass `--json` (it is added). The program's LAST EXPRESSION is its result.

## When to use it — and when not
- USE for 3+ dependent calls (query, then card on the hits, then change-plan), a fan-out over
  many symbols, or anything where you only need a filtered slice of several answers.
- DON'T for one question — a plain `query`/`card` is simpler. DON'T for gathering/syncing/
  literal sweeps: `add`, `sync`, `search`, `boundaries` are not operations; run them normally.

## The recipe (do these in order)
1. Write the program so it **returns only what you will cite**: labels, signatures, counts,
   `file:line` — never the whole verb output. That is where the savings come from.
2. For anything non-trivial run `ctx-optimize code check <file|->` first: it validates syntax
   and operation names and runs nothing (a typo prints `did you mean "ctx.query"?`).
3. Run once per task: `ctx-optimize code run - --input '{"terms":"..."}' --json <<'JS' ... JS`.
   Parameters go in `--input` (global `input`), not string-spliced into the source.
4. Read `ok`. Cite the returned `file:line` as usual; the same discipline as the plain verbs applies.

## Patterns
Chain (find, inspect in parallel, plan the edit):
```
const q = await ops.ctx.query({args: [input.terms, "--budget", "1500"]});
const hits = q.hits.slice(0, 5);
const cards = await Promise.all(hits.map(h => ops.ctx.card({args: [h.node.label]})));
const plan = await ops.ctx.change_plan({args: [hits[0].node.label]});
({symbols: cards.map(c => ({label: c.node.label, sig: c.signature})), plan})
```
Tolerate misses in a fan-out (one throw would otherwise reject the whole `Promise.all`):
```
const out = await Promise.all(input.symbols.map(async n => {
  try { const c = await ops.ctx.card({args: [n]}); return {name: n, sig: c.signature}; }
  catch (e) { return {name: n, error: String(e.message).split("\n")[0]}; }
}));
({found: out.filter(x => !x.error), missing: out.filter(x => x.error)})
```

## Errors you will see (they are errors on purpose)
- A **mistyped flag** throws: `unknown flag --bogus for "hubs" ... accepted: ...`.
- A **name not found exactly** throws: `"X" was not found exactly; the nearest match is "..."`.
  The CLI would have silently answered with the near node; a program cannot see that stderr
  note, so Code Mode refuses. Pass `"--fuzzy"` in `args` only if a near match is acceptable.
- `check` catches syntax errors and unknown operations (exit 2). Argument types (`args` must be
  strings) and unknown properties are rejected when the call runs: `args[0] must be string`.
- Exit codes: 0 ok, 1 ran and failed, 2 nothing ran (diagnostics or bad `--input`).
  `--json` prints one object `{ok, value, error?, diagnostics?, ms}`.
- The abstention rules still hold: `card`/`affected` on a METHOD can carry `unattributed
  callers`, so a caller list inside a program is a FLOOR unless you pass `--include-ambiguous`.

## Gotchas
- Operation names have no hyphen (`change_plan`): `ops.ctx.change-plan` parses as subtraction
  and `check` reports it as zero operations, not an error.
- Discover with `code operations`, `code describe <op>`, `code docs`. Claude Code Code Mode:
  call `ctx-optimize code run - --input ... --json` once per task.
