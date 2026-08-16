# ADR 21 — A leaf directory is not a leaf: drill into files, then declarations

Status: DRAFT — owner asked for it directly; implementing on that basis
Date: 2026-08-16

## The observation

> "why are you not opening the directory i am even thinking files and content
> method and it has includes and all inside which give another impact i guess"

Correct, and measured. `linux/mm` reports every one of its four subsystems as a
leaf, and the viewer now says so honestly — but "leaf" was only ever true of
DIRECTORIES. Inside one of them:

```
mm/kasan — 17 files · 330 functions · 266 structs · 10 enums
           997 edges wholly inside it (636 contains, 361 calls)
```

361 real call edges between the files of a single directory, and the scene
refuses to draw any of them. The drill stops one level above where the work
happens.

## What changes

The unit of a card stops being "a directory" and becomes "whatever the level
is". The level is decided by what `root` NAMES:

| root | cards are | edges drawn |
|---|---|---|
| `""` | directories | lifted `imports`/`calls` between directories |
| a directory WITH subdirectories | its child directories | lifted between those |
| a directory with NO subdirectories | its FILES | `includes`/`imports`/`calls` between files |
| a FILE | its DECLARATIONS | the raw call graph inside that file |

Nothing else in the derivation changes, and that is the point: layering,
ranking, the hub, the outer world and the honesty notes all key off `owner`,
which is the only thing being generalised. A file that everything in a
directory includes becomes that directory's hub for the same reason `storage`
is the repo's hub — most edges arrive there. The measurement is identical; only
the grain moves.

`includes` is why the C case matters: in a kernel directory the file-to-file
structure IS the architecture, and it is invisible at directory grain because
every one of those edges is internal to a single card.

## What it must not become

The killed world view is the standing warning: a level that draws marks
carrying no information is worse than no level. So the same rules hold all the
way down —

- an arrow is still N REAL store edges, summed, never synthesised;
- a card's column is still its longest-path depth in the lifted DAG at THAT
  level, so position keeps meaning direction of dependency;
- AMBIGUOUS edges stay excluded, at every grain;
- the sample is still declared: "top 6 of 330 declarations" is as honest as
  "top 6 of 50 directories", and it has to be printed.

## Bounds

A file can hold hundreds of declarations, and drawing 330 cards would be the
hairball this view exists to avoid. The existing `Options.Cards` cap applies
unchanged (6 + hub), ranked by lifted edge weight, with the count printed. The
level is a lens, not a dump.

## Kill criterion

If the file level on a real directory produces cards whose ranking is
uninformative — every file with the same in/out because `contains` dominates —
then file grain is a list in a costume and this stops at directories. Measured
on `mm/kasan`: 361 `calls` edges against 636 `contains`, and `contains` is not
a lifted relation, so the ranking is driven by real call structure.


## Amendment 2026-08-16 — a level with no edges is not an empty level

Owner, having drilled into `clis/go/brain`: *"two subdirectories, why can't it
just show it."*

It held `brain` and `skill`, neither importing the other, and the level
rendered as a paragraph explaining that while the two of them appeared as pill
links underneath it. Two real directories, and the answer was a sentence.

The rule that produced it was right when it was written: a scene was a flow
chart, x was longest-path depth in the lifted DAG, and a card with no arrow had
no column to stand in — drawing one would have been exactly the "list in a
costume" the wall view was killed for. So `Derive` dropped every subsystem with
`in+out == 0` before ranking, and reported the level as Empty.

That premise is gone. The clustered layout (ADR 22 D5) places a SET without
claiming a direction, and says so on screen. So:

- **An edgeless subsystem is still a subsystem.** It stays in the pool, ranked
  below the ones with edges, so nothing that had a place loses it.
- **`Empty` keeps only its two real jobs**: a root that names nothing (a typo,
  which must never look like a fact about the code), and a level that genuinely
  holds nothing.
- **`children` alone is the door.** `inner > 0` gated it as well — "there is
  something to SEE in there" — and that was true only while an edgeless level
  drew nothing. `include/net/sctp/structs.h` now opens onto its eleven
  declarations, which is a better answer than "nothing to draw". Inner stays as
  INFORMATION on the card ("2 inside · no links"), because knowing that nothing
  in there references anything else in there is worth having before you click.

Three tests encoded the old rule and were changed to the new intent, each
saying so in place: `TestDeriveEmptyGraphRefusesToPretend` (now
`TestDeriveWithNoFlowStillShowsWhatIsThere`, and it still refuses to invent a
link), the leaf half of `TestDrillCrumbsAlwaysLeadOut`, and the b.go assertion
in `TestCardOnlyOffersALevelWorthOpening`.
