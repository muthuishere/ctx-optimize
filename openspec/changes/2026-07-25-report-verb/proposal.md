# ADR — `report`: one artifact for "explain this repo", including what we don't know

Status: **IMPLEMENTED** — 2026-07-25.

## Context

graphify's `analyze()` → `GRAPH_REPORT.md` produces god nodes, *surprising
connections*, and suggested questions. We had `hubs` and a wiki `index.md`
(Subsystems + Hubs) but no single composed answer to "explain this repo to me",
and nothing at all that reported the graph's own blind spots.

## Decision

A `report` verb composing four sections, `--json` for agents:

1. **Store facts** — nodes, edges, confidence mix, subsystem count.
2. **Subsystems** — from the existing deterministic community detection.
3. **Bridges** — edges whose endpoints sit in *different* subsystems: the seams.
4. **What this graph does NOT know** — unattributed call sites, per symbol, with
   the grep that settles them.

Structure is computed from **facts only** (`WithoutAmbiguous`); AMBIGUOUS edges
influence section 4 and nothing else. Pinned by
`TestReportStructureIgnoresAmbiguous`.

## Deliberately NOT graphify's "surprising connections"

Their `_surprise_score` weights confidence `{AMBIGUOUS: 3, INFERRED: 2,
EXTRACTED: 1}` (`analyze.py:211`) — the *least* reliable edge is ranked the
*most* interesting, so the headline finding of their report is the one least
likely to be true. Under "say no instead of being wrong" that is backwards.

We report **bridges** (facts first, sorted EXTRACTED-before-INFERRED) and give
abstentions their own honest section instead of dressing them up as insight.

## Four rounds of measured de-noising

The first draft was garbage, and each fix came from looking at real output
rather than reasoning about it:

| round | what the section showed | fix |
|---|---|---|
| 1 | every bridge was `X imports module://strings\|os\|fmt` | exclude import stubs — an external module is not a subsystem |
| 2 | every bridge was `contains` (`analyze.go contains analyze.go::AmbiguousError.Error`) | nesting is not dependency |
| 3 | `go.mod declares dep:…` and `co_changed_with` flooded it | **allowlist** of dependency relations, not a denylist, so a relation added later cannot silently pollute it |
| 4 | one node held every slot | one row per **subsystem pair** — "where do subsystems touch" is a question about pairs |

Hubs needed the same treatment: the first report ranked `strings` (142),
`os` (111), `fmt` (83) as the repo's top hubs. Import stubs are the
most-connected nodes in *any* repo and say nothing about that repo. graphify
excludes stdlib from god-node ranking for exactly this reason (`analyze.py:9`).
Filtered inside `Report` rather than in `Hubs`, so the standalone `hubs` verb
keeps its shipped behaviour.

## Found while building it: bare-name method resolution is imprecise

Round 4's symptom was not only a presentation problem. Call resolution keys on
the **bare** name (`byName[d.label]`), so every `err.Error()` in the repo
resolves to the single declaration labelled `Error`:

| INFERRED call target | edges | share |
|---|---:|---|
| `internal/analyze/analyze.go::AmbiguousError.Error` | 85 | 3% |
| `internal/extract/tomlwalk/tomlwalk.go::Strings` | 53 | 2% |
| `internal/store/store.go::Open` | 49 | 2% |

of 2,596 INFERRED call edges. These are almost certainly method calls on
unrelated receivers, attributed to whichever declaration happens to own the
name. **Not fixed here** — it is a resolution change with large golden churn and
it deserves its own ADR. Recorded because the report is what surfaced it, which
is a point in the report's favour: a tool that shows you its own graph honestly
will show you its graph's flaws.

## Verified

- Deterministic: two renders byte-identical, and identical under reversed edge
  input order (`TestReportIsDeterministic`).
- Structure ignores AMBIGUOUS; gaps count survives (`TestReportStructureIgnoresAmbiguous`).
- Hubs exclude import stubs; bridges are dependency-relations only, never touch
  an external module, never sit inside one subsystem, never repeat a pair.
- `task ci` green; judged tiers unchanged (16.5 / 13.0).

## Not claimed

- Bridges are only as good as the call graph. With the `Error`-style
  misattribution above still present, some seams on this repo are artifacts of
  that bug rather than real architecture. The section is honest about
  confidence per row, but it cannot be better than its input.
- No comparison of report quality against graphify's on the same corpus has been
  run. The design difference is argued from their scoring code, not measured.
