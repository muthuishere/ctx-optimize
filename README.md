# ctx-optimize

**Optimize the context an AI coding agent spends on a codebase.** Turn any repo
into a queryable knowledge graph + wiki so an agent can find the right code in a
few precise hops instead of burning tokens searching and reading files.

> ⚠️ **Status: early — architecture under active discussion.** This README
> captures the intent and the principles. Nothing is built yet, and the design
> is still being shaped. Do not treat anything here as final.

---

## What it is

Same job as [graphify](https://github.com/Graphify-Labs/graphify) — code (and
docs) → a knowledge graph you can `query` / `path` / `explain`, plus a browsable
wiki — but built around a different set of principles:

- **Go, single binary.** One implementation, no Python runtime, no package-name
  friction. Easy to install and ship.
- **All languages via one common interface.** Extraction producers conform to a
  single node/edge schema, so support is broad and the producer (tree-sitter /
  LSP / SCIP / whatever is available) is swappable behind the same contract.
- **Configurable output — no hardcoded `graphify-out/`.** The export layout is
  yours to configure; we don't impose a fixed directory structure.
- **Agent-skill first, never LLM-API-direct.** The tool itself does the
  deterministic work (build graph, query, wiki). The reasoning LLM is *your
  agent* — invoked through an **agent skill**, not through an LLM API baked into
  this binary. This keeps it agent-agnostic.
- **Headless? Use toolnexus.** For non-interactive / headless runs we ship a
  [toolnexus](#) example (toolnexus loads agent skills by default) rather than
  wiring a direct model client. Same agnostic posture, no lock-in.

## The stack it leans on

- **[citenexus](https://github.com/muthuishere/citenexus) (Go)** — used as a
  library for the **LLM-wiki** generation and for **injecting documents** (its
  RAG/citation side). ctx-optimize consumes citenexus; it does not modify or
  pollute it.
- **toolnexus** — the headless path: an example that loads ctx-optimize's agent
  skill so the same capability works outside an interactive agent.

## Principles (the "why we're different")

1. **Agent-skill first.** Direct LLM-API usage is discouraged by design. The
   agent is the model; we give it a fast, deterministic graph to reason over.
2. **Agnostic.** Works with any agent that can load a skill. Headless is served
   by a toolnexus example, not a hardcoded provider.
3. **Deterministic core.** Graph build + query stay reproducible and diffable;
   LLM-flavored steps (wiki phrasing, naming) are the agent's job, kept out of
   the deterministic path.
4. **One interface, many producers.** The emit schema is the contract; language
   support and extraction mechanism plug in behind it.
5. **Don't pollute the dependencies.** citenexus stays a focused RAG library;
   ctx-optimize is its own thing and merely consumes it.

## Command surface (intended, not yet built)

```
ctx-optimize add <path>         # ingest code/docs into the graph
ctx-optimize query "<question>" # answer within a token budget
ctx-optimize path <a> <b>       # relation path between two symbols
ctx-optimize explain <symbol>   # a symbol + its neighborhood
ctx-optimize export             # emit graph/wiki in a configurable layout
```

Consumed primarily **through an agent skill**; a **toolnexus** example covers the
headless case.

## Prior art / credit

Design and scope informed by graphify (MIT, © Safi Shamsi) — read as reference,
reimplemented in Go with our own principles. See `docs/VISION.md` for the running
design notes and open architecture questions.

## License

MIT © 2026 Muthukumaran Navaneethakrishnan
