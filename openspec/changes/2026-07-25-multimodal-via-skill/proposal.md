# ADR — multi-modal ingest: the HOST agent reads, the binary validates

Status: **DRAFT** — not implemented. Deliberately a separate path from the
graphify-parity batch (`abstain-out-loud`, `community-detection`, `report-verb`).

## Context

graphify maps "code, docs, PDFs, images, videos" into its graph. Its README is
precise about how: *"Code is parsed with tree-sitter AST: deterministic, no LLM…
Docs, PDFs, images and video use **your assistant's model**, or a configured API
key, for a semantic pass."*

We have none of it. `docs/VISION.md:6` forbids LLM work in the product, so the
obvious reading is "out of scope, permanently".

**That reading is wrong**, and it is worth stating plainly because it changes
what is possible: graphify does not call an LLM *from the tool* for the default
path either — the assistant does. Our own `docs/CRITIQUE.md:66` already
describes the same architecture as **Tier 3**:

> the wiki GROWS FROM USE. First conceptual answer about community X is paid at
> full price by the host agent — then the skill saves it as the page: binary
> validates every file:line, stamps member_hash, stores. … **no LLM API ever,
> LLM proposes / binary disposes.**

So multi-modal is not a contract violation. It is Tier 3 applied to a different
input type, and the machinery mostly exists: the validated `add --json` door,
`verify`, and content hashing.

## Decision (proposed)

**The binary never reads a PDF, image or video.** The skill instructs the host
agent to read it — the agent already has vision and document handling — and to
emit `Batch` JSON through the existing `--json` door. The binary's job is
unchanged and unchanged-in-kind: validate the schema, refuse anything
unverifiable, store it, stamp provenance.

Three properties that must hold, and which distinguish this from "let the model
write facts into the graph":

1. **Every node carries a checkable location.** A claim about `page 4 of
   spec.pdf` must cite `spec.pdf` + a page/offset that `verify` can re-check.
   No location, no node — the same rule the code lane already follows.
2. **Provenance is explicit.** These nodes are produced by a model, so they get a
   producer id that says so (`agent:<name>`), and a confidence that is never
   `EXTRACTED`. An agent reading the store must be able to tell a parsed fact
   from a summarised one, and `WithoutAmbiguous`-style filtering must be
   available to callers who want only parsed facts.
3. **Content-hash gating.** The source file's hash is stored with the nodes; when
   the file changes the nodes are stale and are dropped or re-requested, never
   silently retained. This is what stops the graph accumulating claims about a
   document that no longer says that.

## Why this is a separate path

The three shipped items are deterministic graph work. This one changes **who
produces facts**. It deserves separate scrutiny, a separate ADR, and probably a
separate opt-in flag — a user who wants the deterministic guarantee should be
able to have a store with no model-produced nodes in it at all, and to prove it
(`nodes --where producer~agent:` returning nothing).

## Open questions for the owner

1. **Opt-in or default?** I would default it OFF: the "no LLM, nothing leaves
   your machine" promise is a real differentiator and this makes it conditional.
2. **What confidence tier?** `INFERRED` reuses an existing tier but conflates a
   name-resolution guess with a model summary. A new tier (`SUMMARISED`?) is
   honest but expands the schema contract — and the schema is a public door.
3. **Is a summary a node at all**, or is it a *page* in the wiki with an edge to
   the file? Nodes are things the graph traverses; a paragraph of prose about a
   PDF may belong in the wiki lane instead, where it cannot pollute `affected`.
4. **Scope of first cut.** Markdown/PDF text extraction is a much smaller step
   than images and video, and would prove the door without the ambiguity of
   "what is in this screenshot".

## Not claimed

- No spike has been run. Cost, quality and staleness behaviour are all unmeasured.
- graphify's multi-modal quality has not been evaluated either, so "parity" here
  is a description of their architecture, not a benchmark result.
