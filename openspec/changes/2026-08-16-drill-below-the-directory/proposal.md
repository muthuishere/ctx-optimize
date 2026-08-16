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
