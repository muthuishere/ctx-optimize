# ADR — `--include-ambiguous`: a door out of the abstention, with the maybes marked

Status: **IMPLEMENTED** — 2026-07-26.

## Context

Two ADRs in two days made the store abstain rather than guess:
`2026-07-25-abstain-out-loud` (a callee name defined more than once) and
`2026-07-25-method-call-resolution` (a method reached through a receiver whose
type we never established). Both route the call site into an AMBIGUOUS
shortlist, and every traversal verb filters AMBIGUOUS out by default.

That is the right default and it left a real hole. After the receiver gate,
**225 call edges on this repo are shortlists**, and for a method like
`Batch.Validate` the fact-only blast radius is 10 nodes while the full
candidate set is 32. The verbs answered honestly and gave no way to ask for the
rest. `edges --relation calls --confidence AMBIGUOUS --to <id>` listed the
shortlist, but as a flat edge dump — you could not *traverse* with it, so
"what might break if I change this method" had no command at all.

Abstaining is only a service if the user can then go look. Otherwise it is just
a smaller answer.

## Decision

`--include-ambiguous` on `card`, `explain`, `affected`, `path`, `hubs` and
`change-plan`. Off by default; nothing about the default answer changes.

The flag is only safe while the widened results cannot be mistaken for facts,
so that is enforced structurally rather than left to the reader:

| verb | how a maybe is marked |
|---|---|
| `affected` / `change-plan` | row prefixed `?`, plus a footer count. The marker rides on the ROW because rows get copied one at a time |
| `card` | its own `MAYBE called by (AMBIGUOUS — verify before acting)` list, printed after the facts. `CalledBy`/`Calls` stay facts-only **under every option** |
| `explain` | its own `outgoing_ambiguous` / `incoming_ambiguous` maps |
| `path` | the hop is labelled, plus "this path crosses an AMBIGUOUS edge… candidate route, not a fact" |
| `hubs` | degree changes; the caller asked for it explicitly |

Two properties are load-bearing and pinned by tests:

1. **The default cannot be lost by forgetting.** Verbs call `forTraversal`,
   which filters unless an option says otherwise — so the filtered path is the
   one you get for free, and widening takes an explicit argument. The previous
   shape (each verb calling `WithoutAmbiguous` by hand) worked only as long as
   nobody forgot.
2. **Fact fields never widen.** `CardData.CalledBy` and `Explanation.Incoming`
   contain only facts whatever flags were passed; the maybes land in separate
   fields. A consumer that never heard of this flag reads exactly what it read
   before — the property that makes offering the door safe at all.

`report` is deliberately excluded. Its structure is facts-only by design
(`TestReportStructureIgnoresAmbiguous`) and it already has a dedicated section
for what could not be resolved; widening it would double-count the same
abstention in two sections.

## Why not make it the default

Because it would undo both prior ADRs. `docs/VISION.md:284` measured the
failure: guessed edges corrupt god-node ranking and blast radii, which is the
one thing this store exists to get right. The motto is "say no instead of being
wrong" — the flag does not weaken that, it says the *no* out loud and then
hands over the shortlist to the person who asked.

## Verified

- `TestAffectedExcludesMaybesUnlessAsked`, `TestCardKeepsFactListsPureAndNamesTheMaybesSeparately`,
  `TestRenderedCardMarksTheMaybes`, `TestExplainAndHubsHonorTheOption`,
  `TestPathRefusesMaybesUnlessAskedAndLabelsTheHop`.
- `task ci` green; judged tiers untouched (the flag is off in every existing
  path, so no measured answer moves).
- Real output on this repo: `affected Batch.Validate --depth 1` → 10 nodes;
  `--include-ambiguous` → 32, of which 22 marked `?`.

## Not claimed

- No measurement of how often the widened set is *useful* versus noisy. The
  argument is that a user who has read "22 unattributed callers" should be able
  to see them without leaving the tool, not that the 22 are mostly real.
- The flag does not improve resolution. It exposes what resolution refused to
  decide; settling those still means reading the code or grepping.
