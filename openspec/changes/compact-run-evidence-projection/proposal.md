## Why

Agents spend more context reading command output and writing one-off transformation
programs than they spend querying the graph. In a 30-day local sample, Claude returned
140.8M characters from `Read` and 90.3M from `Bash`; the same transcripts contained
14,596 ad hoc Python programs, 67.2% touching JSON/NDJSON. Blind truncation can reduce
bytes while making the session worse, so ctx-optimize needs a measured, deterministic
way to select complete evidence under a budget without hiding what was omitted.

The opportunity is to apply the product's existing strengths to runtime output: lexical
ranking, explicit budgets, honest abstention, impact analysis, and disclosure. The
ranking unit is a canonicalized evidence block, not a raw line or token.
Spike results and decisions: `design.md` (ADR 34, SPIKED, not owner-accepted).
Agent handoff: `HANDOFF.md`. No product code.

## What Changes

- Add a `ctx-optimize compact run -- <command>` concept that executes a direct
  argument vector, segments its output into structural evidence blocks, preserves
  mandatory diagnostics, and selects the remaining blocks under a token budget.
- Add an omission ledger to every projected result: original and returned size, block
  counts by disposition, projector confidence, and deterministic block identifiers.
- Account for every omitted block without durably retaining arbitrary raw command output.
  Complete recall is deferred until a separate security spike can prove that retention is
  compatible with the repository's no-secret-storage posture.
- Reuse query tokenization, bounded output, deterministic tie-breaking, and disclosure
  principles, but do not apply raw node IDF directly to volatile command output.
- Add optional graph enrichment for output references that resolve exactly to an indexed
  file, location, or symbol. Graph impact may boost a block; it may never suppress a
  failure or convert an unresolved reference into a fact.
- Add command-family projectors only after a generic structure-first spike: logs,
  tests/builds, search/read, and git are the first candidates from measured usage.
- Add deterministic structured-transform concepts for the recurring Python one-offs:
  JSON/NDJSON projection, text grouping, assertions, and bounded filesystem inspection.
- Preserve the child process exit code and stdout/stderr distinction. Low-confidence or
  unsupported output passes through rather than being silently summarized.
- Keep the main binary deterministic: no LLM, embeddings, network service, database
  driver, or background telemetry.
- Do not claim bill or token savings from output reduction alone. Any claim must come
  from the existing multi-pass session benchmark and a judged diagnostic-retention set.

## Capabilities

### New Capabilities

- `command-evidence-projection`: Structure-first command execution, mandatory evidence
  preservation, budgeted block selection, omission disclosure, and optional graph impact
  enrichment. Durable raw recall is deferred.
- `structured-transforms`: Deterministic JSON/NDJSON, text, assertion, and filesystem
  operations covering the highest-frequency classes of ad hoc Python found in local
  agent transcripts. This capability has an independent spike and acceptance gate; it
  does not ride on acceptance of the command projector.

### Modified Capabilities

None. Existing graph, query, audit `log`, and literal `search` behavior remains unchanged.

## Impact

- New planning and spike assets under this change; no product code changes before owner
  review of measured results.
- Likely future code areas: a new internal projection package, CLI dispatch and usage,
  hook installers, usage metrics, and hermetic fixtures.
- The existing `log` and `search` verb names prevent flat RTK-compatible aliases;
  projection and transforms remain under the `compact` namespace.
- Raw command output may contain credentials. V1 does not persist command bodies; any
  future recall store requires a separate fail-closed security spike and owner decision.
- The existing benchmark arena must be extended rather than replaced. Evaluation must
  report output bytes, fresh and cache-read tokens, tool calls, correctness, diagnostic
  recall, false omission rate, wall time, and parser confidence separately.
