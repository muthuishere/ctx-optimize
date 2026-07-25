# ADR — Mermaid export

Status: **DRAFT** — not implemented. Scoped out of the 0.11.0 batch.

## Context

`export` supports `json`, `dot` (Graphviz), `graphml` (yEd/Gephi/networkx),
`csv`, and `obsidian` (`internal/app/app.go:1996`). All of them need a tool to
view. Mermaid renders **inline** in GitHub, GitLab, Obsidian, Notion, VS Code
preview and Claude artifacts — so a Mermaid dump is the only export that can be
pasted into a PR description or an ADR and just be a diagram.

graphify ships `callflow_html.py`, which writes Mermaid architecture and
call-flow diagrams. This is the cheapest remaining parity item.

## Decision (proposed)

`ctx-optimize export --format mermaid`, emitting a `flowchart LR`.

The whole design problem is **size**, not syntax. This repo is 4,025 nodes /
8,589 edges; Mermaid becomes unreadable in the low hundreds of nodes and GitHub
refuses to render very large graphs at all. A dump of the full graph would be
technically correct and practically useless — the same failure the `report`
verb's first draft had.

So the export must be **scoped by construction**, reusing filters that already
exist (`--kind`, `--relation`, `--where`, and the graphfilter predicate the
other verbs share):

- Default to a **subsystem-level** diagram: one node per detected community,
  edges = bridges between them (exactly what `report`'s Bridges section
  computes). That is a whole-repo architecture diagram that fits on a page.
- `--kind`/`--relation`/`--where` narrow to a symbol-level diagram when the
  caller has already scoped the question.
- Hard node cap with an explicit truncation note printed **in the diagram**, not
  silently — a diagram that quietly dropped half the graph is a lie in a format
  people paste into design docs.

Open question for the owner: is subsystem-level the right default, or should
`export --format mermaid` refuse without a scope flag and print what scopes are
available? Refusing is more honest; defaulting is friendlier.

## Why it is honest

Mermaid is a rendering of the same nodes/edges — no new facts, no inference.
The only new risk is the truncation one, handled above. AMBIGUOUS edges must be
excluded like every other traversal (ADR `2026-07-25-abstain-out-loud`) or
styled distinctly (dashed), never drawn as solid dependencies.

## Verification (planned)

- Deterministic: same graph → byte-identical output, sorted.
- Renders: the emitted text parses as Mermaid (fixture check against the
  documented grammar subset we use — flowchart, node, edge, subgraph only).
- Truncation is always stated in-diagram when it happens.
- Node ids are escaped: labels contain `(`, `)`, `:`, `.` and quotes, all of
  which are Mermaid syntax.

## Not claimed

No measurement of whether a subsystem-level Mermaid diagram is actually useful
to an agent or a human reviewer. It is plausible and cheap, not proven.
