---
title: Cookbook
description: "The question you would ask a teammate, and the one command that answers it."
---

16 real questions, grouped by situation. Every command is copy-pasteable and matches the [CLI reference](/ctx-optimize/cli/) exactly — nothing here is aspirational. Output shapes are excerpts of the real thing; your numbers and file paths will differ.

## "Where is this, who calls that."

### Find something from a few words

```text
ctx-optimize query "refund flow"
```

```text
Refund flow  [section]  README.md L3-L6
ProcessRefund  [function]  pay.go L3-L3
```

### Who calls this function?

```text
ctx-optimize card "Store.Merge"
```

********

```text
Store.Merge  [method]  internal/store/store.go L200-L273
  sig: func (s *Store) Merge(b *schema.Batch) (nodesAdded, edgesAdded int, err error)
  doc: Merge upserts a validated batch into the graph...
  called by (12): cmdAdd, cmdSync, cmdMerge, ...
  calls (6): Batch.Validate, writeNDJSON, ...
```

### Orient yourself in a repo you've never seen

```text
ctx-optimize hubs --top 10
# or browse the generated map:
open ~/ctxoptimize/<repo>/wiki/
```

### See the architecture of a module

```text
ctx-optimize serve
# Viewer → "Flow — derived architecture"
```

A card is a directory, an arrow is N real edges, the hub is whatever everything depends on,
the plates underneath are the ports. Click a card to drill. [How to read it →](/ctx-optimize/see/)

### Connect two symbols you know are related, but not how

```text
ctx-optimize path "cmdAdd" "Store.Merge"
```

### Get a plain-language read on an unfamiliar node

```text
ctx-optimize explain "PaymentService"
```

****````

## "What breaks if I change this."

### About to change a function — the composed answer

```text
ctx-optimize change-plan validateRefund
```

********

```text
callers (1):
  pay.go::ProcessRefund
blast radius (depth 2, 2 shown): ...
tests to run: pay_test.go::TestProcessRefund
confidence: 4 EXTRACTED, 1 INFERRED
```

****``[](https://github.com/muthuishere/ctx-optimize/blob/main/docs/CRITIQUE.md)

### Just the blast radius, nothing else

```text
ctx-optimize affected PaymentService --depth 2
```

****``

### Trust but verify a citation before acting on it

```text
ctx-optimize verify "pay.go:L1-L5"; echo $?
# 0 — node exists, file exists, range in bounds, no drift vs gather-time HEAD
ctx-optimize verify "pay.go:L900-L950"; echo $?
# 1 — out of bounds, refused
```

****``

## "Can I believe what this store just told me."

### Is the store even current?

```text
ctx-optimize status
# fresh:  ✗ STALE — store at 8a5057b, repo now at 03d0f49; run: ctx-optimize add .
ctx-optimize fresh; echo $?   # 0 fresh / 1 stale / 2 unknown (no git HEAD)
```

****``

### Refresh after the store went stale

```text
ctx-optimize up
```

## "The answer isn't in the repo — it's in prod."

### Get a live database schema into the graph

```text
ctx-optimize adapters help postgres          # the setup card: value format, credential params
export BILLING_DB_URL='postgres://reader:$PG_PASS@db.internal:5432/billing'
ctx-optimize add BILLING_DB_URL              # dial, capture the logical shape, merge, record
```

********``

### Debug a source without touching the store

```text
ctx-optimize capture BILLING_DB_URL
# {"producer":"source:BILLING_DB_URL", "nodes":[...], ...} — printed, nothing written
```

### Where do our prod Kubernetes services live?

```text
ctx-optimize query "prod namespace service ingress"
```

### Feed in something with no native connector

```text
cat > .ctxoptimize/adapters/tickets.sh <<'EOF'
#!/bin/sh
echo '{"producer":"tickets","nodes":[{"id":"t:1","label":"TCK-1 checkout bug","kind":"ticket","file_type":"external","source":"tickets"}]}'
EOF
ctx-optimize add .        # "adapter tickets: 1 nodes, 0 edges"
ctx-optimize query "TCK-1"
```

****````

## "This repo has forty services in it."

### Ask a question scoped to just the module you're in

```text
cd services/billing
ctx-optimize query "dev dependency"
```

****````

### Combine a few modules into one view

```text
ctx-optimize merge api worker --into everything
```

## "New repo, new machine, new teammate."

### Onboard a brand-new repo, or your own fresh clone

```text
cd my-repo && ctx-optimize up
```

### Share your gathered store with the team

```text
ctx-optimize remote push    # runs the script YOUR team declared in .ctxoptimize/config.json
# teammate, on a fresh clone:
ctx-optimize up             # pulls the prebuilt store instead of gathering locally
```

****``````

## The mental model behind these commands, and the outcome-framed walkthroughs.

Read [Concepts](/ctx-optimize/concepts/) for the graph model these recipes query, and [Use cases](/ctx-optimize/use-cases/) for the narrative version of onboarding, refactoring safely, and sharing a store across a team. Every verb here is documented in full on the [CLI reference](/ctx-optimize/cli/).

**[](/ctx-optimize/)[](/ctx-optimize/concepts/)[](/ctx-optimize/use-cases/)[](/ctx-optimize/cli/)[](https://github.com/muthuishere/ctx-optimize)[](https://github.com/muthuishere)
