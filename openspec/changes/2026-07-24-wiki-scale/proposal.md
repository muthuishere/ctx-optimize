# ADR — wiki regen is the scale bottleneck (fix it)

Status: **SUPERSEDED BY `spikes.md`** — 2026-07-24. The spike measured
`wiki.Generate` at true drivers scale at **~9 s, not 735 s** — the premise below
("wiki dominates at scale") is FALSE as measured. Read `spikes.md` for the
corrected plan (M1: autosync skips wiki; M0: re-measure the real 735 s; M2:
incremental render only if it proves to matter). The options below are kept for
history. NO product code until owner sign-off (repo change-flow). Sibling to
`2026-07-24-lazy-autosync` (this amends its open-Q4) and implements the
deterministic half of `2026-07-11-graphify-gaps/design.md`.

## The measured cliff

Linux `drivers/` (release binary, valid tree): **1.7M nodes, but 735 s (12 min)
and 1.1 GB** — because it generated **30,760 wiki pages**. Extraction is seconds;
**wiki regen dominates at scale.** Two consequences:

1. `add`/`sync` is O(all files) past ~10k files and explodes — the "if it stops
   performance, do it differently" case, proven.
2. Our "smallest store" numbers at scale are **wiki-driven, not graph-driven**.

## Root cause (grounded)

- `wiki.Generate` (`internal/wiki/wiki.go:30`) reads ALL nodes and writes one page
  **per file/document node** + per hub, **every call** — no incrementality.
- `gatherInto` (`internal/app/multimodule.go:784`) regenerates the WHOLE wiki on
  *any* graph change (`totalN != 0 || totalPruned != 0`). A 1-line edit on a 30k-
  file repo = a 30,760-page rewrite.
- Today's only guard: skip regen when the graph is byte-identical (0 added/0
  pruned). Useless the moment anything changes.

## Why this is urgent for autosync (amends lazy-autosync Q4)

Lever 3's `lazy`/`block` resync calls this same `gatherInto`, so on a big repo a
**background auto-sync would burn 12 min + 1.1 GB on every edit.** That destroys
the "fast/near-free resync" promise. lazy-autosync Q4 ("keep wiki on auto-sync?")
is hereby answered: **NO** — auto-sync must not do full wiki work.

## NOT the LLM problem

`2026-07-11-graphify-gaps` frames this as LLM re-distill caching. **Our wiki is
deterministic markdown — no LLM in the binary.** So we don't need the distill
cache; we need **incremental deterministic rendering**: re-render only the file
pages whose source changed. Simpler, and byte-for-byte reproducible.

## Options

### Option A — control + cheap guards (small, lands now)

1. **`wiki` config field** (committable, scaffolded at init — owner's ask): typed
   `store.WikiMode` read from `.ctxoptimize/config.json` exactly like `autosync`:
   `"wiki": "auto"` (default) | `"on"` | `"off"` (bool-tolerant: `true`→on,
   `false`→off). Global default in `~/ctxoptimize/config.json`; env override
   `CTX_OPTIMIZE_WIKI`.
   - `auto` — generate the wiki up to a page ceiling (default ~5,000 file pages),
     above which skip it and print a one-line hint (`wiki: 30760 pages skipped
     (repo over threshold) — set "wiki":"on" to force`). Query never needs the
     wiki; hubs/graph verbs are unaffected.
   - `on` — always generate (today's behavior).
   - `off` — never generate.
2. **Auto-sync always skips the wiki** (lever 3): a background/inline code resync
   refreshes the graph only; the wiki refreshes on an explicit `add`/`up`/`sync`.
3. **`--no-wiki` flag** on `add`/`sync` for a one-off skip.

Cost: a typed config field + a threshold branch in `gatherInto` + a
`skipWiki` plumb. Low risk, unblocks the cliff and protects autosync immediately.

### Option B — incremental deterministic wiki (the durable fix)

Re-render only what changed, keyed by content-hash (deterministic, no LLM):

- **File pages** (the 30k bulk): each file page is a pure function of that file's
  node + its immediate edges. Cache `wiki/pages/{slug}` keyed by a
  `page_hash` (the file node's content-hash + its rendered neighbors). On sync,
  re-render only pages whose `page_hash` changed; delete pages for removed files.
  Edit one file → render ~1 page, not 30,760.
- **Hub + index pages** (few, global): cheap; regenerate always. (Or hash the hub
  set and skip when unchanged.)
- Determinism gate: `incremental wiki == full-regen wiki`, byte-for-byte, pinned
  in the golden net (same discipline as the graph equality for levers 1+2).

Cost: a per-page hash manifest (`wiki/index.json` or reuse the store manifest) +
a dirty-set render loop + delete-orphans. Medium. This is the real answer;
Option A's threshold becomes a safety net, not the mechanism.

## Recommendation

**A now, B as the durable follow-up** — possibly same session. A alone removes the
cliff (skip past threshold) and fixes autosync (never full-regen in background),
in a small, safe diff. B makes the wiki genuinely scale so big repos still get a
wiki without the 12-min tax. Doing A first de-risks B (the config surface + the
skip seam are shared).

## Open questions for the owner

1. **Scope now**: A only, B only, or **A now + B this session** (my lean: A now,
   then B if the window allows)?
2. **`auto` ceiling**: skip above ~5,000 file pages? Higher/lower? (A pure
   count; the wall-clock cliff starts well before 30k.)
3. **Default**: `auto` (my lean — safe, no 12-min surprise) or `on` (today's
   always-generate, preserving current output for small repos — which `auto`
   already does under the ceiling)?
4. **Scaffold at init**: write `"wiki": "auto"` explicitly into the scaffolded
   config.json (visible, editable), or leave it implicit (absent = auto)? Owner
   asked for it IN the file — lean: write it in, commented intent via the
   instructions card.

## Success check

- Big repo (30k files): `add`/`sync` no longer spends minutes in wiki; either
  skipped past the ceiling (A) or renders only the changed pages (B).
- `autosync lazy/block` never triggers a full wiki regen.
- Small repos: byte-identical wiki to today (auto under ceiling / B == full).
- Golden: `incremental wiki == full` (B); perf floor for wiki regen pinned.
