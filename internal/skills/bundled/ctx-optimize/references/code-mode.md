# Code Mode — one program instead of many verb calls

`ctx-optimize code` runs a small JavaScript-subset program in ONE process. The six
READ-ONLY verbs are operations; nothing can gather, sync or write the store.

| operation | wraps | args |
|---|---|---|
| `ops.ctx.query` | `query` | `["<2-4 terms>", "--budget", "N"]` |
| `ops.ctx.card` | `card` | `["<label>"]` |
| `ops.ctx.change_plan` | `change-plan` | `["<symbol>"]` |
| `ops.ctx.affected` | `affected` | `["<symbol>", "--depth", "2"]` |
| `ops.ctx.path` | `path` | `["<a>", "<b>"]` |
| `ops.ctx.hubs` | `hubs` | `["--top", "10"]` |

Every call is `await ops.ctx.<op>({args: [...]})` and **resolves to the verb's parsed JSON
object** (do not pass `--json`; it is added). A failing verb throws; use try/catch.

## When to use it
Send ONE program when a task needs 3+ verb calls that depend on each other
(query, then card on the top hits, then change-plan). A single question stays a plain verb.

## Example (query -> parallel cards -> change-plan)
```
ctx-optimize code run - --input '{"terms":"change plan"}' --json <<'JS'
const q = await ops.ctx.query({args: [input.terms, "--budget", "1500"]});
const hits = q.hits.slice(0, 3);
const cards = await Promise.all(hits.map(h => ops.ctx.card({args: [h.node.label]})));
const plan = await ops.ctx.change_plan({args: [hits[0].node.label]});
({labels: hits.map(h => h.node.label), sigs: cards.map(c => c.signature), plan})
JS
```
Return only what you need: the last expression is the result, so filter inside the program.

## Contract
- `--input JSON` becomes the global `input`; `--json` prints one object `{ok, value, error?, diagnostics?, ms}`.
- Exit codes: 0 ok, 1 ran and failed, 2 diagnostics (nothing ran).
- Discover: `code operations`, `code describe <op>`, `code check <file|->` (validate, runs nothing), `code docs`.
- Claude Code Code Mode: call `ctx-optimize code run - --input ... --json` once per task.
- Op names avoid hyphens (`change_plan`) because `ops.ctx.change-plan` parses as subtraction.
