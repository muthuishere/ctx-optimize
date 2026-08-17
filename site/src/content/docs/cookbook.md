---
title: Cookbook
description: Question → one command.
---

```text
# find
ctx-optimize query "refund flow"

# inspect
ctx-optimize card Store.Merge

# about to edit
ctx-optimize change-plan validateRefund

# blast only
ctx-optimize affected PaymentService --depth 2

# A to B
ctx-optimize path cmdAdd Store.Merge

# outer surface
ctx-optimize boundaries
ctx-optimize boundaries --sensitive

# orient
ctx-optimize hubs --top 10
ctx-optimize serve          # Viewer → Flow

# fresh?
ctx-optimize status
ctx-optimize fresh; echo $?   # 0 fresh / 1 stale / 2 unknown
ctx-optimize up
ctx-optimize sync
ctx-optimize verify "pay.go:L1-L5"; echo $?

# live source (env-var NAME, never a raw secret on argv)
ctx-optimize adapters help postgres
ctx-optimize add BILLING_DB_URL
ctx-optimize capture BILLING_DB_URL     # stdout only

# your own producer
# drop .ctxoptimize/adapters/tickets.js — print one Batch, then:
ctx-optimize add .

# this module only
cd services/billing && ctx-optimize query "dev dependency"

# team store
ctx-optimize remote push
# teammate: ctx-optimize up
```

Flags and every verb: [CLI](/ctx-optimize/cli/).
