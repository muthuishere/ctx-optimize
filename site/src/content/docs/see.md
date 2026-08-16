---
title: See the architecture
description: The same store the agent answers from, drawn as a derived scene — numbered cards, labelled curves, a hub, the outer world. Position means dependency depth. An arrow is a fact.
---

The force-directed viewer is a hairball on any real store. That is not a
rendering bug — a 9,854-node module cannot be a picture of every symbol.
So the dashboard grew a second viewer that draws the **architecture**, and
every mark on it is computed from the graph, never authored.

```bash
ctx-optimize serve
# open Viewer, switch to "Flow — derived architecture"
```

![Flow viewer on reqsume/apps/api — travelling dots along real call curves, database as the hub, the outer world as dashed plates](/ctx-optimize/media/flow-reqsume-api.gif)

reqsume `apps/api`. 77 subsystems, 9,854 nodes, 18,387 edges, 273 ports.
The picture shows the top 7 directories by cross-directory edge weight —
and says so, in the footer. It is a sample, not the whole graph.

## What every mark means

This is the rule that killed the first "world view" we drew: **a map with
no routes is a list in a costume.** Position has to carry information.

| mark | what it is |
|---|---|
| a **card** | a directory — the package boundary the author chose, ranked by cross-directory edge weight |
| an **arrow** | N real `imports` / `calls` edges, lifted to those directories and summed, drawn with the relation and the count |
| a card's **column** | its longest-path depth in the lifted dependency DAG (Sugiyama), so left-to-right is the direction dependencies actually point |
| the **hub** | the most depended-upon directory (in-degree, weighted) — here `database`, 241 in / 16 out |
| the **outer world** | the boundary lane's ports, grouped by transport into dashed plates under the subsystems that open them — **names only**, sensitive ones flagged, never a value |

`AMBIGUOUS` edges are excluded, so an arrow is a fact. The footer prints
what the picture is hiding: top N of M directories, N of M lifted
relations drawn, which test trees were dropped, and that this is a
sample.

The server does the derivation (`GET /api/scene`, read-only). 7,663 nodes
and 14,887 edges on this repo become a **4.4 KB** payload. The browser
draws about thirty shapes. That is why the picture stays fast when the
force-directed view has to budget.

## Two other projections of the same scene

The view switcher is a registry. A third viewer is one entry; the shell
never names one.

**House — the codebase as a building.** The same cards, the same arrows,
the same hub and outer world, projected as a cutaway. Floors follow the
same depth that Flow uses for columns. Useful when you want to *point at
a room* in a review, not follow a curve.

**Graph — the force-directed view.** Still there. Still budgeted. A
browser cannot lay out a million-node graph, and the Linux kernel store
on this machine is 2.85M nodes. Click a node to expand its real
neighborhood. Trust `affected` / `path` / `change-plan` on the CLI when
you need the complete answer — those read the whole graph.

## Drill until the work is visible

A leaf directory is not a leaf. Click a card (or the hub) and the unit
changes:

| you clicked | cards become | edges drawn |
|---|---|---|
| the module | directories | lifted `imports` / `calls` between directories |
| a directory with children | its child directories | lifted between those |
| a directory with no children | its **files** | `includes` / `imports` / `calls` between files |
| a file | its **declarations** | the raw call graph inside that file |

`includes` is why the C case matters: in a kernel directory the
file-to-file structure *is* the architecture, and it is invisible at
directory grain because every one of those edges is internal to a single
card. The existing cap still applies (6 + hub), ranked by lifted edge
weight, with the count printed. The level is a lens, not a dump.

## What an architect can actually see

The picture does not grade the module. It shows the facts an architect
already knows how to read.

On that `apps/api` frame:

- **Layering is real.** `handlers` (544 out) sit left; `repository` sits
  right; `database` is the floor. That is depth, not a sort order.
- **The hub is a number you can point at.** 241 in / 16 out. A review
  can start there without a caption telling you it is a smell.
- **The outer world is the review.** 205 `network.http`, 66
  `config.env`, 2 `process.exec`, 18 marked sensitive. No other tool we
  benchmarked draws this plate, because no other tool has the
  [boundary lane](/ctx-optimize/boundaries/).
- **Ambiguity is withheld, not guessed.** The footnote says so.

The agent has been answering from this store the whole time —
`change-plan`, `card`, `boundaries`. The picture is the same facts,
aimed at a human. [How `serve` is locked down →](/ctx-optimize/dashboard/)

## Motion

Flow draws travelling dots along the curves so a dense bundle of `CALLS`
reads as a direction, not a scribble. `prefers-reduced-motion` stops
every one of them. There is no CDN, no animation library — Canvas 2D
and system fonts, same as the rest of the embedded UI.
