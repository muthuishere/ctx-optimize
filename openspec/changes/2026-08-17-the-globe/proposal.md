# ADR 30 — the globe: territories, not points, so nothing is a sample

Status: DRAFT — design recorded, spike specified, build NOT started
Date: 2026-08-17

## The ask

> "the globe will have countries — all modules and sub ones. Clicking it, let
> it go inside full, that way we are showing ALL instead of just top. But all
> connections should be there."

## Why this is not the wall view

My first reading was "modules scattered on a sphere", and I argued against it:
a point on a sphere carries no information, half the data faces away, and edges
across the interior are unreadable. That is the killed wall view in 3D.

The actual proposal is different in the way that matters. A country is a
TERRITORY, and a territory has AREA. Area is derived — nodes, or files, or
whatever the level counts — so position and size both say something before a
single edge is drawn.

And it answers a defect every scene in this repo currently carries:

```
top 12 of 228 modules by how much of the repo hangs off them
  — this is a SAMPLE, not the whole repo
top 7 of 4540 directories by cross-directory edge weight
  — this is a SAMPLE, not the whole graph
```

Cards ration slots, so 216 modules are simply absent and the reader is told so
in small print. A map rations AREA instead: all 228 are drawn, the small ones
are small. **"Show everything" stops being a promise the layout cannot keep.**

## What must be true, or it is decoration

Three rules, taken from the wall view's post-mortem:

1. **Area is derived.** A country's size is a count from the store, never a
   layout convenience.
2. **Adjacency means something.** If two countries share a border, that border
   must be a fact — same community, or a dependency — or the map is teaching
   a relationship that does not exist. This is the hard part, and the spike
   below is mostly about it.
3. **No edge is invented.** Connections are the same `depends` / `calls` /
   `shares` / `references` the flow view already draws, at the same confidence
   tiers, with AMBIGUOUS excluded.

If adjacency cannot be made meaningful, the honest fallback is a flat treemap —
same "everything is on screen, area is derived" property, no implied
neighbourliness, and no hemisphere hidden behind the horizon.

## Drill: full, not sampled

Clicking a country enters it and shows ALL of its regions — its directories,
then its files — with the same area rule. That is the same grain ladder
ADR 21/22 already built (module → directory → file → declaration); the globe
changes the projection, not the levels, so it inherits the crumbs, the
`enter_grain` tie-break and the residual-root-store card already fixed.

## THE SPIKE — three questions, none of them about WebGL

`scripts/spikes/globe/` must answer, before any renderer exists:

1. **Does a deterministic territory layout exist at our scales?** Squarified
   treemap on a sphere, or a spherical Voronoi seeded by cluster centroid.
   Determinism is not negotiable — same store, same map, or a screenshot is
   worthless and an arrangement cannot be saved. Test at 7 (reqsume),
   228 (the-factory), 4,540 (linux directories).
2. **Is adjacency earnable?** Order territories by community
   (`2026-07-14-community-detection` already exists) and measure: what share
   of shared borders join two modules that actually have an edge between them?
   Below some floor, rule 2 fails and the answer is the flat treemap.
3. **What is the drawable ceiling?** Territory count, edge count, and frame
   cost. linux at 4,540 directories with 34,915 lifted relations is the real
   test; the flow view draws 32 of them.

## Cost note, stated up front

This is the largest UI change proposed so far: a projection, a layout engine,
and a drill that must stay consistent with three existing views. The flow view
took most of a session and it draws boxes in two dimensions.

The sequencing I would defend: **the docs lens (ADR 28) first** — designed,
spiked, and fixing a complaint raised three times — then the globe. But the
globe is the one that removes "this is a SAMPLE" from every screen, and that
is a bigger prize than any single view.

## Kill criterion

If the spike cannot produce a deterministic layout whose adjacency is
meaningful, the globe does not ship — a flat treemap does. And if the drill
cannot show ALL of a country at the next level, it does not ship at all: the
entire point is that nothing is a top-N.
