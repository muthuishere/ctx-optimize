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

## The reason is the regression net, NOT the scoreboard

Owner's position, and it settles the framing: *"don't have to win all, and the
capability we possess, that's fine."* So this ADR is explicitly **not** an
attempt to raise our number by adding questions we happen to win. If these
classes are added and our score on them is mediocre, that is a fine outcome —
the point is that it becomes VISIBLE and STAYS visible.

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
- **Breadth is a capability STATEMENT, not a score.** Publish a plain matrix of
  which classes each tool answers, the way `tools.json` already documents the
  field. "We model external boundaries; codegraph does not; codegraph shows more
  source per answer than we do" is a true and useful sentence. Turning that into
  a number we win is the benchmark-you-design-and-win failure, and it costs more
  credibility than it buys.
- **We are not trying to win these classes.** We are the only tool that enters
  some of them, which makes a comparative score meaningless there — the number
  that matters is our own, over time, so a regression shows up.

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

## Where we honestly stand, and that being fine

Stated plainly so no later reader has to reconstruct it:

- **Code-locate**: we are third — 0.804 against codegraph's 0.86. codegraph
  wins by printing more source (18KB per answer vs our 7KB). That is a real
  design choice with a real cost, not a defect we must erase. Fix the actual
  DEFECTS (card omitting line numbers is one), then accept where we land.
- **Context efficiency**: we lead, ~2.3x codegraph on correctness-per-byte.
- **Build and re-gather speed, determinism, boundaries**: we lead, in some
  cases by orders of magnitude, and in boundaries we are alone.
- **`ripgrep` beats us on locate and is ~7x cheaper per session.** Already in
  the README, and it should stay there.

No goal in this ADR is "beat X". The goal is that every capability we ship has
a number attached that can go down.

## D4 — YES to golden gating, with per-class floors and a hermetic fixture

Owner decision 2026-08-15: the boundary classes go into the golden net, not
only the competitor board. That is the whole point — a capability with no
failing test is a promise.

Two design constraints follow:

**Per-class floors, never a renumbered blend.** Do NOT extend linux-block's
20-question set to 25 and re-floor it: that disturbs pinned numbers
(16.5 / 13.0) for an unrelated reason and makes a boundary regression look like
a code-locate regression. Each class carries its own floor, so a boundary rule
going quiet fails the BOUNDARY floor and names itself.

**Gate hermetically; measure at scale separately.** linux has no HTTP egress
and Newtonsoft has no routes, so neither can host these classes. But the golden
net already gates on hermetic FIXTURES (`dockerstack`, `multimod`, `pydeps`),
and a boundary fixture is the natural home: a small repo with known env vars
(one flagged sensitive), a literal HTTP host, an SDK call, a spawned process,
and two routes — every expected port written out exactly. That runs in CI with
no corpus clone and fails loudly when a rule stops matching. The corpus tier
can then measure BREADTH on kubernetes (404 ports) without being the gate.

## D5 — ask the developer's question, not our schema's

Sharpening D1's fairness rule, because `n/a` alone does not make a question
fair. A benchmark phrased in our own vocabulary is rigged even when nobody is
scored zero:

| ask this | not this | why |
|---|---|---|
| "How does the API talk to the worker?" | "What is this port's transport?" | the first is a question anyone has; the second tests our schema |
| "What external services does this repo call?" | "List nodes of kind=port, direction=consumes" | ditto |
| "Which config values are secrets?" | "Which ports carry sensitive=true?" | ditto |

The test: **would a developer ask this if ctx-optimize did not exist?** If yes,
it is a fair question and a competitor's inability to answer is a real
capability gap. If no, we invented the question to fit our answer, and it
belongs in a unit test rather than a benchmark.

The transport case is the interesting one and worth stating plainly: "is this
call HTTP or gRPC" IS a real question — it changes how you debug, retry and
couple two services — but it should be asked the way a developer asks it, with
the transport appearing in the ANSWER rather than in the question.
