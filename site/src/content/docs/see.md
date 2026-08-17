---
title: See the architecture
description: Same store as the CLI. A card is a directory. An arrow is a fact.
---

```bash
ctx-optimize serve
# Viewer → Flow
```

![Flow — reqsume/apps/api](/ctx-optimize/media/flow-reqsume-api.gif)

Top 7 directories by cross-directory weight. Footer says what it hid. Sample, not the whole graph.

| mark | is |
|---|---|
| card | a directory |
| arrow | N real `imports` / `calls`, summed. `AMBIGUOUS` excluded |
| column | dependency depth (left → right) |
| hub | most depended-on directory |
| plates | [boundaries](/ctx-optimize/boundaries/) — names, never values |

`GET /api/scene` derives this in Go (7,663 nodes → 4.4 KB). House is the same scene as a building. Graph is the budgeted force view — use the CLI for a complete walk.

Click to change the unit: module → directories → files → declarations. `includes` is why a C directory has an architecture *inside* one card.

`prefers-reduced-motion` stops the dots. No CDN. [Dashboard lock](/ctx-optimize/dashboard/).
