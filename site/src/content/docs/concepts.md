---
title: What it is
description: "Why query is not grep or embeddings, how the score is actually built, why each verb exists, and the schema — adapters, boundaries, confidence — underneath."
---

Nodes and edges. You ask a verb. No embeddings, no model in this path.

## Why query is not grep

Grep returns a line. You still open the file. Embeddings need a model and still cannot tell a caller from a comment.

`query` ranks **names already in the graph**, then hangs neighbours on the hit.

```text
ctx-optimize query "where is refund processing implemented"
```

### 1. Split like code, drop the English

The question becomes tokens the way identifiers are written.

```text
"where is refund processing implemented"
        │
        ▼  drop where / is   (question grammar — they are not symbols)
refund   processing   implemented

PaymentRefundProcessor.ProcessRefund
        │
        ▼  camelCase + acronyms
payment  refund  processor   process  refund
```

`HTTPServer` → `http` + `server`. Single letters are noise and go away. Stopwords are stripped from the **query only**. If we stripped them from nodes too, a function named `OnPath` would vanish; if we left `on` in the query, IDF would treat that rare word as a clue and hand you `install.go::OnPath`.

What is scored is `label + file path`. The signature and the doc comment ride on the *answer* so you can cite them. They are not extra search fields.

### 2. IDF — a rare word is the clue

Every token gets a weight from how few nodes contain it:

```text
weight(t)  =  0.1  +  ln( N / (1 + df(t)) )
```

`N` is how many nodes are in the store. `df(t)` is how many of them mention `t`. The `0.1` is a floor so a word that appears *everywhere* still matches, just barely.

Imagine:

```text
user      in 30,000 nodes   →  almost no evidence
refund    in    400 nodes   →  real evidence
stripe    in     18 nodes   →  this is probably what they meant
```

So `query "user refund stripe"` is not three equal votes. `stripe` decides. That is why this works on code: the interesting identifier is almost always the rare one.

### 3. Four ways a token can hit — strongest first

**Named.** You typed the node. `query "Store.Merge"` or `query "OPENAI_API_KEY"` matches id, label, or a port’s `identifier` exactly. That hit is **tier 0**. It sits above every scored row. We did not add `+1000` — a bonus has to outrun an unbounded IDF sum, and you cannot promise that. A sort key is correct by construction.

**Exact token.** `refund` is in the node’s set (`RefundProcessor`). Full IDF. Weight 1.0.

**Prefix.** `refund` vs `refunds`. Same idea, **0.7 × IDF**. If several tokens prefix-match, we take the *rarest* one — not whichever Go’s map yielded first. Same query, same store, same answer, every run.

**Trigram.** Typos and infixes. `refnud` and `refund` share character triples:

```text
refund  →  ref  efu  fun  und
refnud  →  ref  efn  fnu  nud
```

Overlap is the [Sørensen–Dice](https://en.wikipedia.org/wiki/S%C3%B8rensen%E2%80%93Dice_coefficient) coefficient: `2 × shared / (len(a) + len(b))`. We keep it only if Dice ≥ 0.5, and it is worth **0.4 × IDF**. Weakest on purpose: a typo can still find you, it cannot beat a real name.

```text
exact token     1.0 × IDF
prefix          0.7 × IDF
trigram         0.4 × IDF
named           not a weight — it sorts first
```

### 4. Then a tiny bit of intent — still no model

`where is url_for implemented` should not rank README, then the test, then the import stub, then the function.

```text
module:// stub     × 0.25     unless you said import / module
*_test.go          × 0.50     unless you said test
section / document × 0.50     unless you said doc / adr / spec
```

That is a rule, not a classifier. Docs used to steal the top slot: 39% of one graph, 15 of 30 top-3 hits on code questions, before this.

### 5. Then the graph — and then we stop

A winning node is not `refund_service.go:37`. It comes with its 1-hop neighbourhood (cap 12):

```text
ProcessRefund  [function]  refund_service.go L37-L80
    sig: func (s *Service) ProcessRefund(id string) error
    ← calls CheckoutHandler
    → calls PaymentGateway.Refund
```

Arrows are **shown**, not extra hits. AMBIGUOUS stays out unless `--include-ambiguous`. Budget ~2000 tokens (`chars/4`) or 20 hits.

Grep still wins strings and config **values**. `card` is indexed (&lt;20 ms on linux). `query` still walks every node.

## Verbs

| Want | Verb |
|---|---|
| Words, no name yet | `query` |
| Known symbol | `card` |
| About to edit | `change-plan` |
| Blast only | `affected` |
| A to B | `path` |
| Plain language | `explain` |
| Orient | `hubs` |
| What it talks to | `boundaries` |
| Picture | `serve` |

[Cookbook](/ctx-optimize/cookbook/) maps the teammate question to the command.

## Adapter

Code is not the system. Drop `.js` / `.py` / `.sh` in `.ctxoptimize/adapters/` — the file *is* registration. Print one `Batch`. Fail-closed validate, merge.

Native sources: an env-var **name** whose value is a URL (`postgres://…`, `s3://…`). Names in config, values only at dial time. Same door: reqsume can show `public.applications` and `reqsume-local` next to `Store.Merge`.

## Boundaries

The call graph that stops at your functions is incomplete. `boundaries` is the outer surface: consumes / provides, transport (`network.http`, `config.env`, `process.exec`), identifier by **name**. Secrets flagged by name. Never a value. Flow draws them as plates.

## Schema

```text
Batch { producer, nodes[], edges[] }
edge.confidence = EXTRACTED | INFERRED | AMBIGUOUS
```

EXTRACTED = parsed. INFERRED = name-matched. AMBIGUOUS = shortlist, filtered out of traversals by default. Blast radius is a floor.

| Lands as | From |
|---|---|
| file / func / class | tree-sitter (`L#-L#`) |
| heading | goldmark (fences are not sections) |
| route → handler | FastAPI, Express, OpenAPI, Ingress… |
| `dep:` | package.json, go.mod, pom, csproj… |
| k8s | manifests; Secret **nodes**, never data |
| table / bucket | live URL in an env var |
| anything else | your adapter |

Store: `~/ctxoptimize/<name>/` (ndjson). Commit: `.ctxoptimize/`. `status` / `fresh` vs git HEAD. Autosync **off**. One store per module; cwd is the scope.

Embedded langs: Go, Python, JS, TS/TSX, Java, C, C++, C#, Rust, Zig, SQL. Others are packs. [How it works](/ctx-optimize/how-it-works/).
