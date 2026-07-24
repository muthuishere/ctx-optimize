# Spikes — native filter vs jq vs python (2026-07-24)

Question: how much does native, in-process filtering beat the
`export --format json | jq` (or `| python`) path the docs currently teach?

## Corpus

The federated mastra store (`~/ctxoptimize/mastra-main`, all 242 module
graphs concatenated as one repo-root export would be): **160,836 nodes,
220,566 edges**. Reassembled `big.json` = **79.5 MB**. The store also persists
these as **`*.ndjson`** on disk — one record per line — which is the key
architectural fact below.

Task (representative): count/emit every `imports` edge (38,468 of them) — the
shape of "which files use X", "all resolves_to edges", "all k8s services".

## Measured (same machine, warm cache)

| path | wall | peak RSS | present on stock Windows/Alpine? |
|---|---|---|---|
| **native** — stream `edges.ndjson`, JSON-decode per line, single pass | **176 ms** | **11.9 MB** | yes (the binary) |
| `jq` over `big.json` (whole-doc) | 730 ms | 561 MB | **no** |
| `python -c json.load` over `big.json` | 670 ms | 771 MB | python usually, jq no |

Native is **~4.1× faster than jq, ~3.8× faster than python, and 47–64× less
memory** — and it is the **only** option that runs on a stock Windows/Alpine
box with nothing else installed.

## Why native wins architecturally (not just "no subprocess")

The store already persists as **ndjson**. A native filter **streams it
record-by-record** and holds O(1) memory — it never materializes the whole
graph. Every `export | {jq,python}` path must first **reassemble the entire
`{nodes,edges}` document and parse all of it** into a second value model
(O(all) memory — the 561/771 MB above). So the win compounds with store size.

**The table is generous to jq/python**: they were handed a pre-materialized
`big.json`. The *real* pipeline is `ctx-optimize export --format json | jq …`,
which additionally pays export's full-graph **serialization + pipe** before jq
even starts — cost the native verb skips entirely. The true gap is larger
than 4×.

## Implications for the design

1. **Ship native filtering over the ndjson stream** — decode per line, filter,
   emit; don't load-then-filter. That's what buys the O(1) memory and the 4×+.
2. **Golden perf floor**: pin the native time on a fixed corpus AND the
   native-vs-jq ratio; CI fails if either regresses (owner's "tighten the
   golden gate"). Real number to floor beneath: ~4× on 220k edges.
3. `--jq` (gojq) stays an arbitrary-shape convenience, but is NOT the fast
   path — it reintroduces whole-doc semantics. Native flag filters are the
   fast path we point agents and the skills at.

## Reproduce

Scripts in the session scratchpad: `nativejson.go` (streaming decode),
`big.json`/`big-edges.ndjson` (assembled from the mastra store). Timings via
`/usr/bin/time -l`.
