# ADR — competitive wedge: head-to-head benchmarks + the serving-surface question (MCP / standalone graph)

Status: DRAFT v1 — owner review pending 2026-07-14. Decisions marked ⚖️ are
open; nothing here is implemented yet.

## Context — the lane is crowded and features alone won't win

Market snapshot (owner-supplied, 2026-07-14):

| player | traction | notes |
|---|---|---|
| graphify | 82K⭐, 1.2M downloads/mo, ~3mo old | solo maintainer, **242 open PRs** — community PRs sit for months (real staleness) |
| CodeGraph | 54K⭐, MIT | local SQLite + **MCP server**; framework-aware routes + cross-language bridges (separate change: `2026-07-14-framework-routes-and-bridges/`) |
| GitNexus | 43K⭐ | — |
| potpie | $2.2M funded | Neo4j-backed (heavy infra) |
| Serena | — | LSP-as-MCP: no graph, live language server |

graphify's exploitable gaps (owner analysis): **static file dump** (not live),
**cloud-model dependence**, **Python/DX friction**, **weak multi-repo merge**.
Our position against each gap is already built — but *unmeasured against
them*:

- static dump → we have `fresh` (git-HEAD gate), incremental `add` with
  producer-scoped prune, and the shrink guard;
- cloud-model dependence → the binary is deterministic, zero model calls;
- Python/DX friction → single static Go binary, npm install without
  postinstall, prebuilt binaries on GitHub Releases;
- weak multi-repo → mirrored per-module stores + navigator + federated
  queries (beam: 188k nodes, federated query ≈0.6s), merge opt-in.

What we have for proof today: `proof/agent/` (agent-in-the-loop Q&A harness,
etcd monorepo set 11/11), `bench/` numbers on the site (12,484 files gathered
0.67s), the chromium field store (1.49M nodes, 3.7M edges, queries <1s).
None of it is a **side-by-side** against the named competitors.

## Decision 1 — benchmark suite: measure the wedge, publish either way

Build `proof/compare/`: one harness, same repos, same questions, every tool
run the way its own docs recommend. Kill-test discipline (docs/CRITIQUE.md):
publish the numbers that don't flatter us too.

### Metrics (each per tool, per repo)

1. **Cold start to first answer** — install + index/gather + one question,
   wall clock. (Our wedge: seconds; potpie needs Neo4j, graphify needs a
   model key.)
2. **Incremental refresh** — touch 5 files, re-index, measure. (Wedge:
   producer-scoped incremental add vs full re-dump.)
3. **Query latency** — p50/p95 over the question set, local.
4. **Tokens-per-answer + correctness** — the existing `proof/agent` harness
   pointed at each tool's interface; grader unchanged. This is the number
   agents actually pay.
5. **Staleness honesty** — edit code, ask again WITHOUT re-indexing: does the
   tool detect/flag it? (`fresh` exists for exactly this; graphify's static
   dump is the gap.)
6. **Monorepo** — the etcd/beam multi-module sets; scope-follows-cwd vs
   their multi-repo story.
7. **Setup dependencies** — count: env keys, services, runtimes required.
   (Deterministic single binary vs SQLite+MCP host vs Neo4j vs model keys.)

### Subjects

- Same three repos across all tools: one small (this repo), one medium
  (etcd), one monorepo (beam). Chromium optional stretch (most tools won't
  finish — that result IS the datapoint, publish it).
- Tools: graphify, CodeGraph, Serena (LSP lane — different architecture,
  include for honesty), GitNexus if runnable locally. potpie only if its
  Neo4j setup is reasonable in CI — otherwise record "setup did not complete
  in N minutes" as a result.

### Deliverables

- `proof/compare/run.sh` + per-tool adapters, results as committed JSON
  (same pattern as `bench/results.json` on the site).
- A "Compared" page on the site fed from that JSON — including the rows we
  lose. The kill-test framing is the marketing.

## Decision 2 ⚖️ — serving surface: MCP verb, standalone graph service, or neither

Owner prompt: "seems like we need to have mcp server so a standalone graph
instead of multiple download — don't know."

The standing contract (CLAUDE.md, VISION.md): **"no LLM calls, no DB
drivers, no embeddings, no MCP — ever."** This decision is a *contract
amendment question*, not a feature question — spelled out honestly.

**UPDATE 2026-07-14 (market research):** MCP is not one distribution option
among several — it is THE winning pattern in this category. The two breakout
leaders both ship MCP as their primary surface: CodeGraph (~47K stars, MIT,
SQLite + 42 MCP tools, 38 languages) and GitNexus (~42K stars, zero-server
LadybugDB + 16 MCP tools). Independent write-ups frame "local-first graph
served over MCP" as the pattern that won the category. Serena is LSP-as-MCP.
Our skill+hook+pointer mechanism reaches Claude Code / Codex / Copilot /
Devin but is INVISIBLE to the growing set of MCP-only hosts (Cursor, Kiro,
AntiGravity, Gemini CLI, generic MCP clients). This reframes the ⚖️: **the
"no MCP" letter of the contract is now a real adoption ceiling, not a purity
badge.** Recommend flipping Option B to a build, on the stdio/read-only/
stdlib terms below — the contract's SPIRIT (deterministic, no network
listener, no model, agent is the only intelligence) survives; only the "no
MCP" clause is retired. Sources: rywalker.com/research/code-intelligence-tools,
knolli.ai graphify-alternatives, the CodeGraph/GitNexus repos.

### Option A — status quo (skill + hook + pointer block)

Zero new surface. The measured mechanism (S16: pointer block fires, skill
alone doesn't) keeps working for Claude Code / Codex / Copilot / Devin. But
any agent host that speaks ONLY MCP (growing set) cannot reach us at all.

### Option B — `ctx-optimize mcp` (stdio, stdlib-only)

An MCP server verb exposing the read verbs (`query`, `card`, `affected`,
`path`, `hubs`, `status`, `fresh`) as MCP tools over **stdio JSON-RPC — no
SDK, no network listener, no new dependency**. The contract's *spirit*
(deterministic, no network, no server infra, agent is the only intelligence)
survives intact; only the *letter* ("no MCP") changes. Notes:

- Read-only tool set first; `add`/`remote` stay CLI-only (mutation via MCP
  is where the complexity and risk live).
- The skill remains the primary, richer interface; MCP is a compatibility
  door for hosts we can't otherwise enter.
- CRITIQUE counter-weight: MCP hosts already have Bash in most cases — an
  MCP server may add surface without adding reach. Measure before building:
  count real agent hosts that have MCP but NOT shell access (Claude Desktop,
  some IDE hosts, mobile hosts). If that list is short, Option B is vanity.

### Option C — standalone shared graph service ("one graph, many consumers")

The other reading of "standalone graph instead of multiple download": a team
runs ONE store; devs don't each pull. Today `serve` is already a read-only
HTTP JSON API (127.0.0.1, dashboard + /api/query). Extending it to a
team-shared deployment means auth, TLS, and a network contract — the
heaviest amendment, and `remote push/pull` (S3) already solves team sharing
at ~zero infra. Recommend: **no**, unless benchmark feedback shows pull
friction is real. The incremental pull is cheap by design.

### Recommendation (owner to confirm ⚖️)

1. Ship Decision 1 (benchmarks) first — it's cheap, it's our lane
   (deterministic speed + honesty), and it produces the launch story.
2. Option B behind a contract amendment, AFTER the MCP-only-host count says
   it's worth it. If built: stdio only, read-only, stdlib only.
3. Option C: not now.

## Success checks

- `proof/compare` runs end-to-end on a clean machine with one command per
  tool; results JSON committed; site page renders it.
- Each wedge claim above ends up with a measured number next to it — or gets
  retracted (CRITIQUE.md gets the retraction).
- MCP decision recorded here as ✅ with the host-count evidence, whichever
  way it goes.
