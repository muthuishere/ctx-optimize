---
title: Use cases
description: Six jobs. The command for each.
---

**New repo.** `ctx-optimize up` then `hubs` and `query`. Stops at “why we designed it that way” — read the file.

**About to edit.** `change-plan X` — callers, blast, tests. Ambiguous calls are dropped, so the radius is a floor.

**What breaks.** `affected X --depth 2`. Static. Flags and reflection are not in the graph unless a producer put them there.

**Monorepo.** `up` scans modules. One store per module. Edit one service, `sync` that one.

**Not in the repo.** `add $BILLING_DB_URL` or drop an adapter script. Names in config, values at dial time.

**Teammate clone.** Declare `remote.push` / `remote.pull` in `.ctxoptimize/config.json`. They run `up`.
