# ADR 13 — we only measure the one thing we are third-best at

Status: DRAFT — owner review pending 2026-08-15. No product code until agreed.
Scope: question sets and harness reporting only (`internal/golden/testdata/
questions/`, `~/ctx-bench-arena/quality/`, `benchmarks/session/`). No producer
change, no schema change.

## The finding

Every question in every scored set is a **code-locate** question.

The 14 competitor-quality questions (`~/ctx-bench-arena/quality/`):
`cmdQuery`, `loadGraphScoped`, `Merge`, `url_for`, `send_file`,
`dispatch_request`, `Flask`, `JSON`, `ServeHTTP`, `Handle`, `addRoute`,
`to_json`, `node_link_graph`, `cluster` — all "where is this symbol defined".

The 20 judged questions per corpus (`internal/golden/testdata/questions/`):
"Where are requests hashed for elevator merge lookups?", "Which function splits
a bio into segments?", "Where is OPAL self-encrypting drive unlock handled?" —
all "where is this implemented".

**Not one question in any scored set asks about:**

| class | example | who can answer |
|---|---|---|
| boundary — egress | "what external APIs does this repo call?" | **only us** |
| boundary — config | "which env vars does it read, and which are secrets?" | **only us** |
| boundary — process | "what does it shell out to?" | **only us** |
| api surface | "what routes does it expose, and with which methods?" | us; partially ast-grep |
| doc→code | "where is X documented?" / "what governs this file?" | **only us** |
| transport shape | "is this call http, grpc, or a queue?" | **only us** |

So the scoreboard that says *query 0.804 vs codegraph 0.86* is real, and it is
measuring a **narrow slice chosen before half the product existed**. We built a
boundary lane, a services registry, a doc graph and route extraction, and then
scored ourselves on none of them.

## Why this matters more as engineering than as marketing

The obvious reading is "our benchmark undersells us". The important reading is
the opposite:

**An unmeasured capability has no regression net.** The boundary lane has unit
tests, but no judged question anywhere. If a rule silently stopped matching, or
`drift` started accusing on INFERRED evidence, or the services registry lost a
vendor — **no score would move.** That is precisely the failure we just fixed in
the perf gate, which recorded its own time and could never fail. Coverage
without a scored question is a promise, not a gate.

## D1 — add question CLASSES, report per-class, never blended

Extend both scored sets with the classes above, following the fairness rules
`benchmarks/session/session.py` already established:

- Every question carries a **class**. Scores are reported per class and never
  averaged into one number — a blended score would hide both that grep beats us
  on `locate` and that nobody else can enter `boundary`.
- A tool that does not claim a class is **`n/a`, never 0**. Scoring codegraph
  zero on "what external APIs does this repo call" is a rigged comparison, not
  a finding, and it would poison the credibility of the classes where we do
  compete honestly.
- **Breadth is its own axis, stated separately from correctness.** The honest
  claim is "we answer six classes, others answer one or two" — NOT a single
  number in which our unique classes drag the average up. Anyone can win a
  blended score by inventing questions only they can answer; that is the
  benchmark-you-design-and-win failure, and it costs more credibility than it
  buys.

## D2 — ground truth must be as verifiable as the locate questions

The existing rubric works because a definition location is checkable by a
machine. The new classes must hold the same bar:

- **boundary/config**: the env var must exist at a real `file:line` in the
  corpus; `ctx-optimize search` (cross-OS, no rg dependency) is the independent
  ground truth, exactly as `boundaries verify` uses it.
- **boundary/egress**: the host must appear in a real literal or SDK call site.
- **api surface**: route + method must be confirmable at a `file:line`.
- **doc→code**: the doc must actually reference the file (ADR 10's backticked
  path citations are the mechanical form).
- **transport shape**: the expected `transport` value is a closed vocabulary
  (`network.http`, `network.ws`, `config.env`, `process.exec`, …), so it is an
  exact match, not a judgement call.

Where a question cannot be graded mechanically, it does not go in the set.
That constraint is what makes the existing sets defensible and must not be
relaxed for the classes we happen to win.

## D3 — pick corpora that HAVE boundaries

linux and Newtonsoft are poor hosts for these classes: the kernel has no HTTP
egress, and a JSON library has no routes. The measured boundary-rich corpora we
already have are **reqsume** (151 ports across 7 modules, real vendors, real
secrets by name), **go-kubernetes** (404 ports), and **java-spring**/**hono**
(routes). Use those, and say plainly that the code-locate corpora stay as they
are — the point is to ADD coverage, not to retire the questions we currently
lose on. Removing a question we score badly on would be the same dishonesty in
the other direction.

## Kill criterion

If the new classes cannot be graded mechanically at the same rigour as the
locate questions, they do not ship. A soft-graded class in the same table as a
hard-graded one contaminates both, and a skeptic is right to discount the whole
board.

## Open question for the owner

Do the judged golden floors gain these classes too, or only the competitor
board? Adding them to `internal/golden` makes them a CI regression gate for the
boundary lane — which is the real prize here — but it also means a boundary
rule change can fail the build, and those floors currently sit at 16.5/13.0 on
a 20-question scale that would have to be restructured.
